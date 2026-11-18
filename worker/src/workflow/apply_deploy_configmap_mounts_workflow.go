package workflow

import (
	"errors"
	"fmt"

	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// ApplyDeployConfigMapMountsWorkflow は、deploy の pending の ConfigMap のマウントを、k8s に反映する Workflow。
//
// 流れ: 版の ConfigMap を用意して Deployment を更新 → 起動し終えるのを待つ → 失敗したら、元のマウントに戻す → 結果を確定する
// → 成功したら、使われなくなった版の ConfigMap を消す。
// 成功ならマウントが pending の内容になり、失敗ならマウントは変わらず failed になる。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 反映する deploy の ID。
//
// 戻り値:
//   - 反映に失敗したとき、または結果を確定できなかったときのエラー(後始末の失敗は、返さない)。
func ApplyDeployConfigMapMountsWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.ApplyDeployConfigMapMountsInput) error {
	// 更新と、起動の待機を行う(失敗しても、すぐには終わらず、後始末へ進む)。
	applyError := runConfigMapMountsApply(workflowContext, input)

	// 後始末は、Workflow がキャンセルされても実行されるよう、切り離した context で行う。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	compensationContext := sdkworkflow.WithActivityOptions(disconnectedContext, compensationOptions())

	// 失敗したときは、Deployment を、動いていた元のマウントに戻す。
	if applyError != nil {
		rollbackError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.RollbackDeploymentConfigMapMountsActivityName,
			sharedworkflow.RollbackDeploymentConfigMapMountsInput{DeployID: input.DeployID}).Get(compensationContext, nil)
		if rollbackError != nil {
			applyError = errors.Join(applyError, fmt.Errorf("Deployment を元の ConfigMap のマウントに戻せませんでした: %w", rollbackError))
		}
	}

	// 結果を DB に確定する(成功ならマウントを昇格、失敗なら failed)。
	commitError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.CommitConfigMapMountsResultActivityName,
		sharedworkflow.CommitConfigMapMountsResultInput{DeployID: input.DeployID, Succeeded: applyError == nil}).Get(compensationContext, nil)
	if commitError != nil {
		return errors.Join(applyError, fmt.Errorf("ConfigMap のマウントの結果を確定できませんでした: %w", commitError))
	}

	// 成功したときだけ、使われなくなった版の ConfigMap を消す(失敗しても、残るだけなので、結果には影響させない)。
	if applyError == nil {
		cleanupContext := sdkworkflow.WithActivityOptions(disconnectedContext, infrastructureOptions())
		_ = sdkworkflow.ExecuteActivity(cleanupContext, sharedworkflow.CleanupConfigMapRevisionsActivityName,
			sharedworkflow.CleanupConfigMapRevisionsInput{DeployID: input.DeployID}).Get(cleanupContext, nil)
	}
	return applyError
}

// runConfigMapMountsApply は、Deployment を pending の ConfigMap のマウントで更新し、起動し終えるのを待つ。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 反映する deploy の ID。
//
// 戻り値:
//   - どちらかが期限までに成功しなかったときのエラー。
func runConfigMapMountsApply(workflowContext sdkworkflow.Context, input sharedworkflow.ApplyDeployConfigMapMountsInput) error {
	// 1. 版の ConfigMap を用意して、Deployment を更新する(更新後の世代を受け取る)。
	infrastructureContext := sdkworkflow.WithActivityOptions(workflowContext, infrastructureOptions())
	var generation int64
	if applyError := sdkworkflow.ExecuteActivity(infrastructureContext, sharedworkflow.ApplyPendingConfigMapMountsActivityName,
		sharedworkflow.ApplyPendingConfigMapMountsInput{DeployID: input.DeployID}).Get(infrastructureContext, &generation); applyError != nil {
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
