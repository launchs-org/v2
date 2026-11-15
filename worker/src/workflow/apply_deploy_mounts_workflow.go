package workflow

import (
	"errors"
	"fmt"

	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// ApplyDeployMountsWorkflow は、deploy の pending のマウントを、k8s に反映する Workflow。
//
// 流れ: PVC を用意して Deployment を更新 → 起動し終えるのを待つ → 失敗したら、元のマウントに戻す → 結果を確定する。
// 成功ならマウントが pending の内容になり、失敗ならマウントは変わらず failed になる。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 反映する deploy の ID。
//
// 戻り値:
//   - 反映に失敗したとき、または結果を確定できなかったときのエラー。
func ApplyDeployMountsWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.ApplyDeployMountsInput) error {
	// 更新と、起動の待機を行う(失敗しても、すぐには終わらず、後始末へ進む)。
	applyError := runMountsApply(workflowContext, input)

	// 後始末は、Workflow がキャンセルされても実行されるよう、切り離した context で行う。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	compensationContext := sdkworkflow.WithActivityOptions(disconnectedContext, compensationOptions())

	// 失敗したときは、Deployment を、動いていた元のマウントに戻す。
	if applyError != nil {
		rollbackError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.RollbackDeploymentMountsActivityName,
			sharedworkflow.RollbackDeploymentMountsInput{DeployID: input.DeployID}).Get(compensationContext, nil)
		if rollbackError != nil {
			applyError = errors.Join(applyError, fmt.Errorf("Deployment を元のマウントに戻せませんでした: %w", rollbackError))
		}
	}

	// 結果を DB に確定する(成功ならマウントを昇格、失敗なら failed)。
	commitError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.CommitMountsResultActivityName,
		sharedworkflow.CommitMountsResultInput{DeployID: input.DeployID, Succeeded: applyError == nil}).Get(compensationContext, nil)
	if commitError != nil {
		return errors.Join(applyError, fmt.Errorf("マウントの結果を確定できませんでした: %w", commitError))
	}
	return applyError
}

// runMountsApply は、Deployment を pending のマウントで更新し、起動し終えるのを待つ。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 反映する deploy の ID。
//
// 戻り値:
//   - どちらかが期限までに成功しなかったときのエラー。
func runMountsApply(workflowContext sdkworkflow.Context, input sharedworkflow.ApplyDeployMountsInput) error {
	// 1. PVC を用意して、Deployment を更新する(更新後の世代を受け取る)。
	infrastructureContext := sdkworkflow.WithActivityOptions(workflowContext, infrastructureOptions())
	var generation int64
	if applyError := sdkworkflow.ExecuteActivity(infrastructureContext, sharedworkflow.ApplyPendingMountsActivityName,
		sharedworkflow.ApplyPendingMountsInput{DeployID: input.DeployID}).Get(infrastructureContext, &generation); applyError != nil {
		return fmt.Errorf("Deployment を更新できませんでした: %w", applyError)
	}
	// 2. 更新後の世代で、起動し終えるのを待つ。
	waitContext := sdkworkflow.WithActivityOptions(workflowContext, startupWaitOptions())
	if waitError := sdkworkflow.ExecuteActivity(waitContext, sharedworkflow.WaitDeploymentReadyActivityName,
		sharedworkflow.WaitDeploymentReadyInput{DeployID: input.DeployID, MinGeneration: generation}).Get(waitContext, nil); waitError != nil {
		return fmt.Errorf("Deployment が起動し終えませんでした: %w", waitError)
	}
	return nil
}
