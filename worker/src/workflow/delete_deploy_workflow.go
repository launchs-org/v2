package workflow

import (
	"errors"
	"fmt"

	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// DeleteDeployWorkflow は、deploy の実体を削除する。
//
// 処理の順序:
//  1. k8s の Deployment を削除する(Pod も消える)
//  2. DB のレコードを削除する(これが終わると、deploy の GET は 404 になる)
//
// どちらの Activity も冪等で、失敗したときは、リトライの期限(DeleteProjectRetryWindow)までリトライする。
// 期限までに終わらなかったとき、または、リトライしても無駄なエラー(名前の形の不正)のときは、
// deploy を delete_failed にしてから、Workflow を失敗で終える。delete_failed にすると、
// 利用者は、もう一度削除を呼んでやり直せる。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 削除する deploy の ID。
//
// 戻り値:
//   - 削除に失敗したときのエラー(deploy は delete_failed にしてある)。
//     delete_failed にもできなかったときは、両方の原因を含む。
func DeleteDeployWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.DeleteDeployInput) error {
	// 削除の手順(Deployment、レコード)を実行する。
	deleteError := deleteDeployEntities(workflowContext, input)
	if deleteError == nil {
		return nil
	}

	// 失敗したので、deploy を delete_failed にする。キャンセルされていても実行できるよう、切り離した context で呼ぶ。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	compensationContext := sdkworkflow.WithActivityOptions(disconnectedContext, compensationOptions())
	markError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.MarkDeployDeleteFailedActivityName,
		sharedworkflow.MarkDeployDeleteFailedInput{DeployID: input.DeployID}).Get(compensationContext, nil)
	// 記録にも失敗したときは、両方の原因を返す(deploy は deleting のまま残る)。
	if markError != nil {
		return errors.Join(deleteError, fmt.Errorf("削除失敗の記録もできませんでした: %w", markError))
	}
	return deleteError
}

// deleteDeployEntities は、Deployment、レコードの順に、deploy の実体を削除する。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 削除する deploy の ID。
//
// 戻り値:
//   - いずれかの Activity が、リトライの期限までに成功しなかったときのエラー。
func deleteDeployEntities(workflowContext sdkworkflow.Context, input sharedworkflow.DeleteDeployInput) error {
	// Activity の実行時間の上限とリトライの方針を決める。期限までは、復旧を待ってリトライし続ける。
	activityContext := sdkworkflow.WithActivityOptions(workflowContext, retryUntil(sharedworkflow.DeleteProjectRetryWindow))
	// 1. k8s の Deployment を削除する。
	if deploymentError := sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.DeleteDeploymentActivityName,
		sharedworkflow.DeleteDeploymentInput{DeployID: input.DeployID}).Get(activityContext, nil); deploymentError != nil {
		return fmt.Errorf("Deployment を削除できませんでした: %w", deploymentError)
	}
	// 2. DB のレコードを削除する。
	if recordError := sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.DeleteDeployRecordActivityName,
		sharedworkflow.DeleteDeployRecordInput{DeployID: input.DeployID}).Get(activityContext, nil); recordError != nil {
		return fmt.Errorf("レコードを削除できませんでした: %w", recordError)
	}
	return nil
}
