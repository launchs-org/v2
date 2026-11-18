// accessstats は、Traefik の Pod のログから、ingress のアクセスの集計(プラグイン launchs-stats の出力)を読み取り、DB に保存する(docs/ingress-plugins.md)。
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

	"launchs/accessstats/src/collector"
	"launchs/accessstats/src/config"
	"launchs/accessstats/src/retention"
	sharedconfig "launchs/shared/config"
	"launchs/shared/database"
	"launchs/shared/k8s"
	"launchs/shared/leader"
	"launchs/shared/logger"
	"launchs/shared/repository"
)

const (
	// leaderLockKey は、リーダー選出の advisory lock の鍵。watcher(7100001)、logcollector(7100002)とは、別の値にする。
	leaderLockKey int64 = 7100003

	// leaderRetryInterval は、リーダーになれなかったときの、取り直しの間隔。
	leaderRetryInterval = 5 * time.Second

	// leaderHeartbeatInterval は、リーダーが、ロックの接続の生存を確かめる間隔。
	leaderHeartbeatInterval = 5 * time.Second
)

// main は、run を呼んで、失敗したときは、原因を表示して、異常終了する。
func main() {
	// 動かして、失敗したら、標準エラー出力に書いて、終了コード 1 で終わる。
	if runError := run(); runError != nil {
		fmt.Fprintln(os.Stderr, runError)
		os.Exit(1)
	}
}

// run は、設定を読み、リーダーになったら、収集とまとめを始める。停止の合図(SIGINT、SIGTERM)で、安全に止まる。
//
// 戻り値:
//   - 起動できなかったときのエラー。正常に止まったときは nil。
func run() error {
	// 設定とロガーを用意する。
	statsConfig, configError := config.LoadAccessStatsConfig(sharedconfig.NewProcessEnvironmentReader())
	if configError != nil {
		return fmt.Errorf("設定を読み込めませんでした: %w", configError)
	}
	appLogger := logger.New(os.Stdout, "accessstats", statsConfig.LogLevel)
	// 停止の合図で、終わる context を作る。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// k8s に接続する(KUBERNETES_ACCESS=none なら、何も集めず、停止の合図まで待つ。開発用)。
	clientset, clientsetError := k8s.NewClientset(statsConfig.Kubernetes)
	if clientsetError != nil {
		if clientsetError == k8s.ErrNotConnected {
			appLogger.Warn("KUBERNETES_ACCESS=none のため、アクセスの集計は集められません(開発用)。停止の合図まで待ちます")
			<-ctx.Done()
			return nil
		}
		return clientsetError
	}
	// データベースに接続する。
	appDatabase, databaseError := database.Open(statsConfig.DatabaseURL)
	if databaseError != nil {
		return databaseError
	}
	defer func() { _ = database.Close(appDatabase) }()
	// リーダーになったものだけが、収集とまとめを動かす。
	elector, electorError := leader.NewElector(appDatabase, leaderLockKey, leaderRetryInterval, leaderHeartbeatInterval, appLogger)
	if electorError != nil {
		return electorError
	}
	statsRepository := repository.NewAccessStatsRepository(appDatabase)
	appLogger.Info("accessstats を起動する(リーダーになるまで待つ)")
	return elector.Run(ctx, func(leaderContext context.Context) error {
		// まとめと削除は、別の goroutine で動かす。
		workContext, stopWork := context.WithCancel(leaderContext)
		defer stopWork()
		go retention.NewRoller(statsRepository, statsConfig.MinuteRetention, statsConfig.HourRetention, retention.Interval, appLogger).Run(workContext)
		// 収集は、リーダーでなくなる(または、止める合図)まで、動かす。
		collector.NewManager(collector.NewKubernetesSource(clientset, statsConfig.TraefikNamespace, statsConfig.TraefikSelector), statsRepository,
			collector.Settings{ReconcileInterval: statsConfig.ReconcileInterval, KeepClients: statsConfig.KeepClients}, appLogger).Run(workContext)
		return nil
	})
}
