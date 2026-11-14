package leader

import (
	"bytes"
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"launchs/shared/database/testdatabase"
	"launchs/shared/logger"
)

// testLockKey は、テスト用のロックの番号。
const testLockKey int64 = 4242

// waitFor は、条件が成り立つまで、短い間隔で待つ。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - description: 待っているものの説明(失敗したときの表示)。
//   - condition: 成り立つべき条件。
func waitFor(testingT *testing.T, description string, condition func() bool) {
	// 失敗したとき、この関数ではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()
	// 期限まで、条件を確かめ続ける。
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	testingT.Fatalf("期限までに成り立たなかった: %s", description)
}

// TestElectorSingleLeaderAndTakeover は、2つの Elector のうち、1つだけがリーダーになり、
// リーダーが止まると、もう1つが引き継ぐことを確認する。
func TestElectorSingleLeaderAndTakeover(testingT *testing.T) {
	// 同じデータベースを、2つの Elector が使う。
	database := testdatabase.New(testingT)
	testLogger := logger.New(&bytes.Buffer{}, "test", slog.LevelInfo)
	var running [2]atomic.Bool
	contexts := make([]context.CancelFunc, 2)
	for index := 0; index < 2; index++ {
		elector, electorError := NewElector(database, testLockKey, 20*time.Millisecond, 50*time.Millisecond, testLogger)
		if electorError != nil {
			testingT.Fatalf("作れるはず: %v", electorError)
		}
		runContext, cancel := context.WithCancel(context.Background())
		contexts[index] = cancel
		defer cancel()
		index := index
		// 仕事は、止められるまで、動いていることを示し続ける。
		go func() {
			_ = elector.Run(runContext, func(leaderContext context.Context) error {
				running[index].Store(true)
				<-leaderContext.Done()
				running[index].Store(false)
				return nil
			})
		}()
	}

	// どちらか1つだけが、リーダーになる(しばらく待っても、2つ同時にはならない)。
	waitFor(testingT, "どちらかがリーダーになる", func() bool { return running[0].Load() || running[1].Load() })
	time.Sleep(300 * time.Millisecond)
	if running[0].Load() == running[1].Load() {
		testingT.Fatalf("1つだけがリーダーのはず: %v %v", running[0].Load(), running[1].Load())
	}

	// リーダーを止めると、もう1つが引き継ぐ。
	leaderIndex := 0
	if running[1].Load() {
		leaderIndex = 1
	}
	contexts[leaderIndex]()
	otherIndex := 1 - leaderIndex
	waitFor(testingT, "もう1つが引き継ぐ", func() bool { return running[otherIndex].Load() })
	if running[leaderIndex].Load() {
		testingT.Errorf("止めた方は、もう動いていないはず")
	}
}

// TestElectorStopsWhenConnectionIsLost は、ロックを持つ接続が切れると、リーダーの仕事が止まり、
// 再びロックを取って、仕事をやり直すことを確認する。
func TestElectorStopsWhenConnectionIsLost(testingT *testing.T) {
	// Elector を1つだけ動かし、仕事が始まった回数と、止められた回数を数える。
	database := testdatabase.New(testingT)
	testLogger := logger.New(&bytes.Buffer{}, "test", slog.LevelInfo)
	elector, electorError := NewElector(database, testLockKey, 20*time.Millisecond, 30*time.Millisecond, testLogger)
	if electorError != nil {
		testingT.Fatalf("作れるはず: %v", electorError)
	}
	var started, stopped atomic.Int32
	runContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = elector.Run(runContext, func(leaderContext context.Context) error {
			started.Add(1)
			<-leaderContext.Done()
			stopped.Add(1)
			return nil
		})
	}()
	waitFor(testingT, "リーダーになる", func() bool { return started.Load() == 1 })

	// ロックを持つ接続を、サーバー側から切る。
	if terminateError := database.Exec("SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype = 'advisory' AND objid = ?", testLockKey).Error; terminateError != nil {
		testingT.Fatalf("接続を切れるはず: %v", terminateError)
	}

	// 仕事が止められ、もう一度リーダーになって、仕事が始まり直す。
	waitFor(testingT, "仕事が止められる", func() bool { return stopped.Load() >= 1 })
	waitFor(testingT, "もう一度リーダーになる", func() bool { return started.Load() >= 2 })
}

// TestElectorReturnsWhenContextIsCanceled は、止める合図で Run が戻り、リーダーの仕事も止まることを確認する。
func TestElectorReturnsWhenContextIsCanceled(testingT *testing.T) {
	// 仕事は、止められるまで動く。
	database := testdatabase.New(testingT)
	testLogger := logger.New(&bytes.Buffer{}, "test", slog.LevelInfo)
	elector, electorError := NewElector(database, testLockKey, 20*time.Millisecond, 30*time.Millisecond, testLogger)
	if electorError != nil {
		testingT.Fatalf("作れるはず: %v", electorError)
	}
	var started atomic.Bool
	runContext, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		_ = elector.Run(runContext, func(leaderContext context.Context) error {
			started.Store(true)
			<-leaderContext.Done()
			return nil
		})
		close(finished)
	}()
	waitFor(testingT, "リーダーになる", started.Load)

	// 止める合図を送ると、Run が戻る。
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		testingT.Fatalf("Run が戻らなかった")
	}
}
