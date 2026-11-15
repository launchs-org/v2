package workflow

import (
	"errors"
	"fmt"

	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// DeleteIngressWorkflow は、ingress を削除する Workflow。
//
// 流れ: IngressRoute の削除 → レコードの削除。期限までに終えられなかったときは、ingress を failed にする(もう一度削除できる)。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 削除する ingress の ID。
//
// 戻り値:
//   - 削除できなかったときのエラー。
func DeleteIngressWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.DeleteIngressInput) error {
	// 実体とレコードの削除を行う。
	deleteError := deleteIngressEntities(workflowContext, input)
	if deleteError == nil {
		return nil
	}

	// 失敗したので、後始末として ingress を failed にする(切り離した context で行う)。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	compensationContext := sdkworkflow.WithActivityOptions(disconnectedContext, compensationOptions())
	markError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.MarkIngressDeleteFailedActivityName,
		sharedworkflow.MarkIngressDeleteFailedInput{IngressID: input.IngressID}).Get(compensationContext, nil)
	if markError != nil {
		return errors.Join(deleteError, fmt.Errorf("削除失敗の記録もできませんでした: %w", markError))
	}
	return deleteError
}

// deleteIngressEntities は、IngressRoute を削除し、続けてレコードを削除する。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 削除する ingress の ID。
//
// 戻り値:
//   - どちらかが期限までに成功しなかったときのエラー。
func deleteIngressEntities(workflowContext sdkworkflow.Context, input sharedworkflow.DeleteIngressInput) error {
	// 削除の Activity は、期限までリトライする。
	activityContext := sdkworkflow.WithActivityOptions(workflowContext, retryUntil(sharedworkflow.DeleteProjectRetryWindow))
	// 1. IngressRoute を削除する。
	if objectError := sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.DeleteIngressObjectActivityName,
		sharedworkflow.DeleteIngressObjectInput{IngressID: input.IngressID}).Get(activityContext, nil); objectError != nil {
		return fmt.Errorf("IngressRoute を削除できませんでした: %w", objectError)
	}
	// 2. レコードを削除する。
	if recordError := sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.DeleteIngressRecordActivityName,
		sharedworkflow.DeleteIngressRecordInput{IngressID: input.IngressID}).Get(activityContext, nil); recordError != nil {
		return fmt.Errorf("レコードを削除できませんでした: %w", recordError)
	}
	return nil
}
