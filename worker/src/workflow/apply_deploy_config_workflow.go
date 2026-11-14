package workflow

import (
	"errors"
	"fmt"

	"go.temporal.io/sdk/temporal"
	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// ApplyDeployConfigWorkflow は、pending の起動設定を k8s に反映し、起動し直すのを待って、結果を確定する。
//
// 処理の順序(仕様どおり、実際の反映を先に試み、結果が決まってから DB の正式な値を更新する):
//  1. pending の値で Deployment を更新する(コンテナが入れ替わる)
//  2. 起動し終える(Ready)まで待つ(最長 300 秒)
//  3. 結果を確定する(成功なら、正式な値を pending の内容にして pending を空にし、active にする。
//     失敗なら、Deployment を元の値に戻した上で、正式な値と pending は変えずに failed にする)
//
// 1 は、k8s の一時的な障害なら、リトライの期限(2分)までリトライする。
// 2 は、300 秒(仕様のタイムアウト)で Ready にならなければ失敗とする。ワーカーが入れ替わっても、期限内なら引き継ぐ。
// 失敗したときは、k8s を元の値に戻す(リトライの期限は 10 分)ので、正式な値は常に、動いている値を表す。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 反映する deploy の ID。
//
// 戻り値:
//   - 反映に失敗したときのエラー(結果は確定してある)。確定もできなかったときは、両方の原因を含む。
func ApplyDeployConfigWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.ApplyDeployConfigInput) error {
	// 反映の手順(Deployment の更新、待機)を実行する。
	applyError := runConfigApply(workflowContext, input)

	// 失敗したときは、k8s の Deployment を、元の値に戻す。戻せなくても、結果の確定は続ける。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	if applyError != nil {
		rollbackContext := sdkworkflow.WithActivityOptions(disconnectedContext, sdkworkflow.ActivityOptions{
			StartToCloseTimeout:    sharedworkflow.DeleteProjectActivityTimeout,
			ScheduleToCloseTimeout: compensationRetryWindow,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumInterval:    activityMaximumRetryInterval,
				InitialInterval:    activityInitialRetryInterval,
				BackoffCoefficient: 2,
			},
		})
		rollbackError := sdkworkflow.ExecuteActivity(rollbackContext, sharedworkflow.RollbackDeploymentConfigActivityName,
			sharedworkflow.RollbackDeploymentConfigInput{DeployID: input.DeployID}).Get(rollbackContext, nil)
		if rollbackError != nil {
			applyError = errors.Join(applyError, fmt.Errorf("Deployment を元の値に戻せませんでした: %w", rollbackError))
		}
	}

	// 結果を DB に確定する。Workflow がキャンセルされていても確定できるよう、切り離した context で呼ぶ。
	commitContext := sdkworkflow.WithActivityOptions(disconnectedContext, sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.DeleteProjectActivityTimeout,
		ScheduleToCloseTimeout: compensationRetryWindow,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumInterval:    activityMaximumRetryInterval,
			InitialInterval:    activityInitialRetryInterval,
			BackoffCoefficient: 2,
		},
	})
	commitError := sdkworkflow.ExecuteActivity(commitContext, sharedworkflow.CommitConfigResultActivityName,
		sharedworkflow.CommitConfigResultInput{DeployID: input.DeployID, Succeeded: applyError == nil}).Get(commitContext, nil)

	// 確定に失敗したときは、反映の失敗と合わせて、両方の原因を返す(deploy は applying のまま残る)。
	if commitError != nil {
		return errors.Join(applyError, fmt.Errorf("起動設定の結果を確定できませんでした: %w", commitError))
	}
	return applyError
}

// runConfigApply は、Deployment を pending の値で更新し、起動し終えるのを待つ。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 反映する deploy の ID。
//
// 戻り値:
//   - 更新に失敗したとき、または、300 秒までに Ready にならなかったときのエラー。
func runConfigApply(workflowContext sdkworkflow.Context, input sharedworkflow.ApplyDeployConfigInput) error {
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
	// 1. pending の値で、Deployment を更新する。
	if applyError := sdkworkflow.ExecuteActivity(infrastructureContext, sharedworkflow.ApplyPendingConfigActivityName,
		sharedworkflow.ApplyPendingConfigInput{DeployID: input.DeployID}).Get(infrastructureContext, nil); applyError != nil {
		return fmt.Errorf("Deployment を更新できませんでした: %w", applyError)
	}

	// 2. 起動し直し終えるのを待つ。deploy の作成と同じ Activity を使い、300秒を全体の期限にする。
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
		return fmt.Errorf("Deployment が起動し終えませんでした: %w", waitError)
	}
	return nil
}
