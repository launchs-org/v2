package activity

import (
	"context"
	"log/slog"

	"go.temporal.io/sdk/temporal"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// ConfigApplyStore は、ApplyDeployConfigActivities が必要とする、deploy の保存先の操作。
//
// 実体は shared/repository の DeployRepository。
type ConfigApplyStore interface {
	// FindForWorker は、持ち主を問わずに、deploy を1件取得する。
	FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error)

	// FindSpec は、スペックの名前から、CPU とメモリの大きさを取得する。
	FindSpec(requestContext context.Context, specName string) (model.DeploySpec, error)

	// CommitConfigResult は、起動設定の反映の結果を確定する。確定したかを返す。
	CommitConfigResult(requestContext context.Context, deployID string, succeeded bool) (bool, error)
}

// ApplyDeployConfigActivities は、起動設定の反映の Workflow が呼ぶ Activity をまとめる。
//
// 起動を待つ Activity は、deploy の作成と同じもの(CreateDeployActivities.WaitDeploymentReady)を使う。
type ApplyDeployConfigActivities struct {
	configApplyStore   ConfigApplyStore
	deploymentOperator DeploymentOperator
	appLogger          *slog.Logger
}

// NewApplyDeployConfigActivities は、ApplyDeployConfigActivities を作る。
//
// 引数:
//   - configApplyStore: deploy の保存先。
//   - deploymentOperator: k8s の Deployment を操作するもの。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した ApplyDeployConfigActivities。
func NewApplyDeployConfigActivities(configApplyStore ConfigApplyStore, deploymentOperator DeploymentOperator, appLogger *slog.Logger) *ApplyDeployConfigActivities {
	// 依存するものを持たせて返す。
	return &ApplyDeployConfigActivities{configApplyStore: configApplyStore, deploymentOperator: deploymentOperator, appLogger: appLogger}
}

// ApplyPendingConfig は、pending の起動設定を使って、k8s の Deployment を更新する。
//
// 正式な値(env、command、args)ではなく、pending の値を使う。Pod のテンプレートが変わるので、
// k8s がコンテナを入れ替える(再起動する)。DB の正式な値は、結果が確定するまで変えない。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: Deployment を更新する deploy の ID。
//
// 戻り値:
//   - deploy を読めなかったとき、適用する値が無い・壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (applyDeployConfigActivities *ApplyDeployConfigActivities) ApplyPendingConfig(requestContext context.Context, input workflow.ApplyPendingConfigInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	applyDeployConfigActivities.appLogger.Info("pending の起動設定で Deployment を更新する",
		"deploy_id", input.DeployID, "activity", workflow.ApplyPendingConfigActivityName)

	// DB から deploy とスペックを読む。
	deploy, findError := applyDeployConfigActivities.configApplyStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}
	spec, specError := applyDeployConfigActivities.configApplyStore.FindSpec(requestContext, deploy.Spec)
	if specError != nil {
		return specError
	}
	// 適用する値が無いのは、何度やっても同じなのでリトライしない。
	if len(deploy.PendingEnv) == 0 {
		return temporal.NewNonRetryableApplicationError("適用する起動設定がありません", "NoPendingConfig", nil)
	}

	// 起動設定を pending の値に差し替えた deploy から、Deployment の内容を組み立てる。
	pendingDeploy := deploy
	pendingDeploy.Env = deploy.PendingEnv
	pendingDeploy.Command = deploy.PendingCommand
	pendingDeploy.Args = deploy.PendingArgs
	definition, definitionError := newDeploymentDefinition(pendingDeploy, spec)
	if definitionError != nil {
		return temporal.NewNonRetryableApplicationError(definitionError.Error(), "InvalidDeployment", definitionError)
	}
	// 更新は、k8s を操作するものに任せる。
	return applyDeployConfigActivities.deploymentOperator.ApplyDeployment(requestContext, definition)
}

// CommitConfigResult は、反映の結果を DB に確定する。
//
// 正式な値を pending の内容で上書きし、pending を空に戻し、status を active(または scaled_zero)か failed にする。
// すでに確定している(applying でない)ときは、何もしない。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 結果を確定する deploy の ID と、成功したか。
//
// 戻り値:
//   - 確定できなかったときのデータベースのエラー。
func (applyDeployConfigActivities *ApplyDeployConfigActivities) CommitConfigResult(requestContext context.Context, input workflow.CommitConfigResultInput) error {
	// 結果をログに残す(失敗は、運用で気づけるよう警告にする)。
	if input.Succeeded {
		applyDeployConfigActivities.appLogger.Info("起動設定の反映に成功した", "deploy_id", input.DeployID, "activity", workflow.CommitConfigResultActivityName)
	} else {
		applyDeployConfigActivities.appLogger.Warn("起動設定の反映に失敗した", "deploy_id", input.DeployID, "activity", workflow.CommitConfigResultActivityName)
	}
	// 確定を、保存先に任せる(確定済みなら何もしない。エラーでないので、結果は使わない)。
	_, commitError := applyDeployConfigActivities.configApplyStore.CommitConfigResult(requestContext, input.DeployID, input.Succeeded)
	return commitError
}
