package workflow

import (
	"errors"
	"fmt"

	"go.temporal.io/sdk/temporal"
	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// ScaleDeployWorkflow は、DB に保存した新しい replicas を k8s に反映し、起動し終えるのを待って、結果を記録する。
//
// 処理の順序:
//  1. Deployment を更新する(replicas を変える)
//  2. Ready になるまで待つ(最長 300 秒。0 にするときは、Pod が無くなれば Ready)
//  3. 結果を記録する(成功なら active、replicas が 0 なら scaled_zero、失敗なら failed)
//
// replicas は「希望する数」なので、失敗しても元には戻さない(status が failed になるだけ)。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: スケーリングする deploy の ID。
//
// 戻り値:
//   - 失敗したときのエラー(status は failed にしてある)。記録もできなかったときは、両方の原因を含む。
func ScaleDeployWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.ScaleDeployInput) error {
	// k8s への依頼の方針。期限までは、リトライを続ける。
	infrastructureContext := sdkworkflow.WithActivityOptions(workflowContext, sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.DeleteProjectActivityTimeout,
		ScheduleToCloseTimeout: sharedworkflow.DeployInfrastructureRetryWindow,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumInterval:    activityMaximumRetryInterval,
			InitialInterval:    activityInitialRetryInterval,
			BackoffCoefficient: 2,
		},
	})
	// 1. Deployment を更新し、失敗したら待たない。
	scaleError := sdkworkflow.ExecuteActivity(infrastructureContext, sharedworkflow.ApplyScaleActivityName,
		sharedworkflow.ApplyScaleInput{DeployID: input.DeployID}).Get(infrastructureContext, nil)
	if scaleError != nil {
		scaleError = fmt.Errorf("Deployment を更新できませんでした: %w", scaleError)
	} else {
		// 2. 起動し終える(または Pod が無くなる)のを待つ。作成と同じ Activity で、300秒が全体の期限。
		waitContext := sdkworkflow.WithActivityOptions(workflowContext, sdkworkflow.ActivityOptions{
			StartToCloseTimeout:    sharedworkflow.DeployStartTimeout,
			ScheduleToCloseTimeout: sharedworkflow.DeployStartTimeout,
			HeartbeatTimeout:       sharedworkflow.DeleteProjectActivityHeartbeatTimeout,
			RetryPolicy: &temporal.RetryPolicy{
				InitialInterval:    activityInitialRetryInterval,
				MaximumInterval:    activityMaximumRetryInterval,
				BackoffCoefficient: 2,
			},
		})
		if waitError := sdkworkflow.ExecuteActivity(waitContext, sharedworkflow.WaitDeploymentReadyActivityName,
			sharedworkflow.WaitDeploymentReadyInput{DeployID: input.DeployID}).Get(waitContext, nil); waitError != nil {
			scaleError = fmt.Errorf("Deployment が起動し終えませんでした: %w", waitError)
		}
	}

	// 3. 結果を DB に記録する。キャンセルされていても記録できるよう、切り離した context で呼ぶ。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	recordContext := sdkworkflow.WithActivityOptions(disconnectedContext, sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.DeleteProjectActivityTimeout,
		ScheduleToCloseTimeout: compensationRetryWindow,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumInterval:    activityMaximumRetryInterval,
			InitialInterval:    activityInitialRetryInterval,
			BackoffCoefficient: 2,
		},
	})
	markError := sdkworkflow.ExecuteActivity(recordContext, sharedworkflow.MarkScaleResultActivityName,
		sharedworkflow.MarkScaleResultInput{DeployID: input.DeployID, Succeeded: scaleError == nil}).Get(recordContext, nil)
	// 記録に失敗したときは、スケーリングの失敗と合わせて、両方の原因を返す(deploy は applying のまま残る)。
	if markError != nil {
		return errors.Join(scaleError, fmt.Errorf("スケーリングの結果を記録できませんでした: %w", markError))
	}
	return scaleError
}
