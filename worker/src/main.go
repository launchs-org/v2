// ワーカーのエントリポイント。Temporal の Workflow と Activity を実行する。
package main

import (
	"fmt"
	"os"

	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedconfig "launchs/shared/config"
	"launchs/shared/database"
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
	// k8s の namespace を削除するものを、設定に合わせて作る。
	namespaceDeleter, namespaceDeleterError := cluster.NewNamespaceDeleter(workerConfig, appLogger)
	if namespaceDeleterError != nil {
		return namespaceDeleterError
	}
	projectDeletionActivities := activity.NewProjectDeletionActivities(
		namespaceDeleter,
		repository.NewProjectRepository(appDatabase),
		appLogger,
	)
	generalWorker.RegisterActivityWithOptions(projectDeletionActivities.DeleteProjectNamespace, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteProjectNamespaceActivityName})
	generalWorker.RegisterActivityWithOptions(projectDeletionActivities.DeleteProjectRecords, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteProjectRecordsActivityName})

	// ワーカーを起動し、停止するまで待つ。
	appLogger.Info("ワーカーを起動する", "task_queue", sharedworkflow.TaskQueueGeneral, "temporal", workerConfig.TemporalAddress)
	if runError := generalWorker.Run(worker.InterruptCh()); runError != nil {
		return fmt.Errorf("ワーカーが異常終了しました: %w", runError)
	}
	appLogger.Info("ワーカーを停止した")
	return nil
}
