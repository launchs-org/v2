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

// fakeEventStore は、テスト用の EventStore。呼び出しの引数を覚え、決めた結果を返す。
type fakeEventStore struct {
	mutex        sync.Mutex
	cutoffs      []time.Time
	maxPerDeploy []int
	result       int64
	err          error
}

// PruneEvents は、引数を覚えて、決めた件数とエラーを返す。
func (fakeStore *fakeEventStore) PruneEvents(_ context.Context, cutoff time.Time, maxPerDeploy int) (int64, error) {
	fakeStore.mutex.Lock()
	defer fakeStore.mutex.Unlock()
	fakeStore.cutoffs = append(fakeStore.cutoffs, cutoff)
	fakeStore.maxPerDeploy = append(fakeStore.maxPerDeploy, maxPerDeploy)
	return fakeStore.result, fakeStore.err
}

// callCount は、PruneEvents が呼ばれた回数を返す。
func (fakeStore *fakeEventStore) callCount() int {
	fakeStore.mutex.Lock()
	defer fakeStore.mutex.Unlock()
	return len(fakeStore.cutoffs)
}

// TestPrunerRun は、始めに1回、そのあとは間隔ごとに削除し、保存期間と件数の上限を渡し、
// 失敗しても続け、止める合図で戻ることを確認する。
func TestPrunerRun(testingT *testing.T) {
	// 設定の値が定数のとおりであること(決めた値: 30日、1000件)。
	testingT.Run("保存期間は30日、1つの deploy の上限は1000件", func(subTest *testing.T) {
		if PodEventRetention != 30*24*time.Hour || MaxPodEventsPerDeploy != 1000 {
			subTest.Errorf("決めた値と違う: %v %d", PodEventRetention, MaxPodEventsPerDeploy)
		}
	})
	// 失敗する保存先でも、止まらずに繰り返す。
	for _, testCase := range []struct {
		name string
		err  error
	}{{"成功しても、間隔ごとに繰り返す", nil}, {"失敗しても、止まらずにやり直す", errors.New("database down")}} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			fakeStore := &fakeEventStore{result: 2, err: testCase.err}
			fixedNow := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
			pruner := NewPruner(fakeStore, 30*24*time.Hour, 1000, 10*time.Millisecond, logger.New(&bytes.Buffer{}, "test", slog.LevelInfo))
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
			if !fakeStore.cutoffs[0].Equal(fixedNow.Add(-30*24*time.Hour)) || fakeStore.maxPerDeploy[0] != 1000 {
				subTest.Errorf("引数が違う: %v %d", fakeStore.cutoffs[0], fakeStore.maxPerDeploy[0])
			}
		})
	}
}
