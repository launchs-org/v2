package crashloop

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"launchs/shared/logger"
	"launchs/shared/repository"
	"launchs/shared/workflow"
)

// fakeCandidateStore は、テスト用の CandidateStore。決めた候補を返し、渡された上限を覚える。
type fakeCandidateStore struct {
	mutex      sync.Mutex
	candidates []repository.CrashLoopCandidate
	err        error
	limits     []repository.CrashLoopLimits
}

// ListCrashLoopCandidates は、上限を覚えて、決めた候補とエラーを返す。
func (fakeStore *fakeCandidateStore) ListCrashLoopCandidates(_ context.Context, limits repository.CrashLoopLimits) ([]repository.CrashLoopCandidate, error) {
	fakeStore.mutex.Lock()
	defer fakeStore.mutex.Unlock()
	fakeStore.limits = append(fakeStore.limits, limits)
	return fakeStore.candidates, fakeStore.err
}

// fakeStarter は、テスト用の Starter。開始を頼まれた入力を覚え、deploy ごとに決めたエラーを返す。
type fakeStarter struct {
	mutex    sync.Mutex
	started  []workflow.StopCrashLoopInput
	failFor  string
	attempts int
}

// StartStop は、入力を覚えて、決めた deploy だけエラーを返す。
func (fakeStarter *fakeStarter) StartStop(_ context.Context, input workflow.StopCrashLoopInput) error {
	fakeStarter.mutex.Lock()
	defer fakeStarter.mutex.Unlock()
	fakeStarter.attempts++
	if input.DeployID == fakeStarter.failFor {
		return errors.New("temporal down")
	}
	fakeStarter.started = append(fakeStarter.started, input)
	return nil
}

// TestNewStopInput は、止める候補から、理由と補足を含む Workflow の入力が作られることを確認する。
func TestNewStopInput(testingT *testing.T) {
	testingT.Run("待っている理由があれば、それを理由にする", func(subTest *testing.T) {
		input := newStopInput(repository.CrashLoopCandidate{DeployID: "deploy-1", PodUID: "uid-1", PodName: "pod-1", RestartCount: 9, WaitingReason: "CrashLoopBackOff", UnhealthyFor: 42*time.Minute + 30*time.Second})
		if input.DeployID != "deploy-1" || input.PodUID != "uid-1" || input.PodName != "pod-1" || input.Reason != "CrashLoopBackOff" ||
			!strings.Contains(input.Message, "9 回") || !strings.Contains(input.Message, "42 分") {
			subTest.Errorf("入力が違う: %+v", input)
		}
	})
	testingT.Run("待っている理由が無ければ、Unhealthy を理由にする", func(subTest *testing.T) {
		if input := newStopInput(repository.CrashLoopCandidate{DeployID: "deploy-1"}); input.Reason != "Unhealthy" {
			subTest.Errorf("Unhealthy のはず: %q", input.Reason)
		}
	})
}

// TestMonitorRun は、始めに1回、間隔ごとに、候補を探して Workflow を開始し、渡された上限を使い、
// 開始に失敗した deploy があっても他の deploy は続け、探すのに失敗しても止まらず、止める合図で戻ることを確認する。
func TestMonitorRun(testingT *testing.T) {
	limits := repository.CrashLoopLimits{UnhealthyFor: 10 * time.Minute}
	run := func(subTest *testing.T, fakeStore *fakeCandidateStore, starter *fakeStarter, rounds int) {
		monitor := NewMonitor(fakeStore, starter, limits, 10*time.Millisecond, logger.New(&bytes.Buffer{}, "test", slog.LevelInfo))
		ctx, cancel := context.WithCancel(context.Background())
		finished := make(chan struct{})
		go func() { monitor.Run(ctx); close(finished) }()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			fakeStore.mutex.Lock()
			calls := len(fakeStore.limits)
			fakeStore.mutex.Unlock()
			if calls >= rounds {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			subTest.Fatalf("止める合図で戻るはず")
		}
	}

	testingT.Run("候補ごとに、止める Workflow の開始を頼み、渡した上限で探す(繰り返し探す)", func(subTest *testing.T) {
		fakeStore := &fakeCandidateStore{candidates: []repository.CrashLoopCandidate{{DeployID: "deploy-1", PodName: "pod-1"}, {DeployID: "deploy-2", PodName: "pod-2"}}}
		starter := &fakeStarter{}
		run(subTest, fakeStore, starter, 3)
		if len(starter.started) < 4 || starter.started[0].DeployID != "deploy-1" || starter.started[1].DeployID != "deploy-2" {
			subTest.Errorf("2つの deploy を、繰り返し頼むはず: %+v", starter.started)
		}
		if fakeStore.limits[0] != limits {
			subTest.Errorf("渡した上限で探すはず: %+v", fakeStore.limits[0])
		}
	})
	testingT.Run("候補が無ければ、何も開始しない", func(subTest *testing.T) {
		starter := &fakeStarter{}
		run(subTest, &fakeCandidateStore{}, starter, 2)
		if starter.attempts != 0 {
			subTest.Errorf("何も開始しないはず: %d", starter.attempts)
		}
	})
	testingT.Run("1つの開始に失敗しても、他の deploy は続ける", func(subTest *testing.T) {
		fakeStore := &fakeCandidateStore{candidates: []repository.CrashLoopCandidate{{DeployID: "deploy-1"}, {DeployID: "deploy-2"}}}
		starter := &fakeStarter{failFor: "deploy-1"}
		run(subTest, fakeStore, starter, 2)
		if len(starter.started) == 0 || starter.started[0].DeployID != "deploy-2" {
			subTest.Errorf("deploy-2 は開始されるはず: %+v", starter.started)
		}
	})
	testingT.Run("探すのに失敗しても、止まらずに繰り返す", func(subTest *testing.T) {
		fakeStore := &fakeCandidateStore{err: errors.New("database down")}
		run(subTest, fakeStore, &fakeStarter{}, 3)
		if len(fakeStore.limits) < 3 {
			subTest.Errorf("3回以上探すはず: %d", len(fakeStore.limits))
		}
	})
}
