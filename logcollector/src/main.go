// logcollector は、deploy のコンテナの出力ログを、Pod ごとに購読して、DB に保存する(docs/logs.md)。
//
// 同時に1つだけ動く(PostgreSQL の advisory lock でリーダーを決める。他は、リーダーが止まるまで待つ)。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"launchs/logcollector/src/collector"
	"launchs/logcollector/src/config"
	"launchs/logcollector/src/retention"
	sharedconfig "launchs/shared/config"
	"launchs/shared/database"
	"launchs/shared/k8s"
	"launchs/shared/leader"
	"launchs/shared/logger"
	"launchs/shared/repository"
)

const (
	// leaderLockKey は、リーダー選出の advisory lock の鍵。watcher(7100001)とは、別の値にする。
	leaderLockKey int64 = 7100002

	// leaderRetryInterval は、リーダーになれなかったときの、取り直しの間隔。
	leaderRetryInterval = 5 * time.Second

	// leaderHeartbeatInterval は、リーダーが、ロックの接続の生存を確かめる間隔。
	leaderHeartbeatInterval = 5 * time.Second

	// followerStopTimeout は、止める合図の後、収集が書き終えるのを待つ時間の上限。
	followerStopTimeout = 10 * time.Second
)

// main は、run を呼んで、失敗したときは、原因を表示して、異常終了する。
func main() {
	// 動かして、失敗したら、標準エラー出力に書いて、終了コード 1 で終わる。
	if runError := run(); runError != nil {
		fmt.Fprintln(os.Stderr, runError)
		os.Exit(1)
	}
}

// run は、設定を読み、リーダーになったら、収集と削除を始める。停止の合図(SIGINT、SIGTERM)で、安全に止まる。
//
// 戻り値:
//   - 起動できなかったときのエラー。正常に止まったときは nil。
func run() error {
	// 設定とロガーを用意する。
	collectorConfig, configError := config.LoadLogcollectorConfig(sharedconfig.NewProcessEnvironmentReader())
	if configError != nil {
		return fmt.Errorf("設定を読み込めませんでした: %w", configError)
	}
	appLogger := logger.New(os.Stdout, "logcollector", collectorConfig.LogLevel)
	// 停止の合図で、終わる context を作る。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// k8s に接続する(KUBERNETES_ACCESS=none なら、何も集めず、停止の合図まで待つ。開発用)。
	clientset, clientsetError := k8s.NewClientset(collectorConfig.Kubernetes)
	if clientsetError != nil {
		if clientsetError == k8s.ErrNotConnected {
			appLogger.Warn("KUBERNETES_ACCESS=none のため、ログは集められません(開発用)。停止の合図まで待ちます")
			<-ctx.Done()
			return nil
		}
		return clientsetError
	}
	// データベースに接続する。
	appDatabase, databaseError := database.Open(collectorConfig.DatabaseURL)
	if databaseError != nil {
		return databaseError
	}
	defer func() { _ = database.Close(appDatabase) }()
	// リーダーになったものだけが、収集と削除を動かす。
	elector, electorError := leader.NewElector(appDatabase, leaderLockKey, leaderRetryInterval, leaderHeartbeatInterval, appLogger)
	if electorError != nil {
		return electorError
	}
	logRepository := repository.NewDeployLogRepository(appDatabase)
	appLogger.Info("logcollector を起動する(リーダーになるまで待つ)")
	return elector.Run(ctx, func(leaderContext context.Context) error {
		// 保存期間と上限の削除は、別の goroutine で動かす。
		workContext, stopWork := context.WithCancel(leaderContext)
		defer stopWork()
		pruner := retention.NewPruner(logRepository, collectorConfig.Retention, collectorConfig.MaxBytesPerPod, retention.PruneInterval, appLogger)
		go pruner.Run(workContext)
		// 収集は、リーダーでなくなる(または、止める合図)まで、動かす。
		manager := collector.NewManager(logRepository, collector.NewKubernetesLogSource(clientset), collector.ManagerSettings{
			ReconcileInterval: collectorConfig.ReconcileInterval, MaxStreams: collectorConfig.MaxStreams, StopTimeout: followerStopTimeout,
			Follower: collector.DefaultFollowerSettings(collectorConfig.MaxLineBytes, collectorConfig.IngestBytesPerSec, collectorConfig.FlushInterval),
		}, appLogger)
		manager.Run(workContext)
		return nil
	})
}
