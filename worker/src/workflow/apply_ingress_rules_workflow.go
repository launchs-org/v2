package workflow

import (
	"errors"
	"fmt"

	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// ApplyIngressRulesWorkflow は、ingress の pending のルールを、IngressRoute に反映する Workflow。
//
// 流れ: IngressRoute の更新 → 失敗したら、元のルールに戻す → 結果を DB に確定する。
// 成功ならルールが pending の内容になり active に、失敗ならルールは変わらず failed になる。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 反映する ingress の ID。
//
// 戻り値:
//   - 反映に失敗したとき、または結果を確定できなかったときのエラー。
func ApplyIngressRulesWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.ApplyIngressRulesInput) error {
	// 更新と巻き戻しの Activity の入力。
	activityInput := sharedworkflow.ApplyIngressRulesActivityInput{IngressID: input.IngressID}

	// 1. pending のルールで、IngressRoute を更新する(失敗しても、すぐには終わらず、後始末へ進む)。
	applyContext := sdkworkflow.WithActivityOptions(workflowContext, infrastructureOptions())
	applyError := sdkworkflow.ExecuteActivity(applyContext, sharedworkflow.ApplyIngressRulesActivityName, activityInput).Get(applyContext, nil)
	if applyError != nil {
		applyError = fmt.Errorf("IngressRoute を更新できませんでした: %w", applyError)
	}

	// 後始末は、Workflow がキャンセルされても実行されるよう、切り離した context で行う。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	compensationContext := sdkworkflow.WithActivityOptions(disconnectedContext, compensationOptions())

	// 2. 失敗したときは、IngressRoute を、動いていた元のルールに戻す。
	if applyError != nil {
		rollbackError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.RollbackIngressRulesActivityName, activityInput).Get(compensationContext, nil)
		if rollbackError != nil {
			applyError = errors.Join(applyError, fmt.Errorf("IngressRoute を元のルールに戻せませんでした: %w", rollbackError))
		}
	}

	// 3. 結果を DB に確定する(成功ならルールを昇格、失敗なら failed)。
	commitError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.CommitIngressRulesResultActivityName,
		sharedworkflow.CommitIngressRulesResultInput{IngressID: input.IngressID, Succeeded: applyError == nil}).Get(compensationContext, nil)
	if commitError != nil {
		return errors.Join(applyError, fmt.Errorf("ルールの反映の結果を確定できませんでした: %w", commitError))
	}
	return applyError
}
