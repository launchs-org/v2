package activity

import (
	"context"
	"log/slog"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/workflow"
)

// DeployDeletionStore は、DeleteDeployActivities が必要とする、deploy の保存先の操作。
//
// 実体は shared/repository の DeployRepository。
type DeployDeletionStore interface {
	// FindForWorker は、持ち主を問わずに、deploy を1件取得する。
	FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error)

	// DeleteRecord は、削除中の deploy を、DB から物理削除する。
	DeleteRecord(requestContext context.Context, deployID string) error

	// MarkDeleteFailed は、削除中の deploy を、delete_failed にする。
	MarkDeleteFailed(requestContext context.Context, deployID string) error
}

// DeleteDeployActivities は、deploy の削除の Workflow が呼ぶ Activity をまとめる。
type DeleteDeployActivities struct {
	deployDeletionStore DeployDeletionStore
	deploymentOperator  DeploymentOperator
	appLogger           *slog.Logger
}

// NewDeleteDeployActivities は、DeleteDeployActivities を作る。
//
// 引数:
//   - deployDeletionStore: deploy の保存先。
//   - deploymentOperator: k8s の Deployment を操作するもの。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した DeleteDeployActivities。
func NewDeleteDeployActivities(deployDeletionStore DeployDeletionStore, deploymentOperator DeploymentOperator, appLogger *slog.Logger) *DeleteDeployActivities {
	// 依存するものを持たせて返す。
	return &DeleteDeployActivities{deployDeletionStore: deployDeletionStore, deploymentOperator: deploymentOperator, appLogger: appLogger}
}

// DeleteDeployment は、deploy に対応する k8s の Deployment を削除する。
//
// 冪等。deploy の行がすでに無いとき(プロジェクトの削除が先に消したなど)は、消すものが無いので、成功にする。
// 名前の形が不正なときは、リトライしても無駄なので、リトライしないエラーにする。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除する deploy の ID。
//
// 戻り値:
//   - deploy を読めなかったとき、k8s への依頼に失敗したとき、名前の形が不正なとき(リトライしない)のエラー。
func (deleteDeployActivities *DeleteDeployActivities) DeleteDeployment(requestContext context.Context, input workflow.DeleteDeploymentInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	deleteDeployActivities.appLogger.Info("Deployment を削除する",
		"deploy_id", input.DeployID, "activity", workflow.DeleteDeploymentActivityName)

	// namespace の名前(プロジェクトの ID)を知るため、deploy を読む。
	deploy, findError := deleteDeployActivities.deployDeletionStore.FindForWorker(requestContext, input.DeployID)
	if apperror.KindOf(findError) == apperror.KindNotFound {
		// すでに行が無いので、消すものも無い。
		return nil
	}
	if findError != nil {
		return findError
	}
	// 削除は、k8s を操作するものに任せる。名前の形の不正は、リトライしない印を付ける。
	return markNonRetryable(deleteDeployActivities.deploymentOperator.DeleteDeployment(requestContext, deploy.ProjectID, deploy.ID))
}

// DeleteDeployRecord は、deploy を DB から削除する。
//
// 冪等。削除中でない deploy は消さない(DeployRepository.DeleteRecord が守る)。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除する deploy の ID。
//
// 戻り値:
//   - データベースのエラー。
func (deleteDeployActivities *DeleteDeployActivities) DeleteDeployRecord(requestContext context.Context, input workflow.DeleteDeployRecordInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	deleteDeployActivities.appLogger.Info("deploy のレコードを削除する",
		"deploy_id", input.DeployID, "activity", workflow.DeleteDeployRecordActivityName)
	// 削除は、保存先に任せる。
	return deleteDeployActivities.deployDeletionStore.DeleteRecord(requestContext, input.DeployID)
}

// MarkDeployDeleteFailed は、削除に失敗した deploy を、delete_failed にする(補償)。
//
// 冪等。削除中でなければ、何もしない。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除に失敗した deploy の ID。
//
// 戻り値:
//   - データベースのエラー。
func (deleteDeployActivities *DeleteDeployActivities) MarkDeployDeleteFailed(requestContext context.Context, input workflow.MarkDeployDeleteFailedInput) error {
	// 運用で気づけるよう、警告で残す。
	deleteDeployActivities.appLogger.Warn("deploy の削除に失敗したので delete_failed にする",
		"deploy_id", input.DeployID, "activity", workflow.MarkDeployDeleteFailedActivityName)
	// 状態の変更は、保存先に任せる。
	return deleteDeployActivities.deployDeletionStore.MarkDeleteFailed(requestContext, input.DeployID)
}
