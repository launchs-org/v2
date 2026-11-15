package service

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"

	sharedworkflow "launchs/shared/workflow"
)

// TemporalVolumeWorkflowStarter は、Temporal で、volume の Workflow を開始する VolumeWorkflowStarter の実装。
type TemporalVolumeWorkflowStarter struct {
	// temporalClient は、Temporal サーバーへのクライアント。
	temporalClient client.Client
}

// NewTemporalVolumeWorkflowStarter は、TemporalVolumeWorkflowStarter を作る。
//
// 引数:
//   - temporalClient: Temporal サーバーへのクライアント。
//
// 戻り値:
//   - 作成した TemporalVolumeWorkflowStarter。
func NewTemporalVolumeWorkflowStarter(temporalClient client.Client) *TemporalVolumeWorkflowStarter {
	// クライアントを持たせて返す。
	return &TemporalVolumeWorkflowStarter{temporalClient: temporalClient}
}

// StartSizeApply は、拡張の反映の Workflow を開始する(開始だけを行い、完了は待たない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - volumeID: 反映する volume の ID。
//
// 戻り値:
//   - Workflow を開始できなかったときのエラー。
func (starter *TemporalVolumeWorkflowStarter) StartSizeApply(requestContext context.Context, volumeID string) error {
	// 実行ごとに別の ID で、期限つきで開始する。
	_, executeError := starter.temporalClient.ExecuteWorkflow(requestContext, client.StartWorkflowOptions{
		ID:                       sharedworkflow.NewApplyVolumeSizeWorkflowID(volumeID),
		TaskQueue:                sharedworkflow.TaskQueueGeneral,
		WorkflowExecutionTimeout: sharedworkflow.ApplyVolumeSizeWorkflowTimeout,
	}, sharedworkflow.ApplyVolumeSizeWorkflowName, sharedworkflow.ApplyVolumeSizeInput{VolumeID: volumeID})
	if executeError != nil {
		return fmt.Errorf("volume の拡張の反映の Workflow を開始できませんでした: %w", executeError)
	}
	return nil
}

// StartVolumeDeletion は、volume の削除の Workflow を開始する(開始だけを行い、完了は待たない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - volumeID: 削除する volume の ID。
//
// 戻り値:
//   - Workflow を開始できなかったときのエラー。
func (starter *TemporalVolumeWorkflowStarter) StartVolumeDeletion(requestContext context.Context, volumeID string) error {
	// 実行ごとに別の ID で、期限つきで開始する。
	_, executeError := starter.temporalClient.ExecuteWorkflow(requestContext, client.StartWorkflowOptions{
		ID:                       sharedworkflow.NewDeleteVolumeWorkflowID(volumeID),
		TaskQueue:                sharedworkflow.TaskQueueGeneral,
		WorkflowExecutionTimeout: sharedworkflow.DeleteProjectWorkflowTimeout,
	}, sharedworkflow.DeleteVolumeWorkflowName, sharedworkflow.DeleteVolumeInput{VolumeID: volumeID})
	if executeError != nil {
		return fmt.Errorf("volume の削除の Workflow を開始できませんでした: %w", executeError)
	}
	return nil
}
