package retention

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"launchs/shared/logger"
)

// fakeProjectEventStore は、テスト用の ProjectEventStore。呼び出しの引数を覚え、決めた結果を返す。
type fakeProjectEventStore struct {
	mutex         sync.Mutex
	cutoffs       []time.Time
	maxPerProject []int
	result        int64
	err           error
}

// PruneProjectEvents は、引数を覚えて、決めた件数とエラーを返す。
func (fakeStore *fakeProjectEventStore) PruneProjectEvents(_ context.Context, cutoff time.Time, maxPerProject int) (int64, error) {
	fakeStore.mutex.Lock()
	defer fakeStore.mutex.Unlock()
	fakeStore.cutoffs = append(fakeStore.cutoffs, cutoff)
	fakeStore.maxPerProject = append(fakeStore.maxPerProject, maxPerProject)
	return fakeStore.result, fakeStore.err
}

// callCount は、PruneProjectEvents が呼ばれた回数を返す。
func (fakeStore *fakeProjectEventStore) callCount() int {
	fakeStore.mutex.Lock()
	defer fakeStore.mutex.Unlock()
	return len(fakeStore.cutoffs)
}

// TestProjectEventPrunerRun は、始めに1回、そのあとは間隔ごとに削除し、保存期間と件数の上限を渡し、
// 失敗しても続け、止める合図で戻ることを確認する。
func TestProjectEventPrunerRun(testingT *testing.T) {
	// 設定の値が定数のとおりであること(決めた値: 7日、5000件)。
	testingT.Run("保存期間は7日、1つのプロジェクトの上限は5000件", func(subTest *testing.T) {
		if ProjectEventRetention != 7*24*time.Hour || MaxProjectEventsPerProject != 5000 {
			subTest.Errorf("決めた値と違う: %v %d", ProjectEventRetention, MaxProjectEventsPerProject)
		}
	})
	// 成功しても、失敗しても、止まらずに繰り返す。
	for _, testCase := range []struct {
		name string
		err  error
	}{{"成功しても、間隔ごとに繰り返す", nil}, {"失敗しても、止まらずにやり直す", errors.New("database down")}} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			fakeStore := &fakeProjectEventStore{result: 2, err: testCase.err}
			fixedNow := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
			pruner := NewProjectEventPruner(fakeStore, 7*24*time.Hour, 5000, 10*time.Millisecond, logger.New(&bytes.Buffer{}, "test", slog.LevelInfo))
			pruner.now = func() time.Time { return fixedNow }
			ctx, cancel := context.WithCancel(context.Background())
			finished := make(chan struct{})
			go func() { pruner.Run(ctx); close(finished) }()

			// 始めの1回のあとも、繰り返し呼ばれる。
			deadline := time.Now().Add(5 * time.Second)
			for fakeStore.callCount() < 3 && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			cancel()
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				subTest.Fatalf("止める合図で戻るはず")
			}
			if fakeStore.callCount() < 3 {
				subTest.Fatalf("3回以上呼ばれるはず: %d", fakeStore.callCount())
			}
			// 渡す時刻は「いまから保存期間を引いた時刻」、件数の上限はそのまま。
			if !fakeStore.cutoffs[0].Equal(fixedNow.Add(-7*24*time.Hour)) || fakeStore.maxPerProject[0] != 5000 {
				subTest.Errorf("引数が違う: %v %d", fakeStore.cutoffs[0], fakeStore.maxPerProject[0])
			}
		})
	}
}
