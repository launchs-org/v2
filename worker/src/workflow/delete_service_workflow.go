package workflow

import (
	"errors"
	"fmt"

	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// DeleteServiceWorkflow は、service の実体を削除する。
//
// 処理の順序:
//  1. k8s の Service を削除する
//  2. DB のレコードを削除する(これが終わると、service の GET は 404 になる)
//
// どちらの Activity も冪等で、失敗したときは、リトライの期限(DeleteProjectRetryWindow)までリトライする。
// 期限までに終わらなかったとき、または、リトライしても無駄なエラー(名前の形の不正)のときは、
// service を failed にしてから、Workflow を失敗で終える。failed にすると、利用者は、もう一度削除を呼んでやり直せる。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 削除する service の ID。
//
// 戻り値:
//   - 削除に失敗したときのエラー(service は failed にしてある)。failed にもできなかったときは、両方の原因を含む。
func DeleteServiceWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.DeleteServiceInput) error {
	// 削除の手順(k8s の Service、レコード)を実行する。
	deleteError := deleteServiceEntities(workflowContext, input)
	if deleteError == nil {
		return nil
	}

	// 失敗したので、service を failed にする。キャンセルされていても実行できるよう、切り離した context で呼ぶ。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	compensationContext := sdkworkflow.WithActivityOptions(disconnectedContext, compensationOptions())
	markError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.MarkServiceDeleteFailedActivityName,
		sharedworkflow.MarkServiceDeleteFailedInput{ServiceID: input.ServiceID}).Get(compensationContext, nil)
	// 記録にも失敗したときは、両方の原因を返す(service は deleting のまま残る)。
	if markError != nil {
		return errors.Join(deleteError, fmt.Errorf("削除失敗の記録もできませんでした: %w", markError))
	}
	return deleteError
}

// deleteServiceEntities は、k8s の Service、レコードの順に、service の実体を削除する。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 削除する service の ID。
//
// 戻り値:
//   - いずれかの Activity が、リトライの期限までに成功しなかったときのエラー。
func deleteServiceEntities(workflowContext sdkworkflow.Context, input sharedworkflow.DeleteServiceInput) error {
	// 期限までは、復旧を待ってリトライし続ける。
	activityContext := sdkworkflow.WithActivityOptions(workflowContext, retryUntil(sharedworkflow.DeleteProjectRetryWindow))
	// 1. k8s の Service を削除する。
	if objectError := sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.DeleteServiceObjectActivityName,
		sharedworkflow.DeleteServiceObjectInput{ServiceID: input.ServiceID}).Get(activityContext, nil); objectError != nil {
		return fmt.Errorf("Service を削除できませんでした: %w", objectError)
	}
	// 2. DB のレコードを削除する。
	if recordError := sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.DeleteServiceRecordActivityName,
		sharedworkflow.DeleteServiceRecordInput{ServiceID: input.ServiceID}).Get(activityContext, nil); recordError != nil {
		return fmt.Errorf("レコードを削除できませんでした: %w", recordError)
	}
	return nil
}
