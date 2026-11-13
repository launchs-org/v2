// ワーカーのエントリポイント。Temporal の Workflow と Activity を実行する。
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedconfig "launchs/shared/config"
	"launchs/shared/database"
	"launchs/shared/k8s"
	"launchs/shared/logger"
	"launchs/shared/repository"
	sharedworkflow "launchs/shared/workflow"
	"launchs/worker/src/activity"
	"launchs/worker/src/cluster"
	"launchs/worker/src/config"
	"launchs/worker/src/workflow"
)

// main は、ワーカーを起動し、失敗したときは、原因を標準エラー出力に書いて、終了コード 1 で終わる。
func main() {
	// 起動に失敗したら、原因を出して異常終了する。
	if runError := run(); runError != nil {
		fmt.Fprintln(os.Stderr, runError)
		os.Exit(1)
	}
}

// run は、設定を読み、データベースと Temporal に接続して、停止の合図(SIGINT、SIGTERM)まで Workflow を処理する。
// 停止の合図は、Temporal の SDK の InterruptCh が受け取り、処理中の Activity を待ってから止まる。
//
// 戻り値:
//   - 設定の読み込み、接続、起動に失敗したときのエラー。正常に停止したときは nil。
func run() error {
	// 環境変数から設定を読む。
	workerConfig, configError := config.LoadWorkerConfig(sharedconfig.NewProcessEnvironmentReader())
	if configError != nil {
		return fmt.Errorf("設定を読み込めませんでした: %w", configError)
	}
	appLogger := logger.New(os.Stdout, "worker", workerConfig.LogLevel)

	// app データベースに接続する。
	appDatabase, databaseError := database.Open(workerConfig.DatabaseURL)
	if databaseError != nil {
		return databaseError
	}
	defer func() { _ = database.Close(appDatabase) }()

	// Temporal のサーバーに接続する。
	temporalClient, clientError := client.Dial(client.Options{
		HostPort:  workerConfig.TemporalAddress,
		Namespace: workerConfig.TemporalNamespace,
	})
	if clientError != nil {
		return fmt.Errorf("Temporal に接続できませんでした: %w", clientError)
	}
	defer temporalClient.Close()

	// 一般の Task Queue を処理するワーカーを作り、Workflow と Activity を登録する。
	generalWorker := worker.New(temporalClient, sharedworkflow.TaskQueueGeneral, worker.Options{})
	generalWorker.RegisterWorkflowWithOptions(workflow.DeleteProjectWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.DeleteProjectWorkflowName})
	// k8s の namespace を削除するものを、設定に合わせて選ぶ。
	namespaceDeleter, namespaceDeleterError := chooseNamespaceDeleter(workerConfig.Kubernetes, appLogger)
	if namespaceDeleterError != nil {
		return namespaceDeleterError
	}
	projectDeletionActivities := activity.NewProjectDeletionActivities(
		namespaceDeleter,
		repository.NewProjectRepository(appDatabase),
		appLogger,
	)
	generalWorker.RegisterActivityWithOptions(projectDeletionActivities.DeleteProjectNamespace, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteProjectNamespaceActivityName})
	generalWorker.RegisterActivityWithOptions(projectDeletionActivities.MarkProjectDeleteFailed, sdkactivity.RegisterOptions{Name: sharedworkflow.MarkProjectDeleteFailedActivityName})
	generalWorker.RegisterActivityWithOptions(projectDeletionActivities.DeleteProjectRecords, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteProjectRecordsActivityName})

	// ワーカーを起動し、停止するまで待つ。
	appLogger.Info("ワーカーを起動する", "task_queue", sharedworkflow.TaskQueueGeneral, "temporal", workerConfig.TemporalAddress)
	if runError := generalWorker.Run(worker.InterruptCh()); runError != nil {
		return fmt.Errorf("ワーカーが異常終了しました: %w", runError)
	}
	appLogger.Info("ワーカーを停止した")
	return nil
}

// chooseNamespaceDeleter は、k8s への接続の設定に合わせて、namespace を削除するものを選ぶ。
//
// 選び方:
//   - 接続の方法が none: k8s につながず、削除せずにログだけを出す仮の実装(開発用)。
//   - それ以外: shared/k8s で作った k8s のクライアントで、実際に削除する実装。
//
// 引数:
//   - kubernetesSettings: k8s への接続の設定。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - namespace を削除するもの。
//   - k8s への接続の設定を読めなかったときのエラー。
func chooseNamespaceDeleter(kubernetesSettings k8s.Settings, appLogger *slog.Logger) (activity.NamespaceDeleter, error) {
	// k8s のクライアントを作る(クラスタへの通信は、最初の操作まで行われない)。
	clientset, clientsetError := k8s.NewClientset(kubernetesSettings)

	// 接続しない設定のときは、起動のログでそうなっていることが分かるようにして、仮の実装を使う。
	if errors.Is(clientsetError, k8s.ErrNotConnected) {
		appLogger.Warn("KUBERNETES_ACCESS=none のため、k8s の namespace は削除されません(開発用)")
		return activity.NewLoggingNamespaceDeleter(appLogger), nil
	}
	// それ以外のエラーは、起動を止める。
	if clientsetError != nil {
		return nil, clientsetError
	}
	// 本物のクライアントで、実際に削除する実装を使う。
	return cluster.NewKubernetesNamespaceDeleter(clientset), nil
}
