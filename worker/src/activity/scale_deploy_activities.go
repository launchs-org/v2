package activity

import (
	"context"
	"log/slog"

	"go.temporal.io/sdk/temporal"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// ScaleStore は、ScaleDeployActivities が必要とする、deploy の保存先の操作。
//
// 実体は shared/repository の DeployRepository。
type ScaleStore interface {
	// FindForWorker は、持ち主を問わずに、deploy を1件取得する。
	FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error)

	// FindSpec は、スペックの名前から、CPU とメモリの大きさを取得する。
	FindSpec(requestContext context.Context, specName string) (model.DeploySpec, error)

	// UpdateStatus は、期待する状態のときだけ、状態を変える。変えられたかを返す。
	UpdateStatus(requestContext context.Context, deployID string, fromStatus string, toStatus string) (bool, error)
}

// ScaleDeployActivities は、スケーリングの Workflow が呼ぶ Activity をまとめる。
//
// 起動を待つ Activity は、deploy の作成と同じもの(CreateDeployActivities.WaitDeploymentReady)を使う。
type ScaleDeployActivities struct {
	scaleStore         ScaleStore
	deploymentOperator DeploymentOperator
	appLogger          *slog.Logger
}

// NewScaleDeployActivities は、ScaleDeployActivities を作る。
//
// 引数:
//   - scaleStore: deploy の保存先。
//   - deploymentOperator: k8s の Deployment を操作するもの。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した ScaleDeployActivities。
func NewScaleDeployActivities(scaleStore ScaleStore, deploymentOperator DeploymentOperator, appLogger *slog.Logger) *ScaleDeployActivities {
	// 依存するものを持たせて返す。
	return &ScaleDeployActivities{scaleStore: scaleStore, deploymentOperator: deploymentOperator, appLogger: appLogger}
}

// ApplyScale は、DB の現在の値(新しい replicas を含む)で、k8s の Deployment を更新する。
//
// pending の起動設定は使わない(スケーリングで、未適用の設定が反映されないようにするため)。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: Deployment を更新する deploy の ID。
//
// 戻り値:
//   - deploy を読めなかったとき、内容が壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (scaleDeployActivities *ScaleDeployActivities) ApplyScale(requestContext context.Context, input workflow.ApplyScaleInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	scaleDeployActivities.appLogger.Info("レプリカ数を Deployment に反映する",
		"deploy_id", input.DeployID, "activity", workflow.ApplyScaleActivityName)

	// DB から deploy とスペックを読む。
	deploy, findError := scaleDeployActivities.scaleStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}
	spec, specError := scaleDeployActivities.scaleStore.FindSpec(requestContext, deploy.Spec)
	if specError != nil {
		return specError
	}
	// 現在の値から、Deployment の内容を組み立てる。壊れていたら、何度やっても同じなのでリトライしない。
	definition, definitionError := newDeploymentDefinition(deploy, spec)
	if definitionError != nil {
		return temporal.NewNonRetryableApplicationError(definitionError.Error(), "InvalidDeployment", definitionError)
	}
	// 更新は、k8s を操作するものに任せる。
	return scaleDeployActivities.deploymentOperator.ApplyDeployment(requestContext, definition)
}

// MarkScaleResult は、スケーリングの結果を、deploy の状態として DB に記録する。
//
// applying の deploy だけを変える(別の処理が先に変えていたら、上書きしない)。
//   - 成功: active(replicas が 0 なら scaled_zero)
//   - 失敗: failed
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 結果を記録する deploy の ID と、成功したか。
//
// 戻り値:
//   - deploy を読めなかったとき、または状態を更新できなかったときのエラー。
func (scaleDeployActivities *ScaleDeployActivities) MarkScaleResult(requestContext context.Context, input workflow.MarkScaleResultInput) error {
	// 結果をログに残す(失敗は、運用で気づけるよう警告にする)。
	if input.Succeeded {
		scaleDeployActivities.appLogger.Info("スケーリングに成功した", "deploy_id", input.DeployID, "activity", workflow.MarkScaleResultActivityName)
	} else {
		scaleDeployActivities.appLogger.Warn("スケーリングに失敗した", "deploy_id", input.DeployID, "activity", workflow.MarkScaleResultActivityName)
	}
	// 記録する状態を決める。成功なら、replicas に合わせて active か scaled_zero にする。
	targetStatus := model.DeployStatusFailed
	if input.Succeeded {
		deploy, findError := scaleDeployActivities.scaleStore.FindForWorker(requestContext, input.DeployID)
		if findError != nil {
			return findError
		}
		targetStatus = model.DeployStatusActive
		if deploy.Replicas == 0 {
			targetStatus = model.DeployStatusScaledZero
		}
	}
	// applying のときだけ、状態を変える(変えられなくても、別の処理が先に決めたということなので、エラーにしない)。
	_, updateError := scaleDeployActivities.scaleStore.UpdateStatus(requestContext, input.DeployID, model.DeployStatusApplying, targetStatus)
	return updateError
}
