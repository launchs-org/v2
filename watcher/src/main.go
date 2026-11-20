// watcher のエントリポイント。k8s の Pod と Deployment を観測し、実際の状態を DB に反映する。
//
// 同時に動くのは1つだけ。複数起動しても、PostgreSQL の advisory lock で、リーダーになった1つだけが観測する。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"

	sharedconfig "launchs/shared/config"
	"launchs/shared/database"
	"launchs/shared/k8s"
	"launchs/shared/leader"
	"launchs/shared/logger"
	"launchs/shared/repository"
	"launchs/watcher/src/config"
	"launchs/watcher/src/crashloop"
	"launchs/watcher/src/events"
	"launchs/watcher/src/metrics"
	"launchs/watcher/src/observer"
	"launchs/watcher/src/retention"
)

const (
	// leaderLockKey は、watcher のリーダー選出に使う、advisory lock の番号。他の用途と重ならない値にする。
	leaderLockKey int64 = 7100001

	// leaderRetryInterval は、リーダーになれなかったときに、次にロックを試すまでの待ち時間。
	leaderRetryInterval = 5 * time.Second

	// leaderHeartbeatInterval は、リーダーの間、データベースの接続が生きているかを確かめる間隔。
	leaderHeartbeatInterval = 5 * time.Second
)

// main は、watcher を起動し、失敗したときは、原因を標準エラー出力に書いて、終了コード 1 で終わる。
func main() {
	// 起動に失敗したら、原因を出して異常終了する。
	if runError := run(); runError != nil {
		fmt.Fprintln(os.Stderr, runError)
		os.Exit(1)
	}
}

// run は、設定を読み、データベースと k8s に接続して、停止の合図(SIGINT、SIGTERM)まで観測する。
//
// 戻り値:
//   - 設定の読み込み、接続に失敗したときのエラー。正常に停止したときは nil。
func run() error {
	// 環境変数から設定を読む。
	watcherConfig, configError := config.LoadWatcherConfig(sharedconfig.NewProcessEnvironmentReader())
	if configError != nil {
		return fmt.Errorf("設定を読み込めませんでした: %w", configError)
	}
	appLogger := logger.New(os.Stdout, "watcher", watcherConfig.LogLevel)

	// 停止の合図(SIGINT、SIGTERM)で、キャンセルされる context を作る。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// k8s のクライアントを作る。つながない設定(none)のときは、観測できないので、停止の合図まで何もせずに待つ。
	clientset, clientsetError := k8s.NewClientset(watcherConfig.Kubernetes)
	if clientsetError != nil {
		if clientsetError == k8s.ErrNotConnected {
			appLogger.Warn("KUBERNETES_ACCESS=none のため、k8s は観測されません(開発用)。停止の合図まで待ちます")
			<-ctx.Done()
			return nil
		}
		return clientsetError
	}

	// app データベースに接続する。
	appDatabase, databaseError := database.Open(watcherConfig.DatabaseURL)
	if databaseError != nil {
		return databaseError
	}
	defer func() { _ = database.Close(appDatabase) }()

	// Temporal に接続する(異常な状態が続く deploy を止める Workflow の開始に使う)。
	temporalClient, temporalError := client.Dial(client.Options{HostPort: watcherConfig.TemporalAddress, Namespace: watcherConfig.TemporalNamespace})
	if temporalError != nil {
		return fmt.Errorf("Temporal に接続できませんでした: %w", temporalError)
	}
	defer temporalClient.Close()

	// リーダー選出を作り、リーダーになったときだけ観測する。
	elector, electorError := leader.NewElector(appDatabase, leaderLockKey, leaderRetryInterval, leaderHeartbeatInterval, appLogger)
	if electorError != nil {
		return electorError
	}
	observationRepository := repository.NewDeployObservationRepository(appDatabase)
	projectEventRepository := repository.NewProjectEventRepository(appDatabase)
	appLogger.Info("watcher を起動する(リーダーになるまで待つ)")
	return elector.Run(ctx, func(leaderContext context.Context) error {
		// ----- 上限を超えた Pod のイベントを、一定の間隔で削除する(リーダーの間だけ。観測が終わったら止める) -----
		workContext, stopWork := context.WithCancel(leaderContext)
		defer stopWork()
		pruner := retention.NewPruner(observationRepository, retention.PodEventRetention, retention.MaxPodEventsPerDeploy, retention.PruneInterval, appLogger)
		go pruner.Run(workContext)

		// ----- 上限を超えたプロジェクトの k8s の Event を、一定の間隔で削除する(リーダーの間だけ) -----
		projectEventPruner := retention.NewProjectEventPruner(projectEventRepository, retention.ProjectEventRetention, retention.MaxProjectEventsPerProject, retention.PruneInterval, appLogger)
		go projectEventPruner.Run(workContext)

		// ----- プロジェクトの namespace の k8s の Event を、DB に保存する(リーダーの間だけ。止まったときは、ログに出して、観測は続ける) -----
		go func() {
			if collectError := events.New(clientset, projectEventRepository, appLogger).Run(workContext); collectError != nil && workContext.Err() == nil {
				appLogger.Error("k8s の Event の収集が止まりました", "error", collectError)
			}
		}()

		// ----- deploy の CPU・メモリ・再起動の回数を、一定の間隔で観測して、DB に保存する(リーダーの間だけ。24 時間分を残す) -----
		go metrics.New(clientset, repository.NewDeployMetricRepository(appDatabase), appLogger).Run(workContext)

		// ----- 異常な状態が続く deploy を探して、止める Workflow を開始する(リーダーの間だけ) -----
		crashLoopMonitor := crashloop.NewMonitor(observationRepository, crashloop.NewTemporalStarter(temporalClient), watcherConfig.CrashLoop, crashloop.CheckInterval, appLogger)
		go crashLoopMonitor.Run(workContext)

		// ----- 観測する(リーダーになるたびに、新しい Observer で、全件の読み込みからやり直す。引き継ぎと同じ手順) -----
		return observer.New(clientset, observationRepository, appLogger).Run(workContext)
	})
}
