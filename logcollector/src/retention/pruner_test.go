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
	"launchs/shared/repository"
)

// fakeLogStore は、テスト用の LogStore。呼び出された引数を記録し、決めたエラーを返す。
type fakeLogStore struct {
	mutex          sync.Mutex
	cutoffs        []time.Time
	maxBytes       []int64
	failuresBefore int
}

// PruneLogs は、引数を記録して、最初の failuresBefore 回は、エラーを返す。
//
// 引数:
//   - requestContext: 使わない。
//   - cutoff: 削除の基準の時刻。
//   - maxBytesPerPod: Pod ごとの上限。
//
// 戻り値:
//   - 削除の結果と、エラー。
func (store *fakeLogStore) PruneLogs(requestContext context.Context, cutoff time.Time, maxBytesPerPod int64) (repository.LogPruneResult, error) {
	// 記録して、失敗させる回数なら、エラーを返す。
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.cutoffs = append(store.cutoffs, cutoff)
	store.maxBytes = append(store.maxBytes, maxBytesPerPod)
	if len(store.cutoffs) <= store.failuresBefore {
		return repository.LogPruneResult{}, errors.New("database is down")
	}
	return repository.LogPruneResult{ExpiredLines: 1}, nil
}

// callCount は、呼び出された回数を返す。
//
// 戻り値:
//   - 回数。
func (store *fakeLogStore) callCount() int {
	// ロックして数える。
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return len(store.cutoffs)
}

// TestPrunerRunsImmediatelyAndPeriodicallyAndSurvivesFailures は、始めに1回削除すること、間隔ごとに繰り返すこと、
// 失敗しても止まらずに、次の間隔でやり直すこと、基準の時刻が「いま - 保存期間」であること、止める合図で終わることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestPrunerRunsImmediatelyAndPeriodicallyAndSurvivesFailures(testingT *testing.T) {
	// 最初の1回は失敗する(削除できない)ストアと、固定の時計。
	store := &fakeLogStore{failuresBefore: 1}
	pruner := NewPruner(store, 7*24*time.Hour, 1234, 10*time.Millisecond, logger.New(&bytes.Buffer{}, "logcollector", slog.LevelInfo))
	fixedNow := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	pruner.now = func() time.Time { return fixedNow }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		pruner.Run(ctx)
	}()
	// 失敗した後も、繰り返される。
	deadline := time.Now().Add(5 * time.Second)
	for store.callCount() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if store.callCount() < 3 {
		testingT.Fatalf("失敗しても繰り返すはず: %d", store.callCount())
	}
	// 基準は 7 日前、上限は設定の値。
	if !store.cutoffs[0].Equal(fixedNow.Add(-7*24*time.Hour)) || store.maxBytes[0] != 1234 {
		testingT.Fatalf("基準と上限が合うはず: %v %d", store.cutoffs[0], store.maxBytes[0])
	}
}
