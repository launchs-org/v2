package workflow

import (
	"errors"
	"fmt"
	"time"

	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// volumeExpandOptions は、PVC の拡張(と、容量が増えるのを待つこと)の Activity の設定を返す。
//
// 待つ時間の上限(5分)と、生きていることを伝える間隔を決める。上限を超えたら、失敗にする。
//
// 戻り値:
//   - Activity の設定。
func volumeExpandOptions() sdkworkflow.ActivityOptions {
	// 全体の上限で、リトライを打ち切る。
	return sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.VolumeExpandTimeout + time.Minute,
		ScheduleToCloseTimeout: sharedworkflow.VolumeExpandTimeout + time.Minute,
		HeartbeatTimeout:       sharedworkflow.DeleteProjectActivityHeartbeatTimeout,
		RetryPolicy:            standardRetryPolicy(),
	}
}

// ApplyVolumeSizeWorkflow は、volume の pending の大きさを、PVC に反映する Workflow。
//
// 流れ: PVC の拡張と、容量が増えるのを待つ → 結果を確定する。成功なら大きさが pending の値になり active に、
// 失敗なら大きさと pending は変わらず failed になる(PVC は縮められないので、巻き戻しはしない)。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 反映する volume の ID。
//
// 戻り値:
//   - 反映に失敗したとき、または結果を確定できなかったときのエラー。
func ApplyVolumeSizeWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.ApplyVolumeSizeInput) error {
	// 1. PVC を拡張して、反映されるのを待つ(失敗しても、すぐには終わらず、確定へ進む)。
	expandContext := sdkworkflow.WithActivityOptions(workflowContext, volumeExpandOptions())
	expandError := sdkworkflow.ExecuteActivity(expandContext, sharedworkflow.ExpandVolumeClaimActivityName,
		sharedworkflow.ExpandVolumeClaimInput{VolumeID: input.VolumeID}).Get(expandContext, nil)
	if expandError != nil {
		expandError = fmt.Errorf("PVC を拡張できませんでした: %w", expandError)
	}

	// 2. 結果を DB に確定する(Workflow がキャンセルされても実行されるよう、切り離した context で行う)。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	commitContext := sdkworkflow.WithActivityOptions(disconnectedContext, compensationOptions())
	commitError := sdkworkflow.ExecuteActivity(commitContext, sharedworkflow.CommitVolumeSizeResultActivityName,
		sharedworkflow.CommitVolumeSizeResultInput{VolumeID: input.VolumeID, Succeeded: expandError == nil}).Get(commitContext, nil)
	if commitError != nil {
		return errors.Join(expandError, fmt.Errorf("拡張の結果を確定できませんでした: %w", commitError))
	}
	return expandError
}

// DeleteVolumeWorkflow は、volume を削除する Workflow。
//
// 流れ: PVC の削除 → レコードの削除。期限までに終えられなかったときは、volume を failed にする(もう一度削除できる)。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 削除する volume の ID。
//
// 戻り値:
//   - 削除できなかったときのエラー。
func DeleteVolumeWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.DeleteVolumeInput) error {
	// 実体とレコードの削除を行う。
	deleteError := deleteVolumeEntities(workflowContext, input)
	if deleteError == nil {
		return nil
	}

	// 失敗したので、後始末として volume を failed にする(切り離した context で行う)。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	compensationContext := sdkworkflow.WithActivityOptions(disconnectedContext, compensationOptions())
	markError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.MarkVolumeDeleteFailedActivityName,
		sharedworkflow.MarkVolumeDeleteFailedInput{VolumeID: input.VolumeID}).Get(compensationContext, nil)
	if markError != nil {
		return errors.Join(deleteError, fmt.Errorf("削除失敗の記録もできませんでした: %w", markError))
	}
	return deleteError
}

// deleteVolumeEntities は、PVC を削除し、続けてレコードを削除する。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 削除する volume の ID。
//
// 戻り値:
//   - どちらかが期限までに成功しなかったときのエラー。
func deleteVolumeEntities(workflowContext sdkworkflow.Context, input sharedworkflow.DeleteVolumeInput) error {
	// 削除の Activity は、期限までリトライする。
	activityContext := sdkworkflow.WithActivityOptions(workflowContext, retryUntil(sharedworkflow.DeleteProjectRetryWindow))
	// 1. PVC を削除する。
	if claimError := sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.DeleteVolumeClaimActivityName,
		sharedworkflow.DeleteVolumeClaimInput{VolumeID: input.VolumeID}).Get(activityContext, nil); claimError != nil {
		return fmt.Errorf("PVC を削除できませんでした: %w", claimError)
	}
	// 2. レコードを削除する。
	if recordError := sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.DeleteVolumeRecordActivityName,
		sharedworkflow.DeleteVolumeRecordInput{VolumeID: input.VolumeID}).Get(activityContext, nil); recordError != nil {
		return fmt.Errorf("レコードを削除できませんでした: %w", recordError)
	}
	return nil
}
