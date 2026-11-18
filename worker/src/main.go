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
	// k8s を操作する役を、設定に合わせて選ぶ。
	operators, operatorsError := chooseClusterOperators(workerConfig.Kubernetes, cluster.OperatorSettings{
		RuntimeClass: workerConfig.RuntimeClass, IngressNamespace: workerConfig.IngressNamespace, BlockedNetworks: workerConfig.BlockedNetworks,
	}, cluster.IngressRouteSettings{APIVersion: workerConfig.TraefikAPIVersion, EntryPoints: workerConfig.TraefikEntryPoints},
		cluster.VolumeSettings{StorageClass: workerConfig.VolumeStorageClass, AccessMode: workerConfig.VolumeAccessMode}, appLogger)
	if operatorsError != nil {
		return operatorsError
	}
	projectDeletionActivities := activity.NewProjectDeletionActivities(
		operators.namespaceDeleter,
		repository.NewProjectRepository(appDatabase),
		appLogger,
	)
	generalWorker.RegisterActivityWithOptions(projectDeletionActivities.DeleteProjectNamespace, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteProjectNamespaceActivityName})
	generalWorker.RegisterActivityWithOptions(projectDeletionActivities.MarkProjectDeleteFailed, sdkactivity.RegisterOptions{Name: sharedworkflow.MarkProjectDeleteFailedActivityName})
	generalWorker.RegisterActivityWithOptions(projectDeletionActivities.DeleteProjectRecords, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteProjectRecordsActivityName})

	// ワーカーを起動し、停止するまで待つ。

	// deploy を k8s に作る Workflow と、その Activity を登録する。
	generalWorker.RegisterWorkflowWithOptions(workflow.CreateDeployWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.CreateDeployWorkflowName})
	createDeployActivities := activity.NewCreateDeployActivities(repository.NewDeployRepository(appDatabase), operators.deploymentOperator, repository.NewDeployObservationRepository(appDatabase), appLogger)
	generalWorker.RegisterActivityWithOptions(createDeployActivities.EnsureProjectNamespace, sdkactivity.RegisterOptions{Name: sharedworkflow.EnsureProjectNamespaceActivityName})
	generalWorker.RegisterActivityWithOptions(createDeployActivities.ApplyDeployment, sdkactivity.RegisterOptions{Name: sharedworkflow.ApplyDeploymentActivityName})
	generalWorker.RegisterActivityWithOptions(createDeployActivities.WaitDeploymentReady, sdkactivity.RegisterOptions{Name: sharedworkflow.WaitDeploymentReadyActivityName})
	generalWorker.RegisterActivityWithOptions(createDeployActivities.MarkDeployResult, sdkactivity.RegisterOptions{Name: sharedworkflow.MarkDeployResultActivityName})

	// 起動設定(pending)を k8s に反映する Workflow と、その Activity を登録する(起動を待つ Activity は、作成と共通)。
	generalWorker.RegisterWorkflowWithOptions(workflow.ApplyDeployConfigWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.ApplyDeployConfigWorkflowName})
	applyDeployConfigActivities := activity.NewApplyDeployConfigActivities(repository.NewDeployRepository(appDatabase), operators.deploymentOperator, appLogger)
	generalWorker.RegisterActivityWithOptions(applyDeployConfigActivities.ApplyPendingConfig, sdkactivity.RegisterOptions{Name: sharedworkflow.ApplyPendingConfigActivityName})
	generalWorker.RegisterActivityWithOptions(applyDeployConfigActivities.RollbackDeploymentConfig, sdkactivity.RegisterOptions{Name: sharedworkflow.RollbackDeploymentConfigActivityName})
	generalWorker.RegisterActivityWithOptions(applyDeployConfigActivities.CommitConfigResult, sdkactivity.RegisterOptions{Name: sharedworkflow.CommitConfigResultActivityName})

	// レプリカ数を k8s に反映する Workflow と、その Activity を登録する(起動を待つ Activity は、作成と共通)。
	generalWorker.RegisterWorkflowWithOptions(workflow.ScaleDeployWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.ScaleDeployWorkflowName})
	scaleDeployActivities := activity.NewScaleDeployActivities(repository.NewDeployRepository(appDatabase), operators.deploymentOperator, appLogger)
	generalWorker.RegisterActivityWithOptions(scaleDeployActivities.ApplyScale, sdkactivity.RegisterOptions{Name: sharedworkflow.ApplyScaleActivityName})
	generalWorker.RegisterActivityWithOptions(scaleDeployActivities.MarkScaleResult, sdkactivity.RegisterOptions{Name: sharedworkflow.MarkScaleResultActivityName})

	// deploy を削除する Workflow と、その Activity を登録する。
	generalWorker.RegisterWorkflowWithOptions(workflow.DeleteDeployWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.DeleteDeployWorkflowName})
	deleteDeployActivities := activity.NewDeleteDeployActivities(repository.NewDeployRepository(appDatabase), operators.deploymentOperator, appLogger)
	generalWorker.RegisterActivityWithOptions(deleteDeployActivities.DeleteDeployment, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteDeploymentActivityName})
	generalWorker.RegisterActivityWithOptions(deleteDeployActivities.DeleteDeployRecord, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteDeployRecordActivityName})
	generalWorker.RegisterActivityWithOptions(deleteDeployActivities.MarkDeployDeleteFailed, sdkactivity.RegisterOptions{Name: sharedworkflow.MarkDeployDeleteFailedActivityName})

	// 異常な状態が続く deploy を止める Workflow と、その Activity を登録する。
	generalWorker.RegisterWorkflowWithOptions(workflow.StopCrashLoopWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.StopCrashLoopWorkflowName})
	crashLoopActivities := activity.NewCrashLoopActivities(repository.NewDeployRepository(appDatabase), operators.deploymentOperator, appLogger)
	generalWorker.RegisterActivityWithOptions(crashLoopActivities.BeginCrashLoopStop, sdkactivity.RegisterOptions{Name: sharedworkflow.BeginCrashLoopStopActivityName})
	generalWorker.RegisterActivityWithOptions(crashLoopActivities.ScaleToZero, sdkactivity.RegisterOptions{Name: sharedworkflow.ScaleToZeroActivityName})
	generalWorker.RegisterActivityWithOptions(crashLoopActivities.FinishCrashLoopStop, sdkactivity.RegisterOptions{Name: sharedworkflow.FinishCrashLoopStopActivityName})

	// service(k8s の Service)を作る・削除する Workflow と、その Activity を登録する。deploy の削除が使う Activity も、ここにある。
	generalWorker.RegisterWorkflowWithOptions(workflow.CreateServiceWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.CreateServiceWorkflowName})
	generalWorker.RegisterWorkflowWithOptions(workflow.DeleteServiceWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.DeleteServiceWorkflowName})
	serviceRepository := repository.NewServiceRepository(appDatabase)
	serviceActivities := activity.NewServiceActivities(serviceRepository, serviceRepository, operators.serviceOperator,
		activity.ServiceSettings{ServiceType: workerConfig.ServiceType, ClusterDomain: workerConfig.ClusterDomain}, appLogger)
	generalWorker.RegisterActivityWithOptions(serviceActivities.EnsureService, sdkactivity.RegisterOptions{Name: sharedworkflow.EnsureServiceActivityName})
	generalWorker.RegisterActivityWithOptions(serviceActivities.MarkServiceResult, sdkactivity.RegisterOptions{Name: sharedworkflow.MarkServiceResultActivityName})
	generalWorker.RegisterActivityWithOptions(serviceActivities.DeleteServiceObject, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteServiceObjectActivityName})
	generalWorker.RegisterActivityWithOptions(serviceActivities.DeleteServiceRecord, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteServiceRecordActivityName})
	generalWorker.RegisterActivityWithOptions(serviceActivities.MarkServiceDeleteFailed, sdkactivity.RegisterOptions{Name: sharedworkflow.MarkServiceDeleteFailedActivityName})
	generalWorker.RegisterWorkflowWithOptions(workflow.ApplyServicePortsWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.ApplyServicePortsWorkflowName})
	generalWorker.RegisterActivityWithOptions(serviceActivities.ApplyServicePorts, sdkactivity.RegisterOptions{Name: sharedworkflow.ApplyServicePortsActivityName})
	generalWorker.RegisterActivityWithOptions(serviceActivities.RollbackServicePorts, sdkactivity.RegisterOptions{Name: sharedworkflow.RollbackServicePortsActivityName})
	generalWorker.RegisterActivityWithOptions(serviceActivities.CommitServicePortsResult, sdkactivity.RegisterOptions{Name: sharedworkflow.CommitServicePortsResultActivityName})
	generalWorker.RegisterActivityWithOptions(serviceActivities.DeleteDeployService, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteDeployServiceActivityName})

	// ingress(ルールの反映と削除)の Workflow と Activity を登録する。
	generalWorker.RegisterWorkflowWithOptions(workflow.ApplyIngressRulesWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.ApplyIngressRulesWorkflowName})
	generalWorker.RegisterWorkflowWithOptions(workflow.DeleteIngressWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.DeleteIngressWorkflowName})
	ingressActivities := activity.NewIngressActivities(repository.NewIngressRepository(appDatabase), operators.ingressOperator, appLogger)
	generalWorker.RegisterActivityWithOptions(ingressActivities.ApplyIngressRules, sdkactivity.RegisterOptions{Name: sharedworkflow.ApplyIngressRulesActivityName})
	generalWorker.RegisterActivityWithOptions(ingressActivities.RollbackIngressRules, sdkactivity.RegisterOptions{Name: sharedworkflow.RollbackIngressRulesActivityName})
	generalWorker.RegisterActivityWithOptions(ingressActivities.CommitIngressRulesResult, sdkactivity.RegisterOptions{Name: sharedworkflow.CommitIngressRulesResultActivityName})
	generalWorker.RegisterActivityWithOptions(ingressActivities.DeleteIngressObject, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteIngressObjectActivityName})
	generalWorker.RegisterActivityWithOptions(ingressActivities.DeleteIngressRecord, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteIngressRecordActivityName})
	generalWorker.RegisterActivityWithOptions(ingressActivities.MarkIngressDeleteFailed, sdkactivity.RegisterOptions{Name: sharedworkflow.MarkIngressDeleteFailedActivityName})

	// volume(拡張の反映と削除)と、マウントの反映の Workflow と Activity を登録する。
	generalWorker.RegisterWorkflowWithOptions(workflow.ApplyVolumeSizeWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.ApplyVolumeSizeWorkflowName})
	generalWorker.RegisterWorkflowWithOptions(workflow.DeleteVolumeWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.DeleteVolumeWorkflowName})
	volumeRepository := repository.NewVolumeRepository(appDatabase)
	volumeActivities := activity.NewVolumeActivities(volumeRepository, operators.volumeOperator, appLogger)
	generalWorker.RegisterActivityWithOptions(volumeActivities.ExpandVolumeClaim, sdkactivity.RegisterOptions{Name: sharedworkflow.ExpandVolumeClaimActivityName})
	generalWorker.RegisterActivityWithOptions(volumeActivities.CommitVolumeSizeResult, sdkactivity.RegisterOptions{Name: sharedworkflow.CommitVolumeSizeResultActivityName})
	generalWorker.RegisterActivityWithOptions(volumeActivities.DeleteVolumeClaim, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteVolumeClaimActivityName})
	generalWorker.RegisterActivityWithOptions(volumeActivities.DeleteVolumeRecord, sdkactivity.RegisterOptions{Name: sharedworkflow.DeleteVolumeRecordActivityName})
	generalWorker.RegisterActivityWithOptions(volumeActivities.MarkVolumeDeleteFailed, sdkactivity.RegisterOptions{Name: sharedworkflow.MarkVolumeDeleteFailedActivityName})
	generalWorker.RegisterWorkflowWithOptions(workflow.ApplyDeployMountsWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.ApplyDeployMountsWorkflowName})
	mountsActivities := activity.NewApplyDeployMountsActivities(repository.NewDeployRepository(appDatabase), volumeRepository, operators.deploymentOperator, operators.volumeOperator, appLogger)
	generalWorker.RegisterActivityWithOptions(mountsActivities.ApplyPendingMounts, sdkactivity.RegisterOptions{Name: sharedworkflow.ApplyPendingMountsActivityName})
	generalWorker.RegisterActivityWithOptions(mountsActivities.RollbackDeploymentMounts, sdkactivity.RegisterOptions{Name: sharedworkflow.RollbackDeploymentMountsActivityName})
	generalWorker.RegisterActivityWithOptions(mountsActivities.CommitMountsResult, sdkactivity.RegisterOptions{Name: sharedworkflow.CommitMountsResultActivityName})
	// deploy のファイル(ConfigMap)の反映の Workflow と Activity を登録する。
	generalWorker.RegisterWorkflowWithOptions(workflow.ApplyDeployFilesWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.ApplyDeployFilesWorkflowName})
	filesActivities := activity.NewApplyDeployFilesActivities(repository.NewDeployRepository(appDatabase), operators.deploymentOperator, operators.configMapOperator, appLogger)
	generalWorker.RegisterActivityWithOptions(filesActivities.ApplyPendingFiles, sdkactivity.RegisterOptions{Name: sharedworkflow.ApplyPendingFilesActivityName})
	generalWorker.RegisterActivityWithOptions(filesActivities.RollbackDeploymentFiles, sdkactivity.RegisterOptions{Name: sharedworkflow.RollbackDeploymentFilesActivityName})
	generalWorker.RegisterActivityWithOptions(filesActivities.CommitFilesResult, sdkactivity.RegisterOptions{Name: sharedworkflow.CommitFilesResultActivityName})

	// deploy の ConfigMap のマウント(共有の ConfigMap の版を固定してマウントする)の反映の Workflow と Activity を登録する。
	generalWorker.RegisterWorkflowWithOptions(workflow.ApplyDeployConfigMapMountsWorkflow, sdkworkflow.RegisterOptions{Name: sharedworkflow.ApplyDeployConfigMapMountsWorkflowName})
	configMapMountsActivities := activity.NewApplyDeployConfigMapMountsActivities(repository.NewDeployRepository(appDatabase), repository.NewConfigMapRepository(appDatabase), operators.deploymentOperator, operators.configMapRevisionOperator, appLogger)
	generalWorker.RegisterActivityWithOptions(configMapMountsActivities.ApplyPendingConfigMapMounts, sdkactivity.RegisterOptions{Name: sharedworkflow.ApplyPendingConfigMapMountsActivityName})
	generalWorker.RegisterActivityWithOptions(configMapMountsActivities.RollbackDeploymentConfigMapMounts, sdkactivity.RegisterOptions{Name: sharedworkflow.RollbackDeploymentConfigMapMountsActivityName})
	generalWorker.RegisterActivityWithOptions(configMapMountsActivities.CommitConfigMapMountsResult, sdkactivity.RegisterOptions{Name: sharedworkflow.CommitConfigMapMountsResultActivityName})
	generalWorker.RegisterActivityWithOptions(configMapMountsActivities.CleanupConfigMapRevisions, sdkactivity.RegisterOptions{Name: sharedworkflow.CleanupConfigMapRevisionsActivityName})

	appLogger.Info("ワーカーを起動する", "task_queue", sharedworkflow.TaskQueueGeneral, "temporal", workerConfig.TemporalAddress)
	if runError := generalWorker.Run(worker.InterruptCh()); runError != nil {
		return fmt.Errorf("ワーカーが異常終了しました: %w", runError)
	}
	appLogger.Info("ワーカーを停止した")
	return nil
}

// clusterOperators は、k8s を操作する役の一式。
type clusterOperators struct {
	// namespaceDeleter は、プロジェクトの namespace を削除する役。
	namespaceDeleter activity.NamespaceDeleter

	// deploymentOperator は、namespace と Deployment を作る・確かめる役。
	deploymentOperator activity.DeploymentOperator
	// serviceOperator は、k8s の Service を作る・削除する役。
	serviceOperator activity.ServiceOperator

	// volumeOperator は、PVC を作る・拡張する・削除する役。
	volumeOperator activity.VolumeOperator

	// configMapOperator は、deploy のファイルの置き場(ConfigMap)を作る・削除する役。
	configMapOperator activity.ConfigMapOperator

	// configMapRevisionOperator は、共有の ConfigMap の版(immutable な ConfigMap)を作る・削除する役。
	configMapRevisionOperator activity.ConfigMapRevisionOperator

	// ingressOperator は、Traefik の IngressRoute を作る・削除する役。
	ingressOperator activity.IngressOperator
}

// chooseClusterOperators は、k8s への接続の設定に合わせて、k8s を操作する役を選ぶ。
//
// 選び方:
//   - 接続の方法が none: k8s につながず、何も操作せずログだけを出す仮の実装(開発用)。
//   - それ以外: shared/k8s で作った k8s のクライアントで、実際に操作する実装。
//     クライアントは1つだけ作り、全ての役で共有する。
//
// 引数:
//   - kubernetesSettings: k8s への接続の設定。
//   - operatorSettings: k8s を操作する役の設定(RuntimeClass、外からの通信を中継する namespace、除くネットワーク)。
//   - ingressSettings: Traefik の IngressRoute の apiVersion と entryPoint。
//   - volumeSettings: PVC の StorageClass とアクセスモード。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - k8s を操作する役の一式。
//   - k8s への接続の設定を読めなかったときのエラー。
func chooseClusterOperators(kubernetesSettings k8s.Settings, operatorSettings cluster.OperatorSettings, ingressSettings cluster.IngressRouteSettings, volumeSettings cluster.VolumeSettings, appLogger *slog.Logger) (clusterOperators, error) {
	// k8s のクライアントを作る(クラスタへの通信は、最初の操作まで行われない)。
	clientset, clientsetError := k8s.NewClientset(kubernetesSettings)

	// 接続しない設定のときは、起動のログでそうなっていることが分かるようにして、仮の実装を使う。
	if errors.Is(clientsetError, k8s.ErrNotConnected) {
		appLogger.Warn("KUBERNETES_ACCESS=none のため、k8s は操作されません(namespace は削除されず、deploy は起動を確かめずに active になります。開発用)")
		return clusterOperators{
			namespaceDeleter:          activity.NewLoggingNamespaceDeleter(appLogger),
			deploymentOperator:        activity.NewLoggingDeploymentOperator(appLogger),
			serviceOperator:           activity.NewLoggingServiceOperator(appLogger),
			ingressOperator:           activity.NewLoggingIngressOperator(appLogger),
			volumeOperator:            activity.NewLoggingVolumeOperator(appLogger),
			configMapOperator:         activity.NewLoggingConfigMapOperator(appLogger),
			configMapRevisionOperator: activity.NewLoggingConfigMapRevisionOperator(appLogger),
		}, nil
	}
	// それ以外のエラーは、起動を止める。
	if clientsetError != nil {
		return clusterOperators{}, clientsetError
	}
	// CRD を操作する動的クライアントを作り、IngressRoute を操作する役を作る。
	dynamicClient, dynamicError := k8s.NewDynamicClient(kubernetesSettings)
	if dynamicError != nil {
		return clusterOperators{}, dynamicError
	}
	ingressOperator, ingressError := cluster.NewKubernetesIngressOperator(dynamicClient, ingressSettings)
	if ingressError != nil {
		return clusterOperators{}, ingressError
	}
	// 本物のクライアントで、実際に操作する実装を使う。
	return clusterOperators{
		namespaceDeleter:          cluster.NewKubernetesNamespaceDeleter(clientset),
		deploymentOperator:        cluster.NewKubernetesDeploymentOperator(clientset, operatorSettings),
		serviceOperator:           cluster.NewKubernetesServiceOperator(clientset),
		ingressOperator:           ingressOperator,
		volumeOperator:            cluster.NewKubernetesVolumeOperator(clientset, volumeSettings),
		configMapOperator:         cluster.NewKubernetesConfigMapOperator(clientset),
		configMapRevisionOperator: cluster.NewKubernetesConfigMapRevisionOperator(clientset),
	}, nil
}
