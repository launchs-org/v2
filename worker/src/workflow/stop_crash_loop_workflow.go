package workflow

import (
	"errors"
	"fmt"

	"go.temporal.io/sdk/temporal"
	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// StopCrashLoopWorkflow は、異常な状態(クラッシュなど)が続く deploy を止める。
//
// 処理の順序:
//  1. 止める準備をする(active の deploy を applying にして、理由のイベントを記録する)。
//     active でなかったとき(別の処理が進んでいる)は、何もせずに終わる
//  2. k8s の Deployment のレプリカを 0 にする(コンテナの再起動が止まる。DB の replicas は変えない)
//  3. deploy を failed にする。2 に失敗しても、applying のまま残さないよう、failed にする
//
// 2 は、k8s の一時的な障害なら、リトライの期限(2分)までリトライする。3 は、キャンセルされていても実行できるよう、
// 切り離した context で、補償と同じ期限(10分)までリトライする。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 止める deploy と理由。
//
// 戻り値:
//   - レプリカを 0 にできなかったとき、または failed にできなかったときのエラー。止める必要が無ければ nil。
func StopCrashLoopWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.StopCrashLoopInput) error {
	// ===== 1. 止める準備 =====
	retryPolicy := &temporal.RetryPolicy{
		MaximumInterval:    activityMaximumRetryInterval,
		InitialInterval:    activityInitialRetryInterval,
		BackoffCoefficient: 2,
	}
	beginContext := sdkworkflow.WithActivityOptions(workflowContext, sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.DeleteProjectActivityTimeout,
		ScheduleToCloseTimeout: sharedworkflow.DeployInfrastructureRetryWindow,
		RetryPolicy:            retryPolicy,
	})
	var begun sharedworkflow.BeginCrashLoopStopResult
	if beginError := sdkworkflow.ExecuteActivity(beginContext, sharedworkflow.BeginCrashLoopStopActivityName,
		sharedworkflow.BeginCrashLoopStopInput{Stop: input}).Get(beginContext, &begun); beginError != nil {
		return fmt.Errorf("止める準備ができませんでした: %w", beginError)
	}
	// active でなかったときは、他の処理に任せて、何もせずに終わる。
	if !begun.Started {
		return nil
	}

	// ===== 2. レプリカを 0 にする =====
	scaleError := sdkworkflow.ExecuteActivity(beginContext, sharedworkflow.ScaleToZeroActivityName,
		sharedworkflow.ScaleToZeroInput{DeployID: input.DeployID}).Get(beginContext, nil)
	if scaleError != nil {
		scaleError = fmt.Errorf("Deployment のレプリカを 0 にできませんでした: %w", scaleError)
	}

	// ===== 3. failed にする(キャンセルされていても実行できるよう、切り離した context で) =====
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	finishContext := sdkworkflow.WithActivityOptions(disconnectedContext, sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.DeleteProjectActivityTimeout,
		ScheduleToCloseTimeout: compensationRetryWindow,
		RetryPolicy:            retryPolicy,
	})
	finishError := sdkworkflow.ExecuteActivity(finishContext, sharedworkflow.FinishCrashLoopStopActivityName,
		sharedworkflow.FinishCrashLoopStopInput{DeployID: input.DeployID}).Get(finishContext, nil)
	// failed にもできなかったときは、両方の原因を返す(deploy は applying のまま残る)。
	if finishError != nil {
		return errors.Join(scaleError, fmt.Errorf("failed にできませんでした: %w", finishError))
	}
	return scaleError
}
