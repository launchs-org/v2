package activity

import (
	"context"
	"log/slog"

	"go.temporal.io/sdk/temporal"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// FilesApplyStore は、ファイルの反映の Activity が使う、データベースの操作。
type FilesApplyStore interface {
	// FindForWorker は、持ち主を問わずに、deploy を1件取得する。
	FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error)

	// FindSpec は、スペックの名前から、大きさを取得する。
	FindSpec(requestContext context.Context, specName string) (model.DeploySpec, error)

	// CommitFilesResult は、ファイルの反映の結果を確定する(applying の deploy だけ)。確定できたかを返す。
	CommitFilesResult(requestContext context.Context, deployID string, succeeded bool) (bool, error)
}

// ApplyDeployFilesActivities は、ファイルの反映の Workflow が使う Activity をまとめたもの。
type ApplyDeployFilesActivities struct {
	filesStore         FilesApplyStore
	deploymentOperator DeploymentOperator
	configMapOperator  ConfigMapOperator
	appLogger          *slog.Logger
}

// NewApplyDeployFilesActivities は、ApplyDeployFilesActivities を作る。
//
// 引数:
//   - filesStore: deploy に関するデータベースの操作。
//   - deploymentOperator: k8s の Deployment を操作するもの。
//   - configMapOperator: k8s の ConfigMap を操作するもの。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した ApplyDeployFilesActivities。
func NewApplyDeployFilesActivities(filesStore FilesApplyStore, deploymentOperator DeploymentOperator, configMapOperator ConfigMapOperator, appLogger *slog.Logger) *ApplyDeployFilesActivities {
	// 受け取ったものを持たせる。
	return &ApplyDeployFilesActivities{filesStore: filesStore, deploymentOperator: deploymentOperator, configMapOperator: configMapOperator, appLogger: appLogger}
}

// ApplyPendingFiles は、namespace を用意し、pending のファイルで ConfigMap を作り(または更新し)、Deployment を更新する。
//
// ファイルが 0 件になる(全て外す)ときは、Deployment からファイルを外してから、ConfigMap を削除する(先に消すと、動いている Pod が、
// ConfigMap を見失うため)。起動引数や環境変数は、現在の値を使う(pending の起動設定は使わない)。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 更新する deploy の ID。
//
// 戻り値:
//   - 更新後の Deployment の世代(起動を待つときの、最小の世代として使う)。
//   - deploy を読めなかったとき、適用するファイルが無い・壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (applyDeployFilesActivities *ApplyDeployFilesActivities) ApplyPendingFiles(requestContext context.Context, input workflow.ApplyPendingFilesInput) (int64, error) {
	// どの Activity が何のために動いているかを、ログに残す(ファイルの内容は、秘密を含みうるので、出さない)。
	applyDeployFilesActivities.appLogger.Info("pending のファイルで Deployment を更新する", "deploy_id", input.DeployID, "activity", workflow.ApplyPendingFilesActivityName)
	// DB から deploy とスペックを読む。
	deploy, findError := applyDeployFilesActivities.filesStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return 0, findError
	}
	spec, specError := applyDeployFilesActivities.filesStore.FindSpec(requestContext, deploy.Spec)
	if specError != nil {
		return 0, specError
	}
	// 適用するファイルが無いのは、何度やっても同じなので、リトライしない。
	if !deploy.HasPendingFiles() {
		return 0, temporal.NewNonRetryableApplicationError("適用するファイルがありません", "NoPendingFiles", nil)
	}
	pendingFiles, parseError := model.ParseFiles(deploy.PendingFiles)
	if parseError != nil {
		return 0, temporal.NewNonRetryableApplicationError("ファイルが壊れています", "InvalidFiles", parseError)
	}
	// namespace を用意する(まだ deploy が起動していないプロジェクトでは、無いことがある)。
	if namespaceError := applyDeployFilesActivities.deploymentOperator.EnsureNamespace(requestContext, deploy.ProjectID); namespaceError != nil {
		return 0, markNonRetryable(namespaceError)
	}
	// ファイルがあるときは、先に ConfigMap を用意する(Deployment が、ConfigMap を参照できるように)。
	if len(pendingFiles) > 0 {
		if configMapError := applyDeployFilesActivities.configMapOperator.ApplyConfigMap(requestContext, deploy.ProjectID, deploy.ID, newConfigMapData(pendingFiles)); configMapError != nil {
			return 0, markNonRetryable(configMapError)
		}
	}
	// pending のファイルを使った Deployment の内容を組み立てて、k8s を操作するものに任せる。
	pendingDeploy := deploy
	pendingDeploy.Files = deploy.PendingFiles
	definition, definitionError := newDeploymentDefinition(pendingDeploy, spec)
	if definitionError != nil {
		return 0, temporal.NewNonRetryableApplicationError(definitionError.Error(), "InvalidDeployment", definitionError)
	}
	generation, applyError := applyDeployFilesActivities.deploymentOperator.ApplyDeployment(requestContext, definition)
	if applyError != nil {
		return 0, applyError
	}
	// ファイルを全て外したときは、Deployment が、ConfigMap を使わなくなってから、ConfigMap を削除する。
	if len(pendingFiles) == 0 {
		if deleteError := applyDeployFilesActivities.configMapOperator.DeleteConfigMap(requestContext, deploy.ProjectID, deploy.ID); deleteError != nil {
			return 0, markNonRetryable(deleteError)
		}
	}
	return generation, nil
}

// RollbackDeploymentFiles は、反映に失敗したとき、ConfigMap と Deployment を、現在の(動いていた)ファイルに戻す。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 元に戻す deploy の ID。
//
// 戻り値:
//   - deploy を読めなかったとき、値が壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (applyDeployFilesActivities *ApplyDeployFilesActivities) RollbackDeploymentFiles(requestContext context.Context, input workflow.RollbackDeploymentFilesInput) error {
	// 運用で気づけるよう、警告で残す。
	applyDeployFilesActivities.appLogger.Warn("反映に失敗したので、ConfigMap と Deployment を元のファイルに戻す", "deploy_id", input.DeployID, "activity", workflow.RollbackDeploymentFilesActivityName)
	// DB から deploy とスペックを読み、現在の値で、Deployment の内容を組み立てる。
	deploy, findError := applyDeployFilesActivities.filesStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}
	spec, specError := applyDeployFilesActivities.filesStore.FindSpec(requestContext, deploy.Spec)
	if specError != nil {
		return specError
	}
	currentFiles, parseError := model.ParseFiles(deploy.Files)
	if parseError != nil {
		return temporal.NewNonRetryableApplicationError("ファイルが壊れています", "InvalidFiles", parseError)
	}
	// 元のファイルがあるときは、ConfigMap を、元の内容に戻す(無いときは、Deployment を戻したあと、消す)。
	if len(currentFiles) > 0 {
		if configMapError := applyDeployFilesActivities.configMapOperator.ApplyConfigMap(requestContext, deploy.ProjectID, deploy.ID, newConfigMapData(currentFiles)); configMapError != nil {
			return markNonRetryable(configMapError)
		}
	}
	definition, definitionError := newDeploymentDefinition(deploy, spec)
	if definitionError != nil {
		return temporal.NewNonRetryableApplicationError(definitionError.Error(), "InvalidDeployment", definitionError)
	}
	// k8s を操作するものに任せる。
	if _, applyError := applyDeployFilesActivities.deploymentOperator.ApplyDeployment(requestContext, definition); applyError != nil {
		return markNonRetryable(applyError)
	}
	// 元のファイルが無かったときは、反映で作った ConfigMap を、消す。
	if len(currentFiles) == 0 {
		return markNonRetryable(applyDeployFilesActivities.configMapOperator.DeleteConfigMap(requestContext, deploy.ProjectID, deploy.ID))
	}
	return nil
}

// CommitFilesResult は、ファイルの反映の結果を DB に確定する。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 結果を確定する deploy の ID と、成功したか。
//
// 戻り値:
//   - 確定できなかったときのデータベースのエラー。
func (applyDeployFilesActivities *ApplyDeployFilesActivities) CommitFilesResult(requestContext context.Context, input workflow.CommitFilesResultInput) error {
	// 結果をログに残す(失敗は、運用で気づけるよう警告にする)。
	if input.Succeeded {
		applyDeployFilesActivities.appLogger.Info("ファイルの反映に成功した", "deploy_id", input.DeployID, "activity", workflow.CommitFilesResultActivityName)
	} else {
		applyDeployFilesActivities.appLogger.Warn("ファイルの反映に失敗した", "deploy_id", input.DeployID, "activity", workflow.CommitFilesResultActivityName)
	}
	// 確定を、保存先に任せる(確定済みなら何もしない)。
	_, commitError := applyDeployFilesActivities.filesStore.CommitFilesResult(requestContext, input.DeployID, input.Succeeded)
	return commitError
}
