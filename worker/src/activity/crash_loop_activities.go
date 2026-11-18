package activity

import (
	"context"
	"log/slog"
	"time"

	"go.temporal.io/sdk/temporal"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// CrashLoopStore は、CrashLoopActivities が必要とする、deploy の保存先の操作。
//
// 実体は shared/repository の DeployRepository。
type CrashLoopStore interface {
	// FindForWorker は、持ち主を問わずに、deploy を1件取得する。
	FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error)

	// FindSpec は、スペックの名前から、CPU とメモリの大きさを取得する。
	FindSpec(requestContext context.Context, specName string) (model.DeploySpec, error)

	// BeginCrashLoopStop は、active の deploy を applying にして、止める理由のイベントを記録する。始めたかを返す。
	BeginCrashLoopStop(requestContext context.Context, deployID string, podUID string, podName string, reason string, message string, occurredAt time.Time) (bool, error)

	// FinishCrashLoopStop は、applying の deploy を failed にする。
	FinishCrashLoopStop(requestContext context.Context, deployID string) error
}

// CrashLoopActivities は、異常な状態が続く deploy を止める Workflow が呼ぶ Activity をまとめる。
type CrashLoopActivities struct {
	crashLoopStore     CrashLoopStore
	deploymentOperator DeploymentOperator
	appLogger          *slog.Logger
	// now は、現在の時刻を返す。テストで差し替える。
	now func() time.Time
}

// NewCrashLoopActivities は、CrashLoopActivities を作る。
//
// 引数:
//   - crashLoopStore: deploy の保存先。
//   - deploymentOperator: k8s の Deployment を操作するもの。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した CrashLoopActivities。
func NewCrashLoopActivities(crashLoopStore CrashLoopStore, deploymentOperator DeploymentOperator, appLogger *slog.Logger) *CrashLoopActivities {
	// 依存するものを持たせて返す。
	return &CrashLoopActivities{crashLoopStore: crashLoopStore, deploymentOperator: deploymentOperator, appLogger: appLogger, now: time.Now}
}

// BeginCrashLoopStop は、止める準備として、deploy を applying にし、止める理由のイベントを記録する。
//
// deploy が active でない(別の処理が進んでいる、または、すでに止まっている)ときは、何もせず、
// Started を false にして返す。Workflow は、そのまま終わる。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 止める deploy と理由。
//
// 戻り値:
//   - 準備ができたか。
//   - データベースのエラー。
func (crashLoopActivities *CrashLoopActivities) BeginCrashLoopStop(requestContext context.Context, input workflow.BeginCrashLoopStopInput) (workflow.BeginCrashLoopStopResult, error) {
	// 運用で気づけるよう、警告で残す。
	crashLoopActivities.appLogger.Warn("異常な状態が続いているので、deploy を止める準備をする",
		"deploy_id", input.Stop.DeployID, "pod", input.Stop.PodName, "reason", input.Stop.Reason, "message", input.Stop.Message,
		"activity", workflow.BeginCrashLoopStopActivityName)
	// 状態の変更と、イベントの記録は、保存先に任せる。
	started, beginError := crashLoopActivities.crashLoopStore.BeginCrashLoopStop(requestContext, input.Stop.DeployID, input.Stop.PodUID,
		input.Stop.PodName, input.Stop.Reason, input.Stop.Message, crashLoopActivities.now())
	return workflow.BeginCrashLoopStopResult{Started: started}, beginError
}

// ScaleToZero は、k8s の Deployment のレプリカを 0 にして、コンテナの再起動を止める。
//
// DB の replicas(希望する数)は変えない。Deployment の内容は、現在の値(起動設定の pending は使わない)で組み立て、
// レプリカだけを 0 にする。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: レプリカを 0 にする deploy の ID。
//
// 戻り値:
//   - deploy を読めなかったとき、内容が壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (crashLoopActivities *CrashLoopActivities) ScaleToZero(requestContext context.Context, input workflow.ScaleToZeroInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	crashLoopActivities.appLogger.Warn("Deployment のレプリカを 0 にする",
		"deploy_id", input.DeployID, "activity", workflow.ScaleToZeroActivityName)

	// DB から deploy とスペックを読む。
	deploy, findError := crashLoopActivities.crashLoopStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}
	spec, specError := crashLoopActivities.crashLoopStore.FindSpec(requestContext, deploy.Spec)
	if specError != nil {
		return specError
	}
	// 現在の値から Deployment の内容を組み立て、レプリカだけを 0 にする。
	definition, definitionError := newDeploymentDefinition(deploy, spec)
	if definitionError != nil {
		return temporal.NewNonRetryableApplicationError(definitionError.Error(), "InvalidDeployment", definitionError)
	}
	definition.Replicas = 0
	// 更新は、k8s を操作するものに任せる(世代は、待たないので使わない)。
	_, applyError := crashLoopActivities.deploymentOperator.ApplyDeployment(requestContext, definition)
	return applyError
}

// FinishCrashLoopStop は、止め終えた deploy を failed にする。
//
// applying のときだけ変える(別の処理が先に変えていたら、何もしない)。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 止め終えた deploy の ID。
//
// 戻り値:
//   - データベースのエラー。
func (crashLoopActivities *CrashLoopActivities) FinishCrashLoopStop(requestContext context.Context, input workflow.FinishCrashLoopStopInput) error {
	// 結果をログに残す。
	crashLoopActivities.appLogger.Warn("異常な状態が続いた deploy を止めた(failed にする)",
		"deploy_id", input.DeployID, "activity", workflow.FinishCrashLoopStopActivityName)
	// 状態の変更は、保存先に任せる。
	return crashLoopActivities.crashLoopStore.FinishCrashLoopStop(requestContext, input.DeployID)
}
