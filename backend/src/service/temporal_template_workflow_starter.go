package service

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"

	sharedworkflow "launchs/shared/workflow"
)

// TemporalTemplateWorkflowStarter は、Temporal で、テンプレートの実行の Workflow を開始する TemplateApplyStarter の実装。
type TemporalTemplateWorkflowStarter struct {
	// temporalClient は、Temporal サーバーへのクライアント。
	temporalClient client.Client
}

// NewTemporalTemplateWorkflowStarter は、TemporalTemplateWorkflowStarter を作る。
//
// 引数:
//   - temporalClient: Temporal サーバーへのクライアント。
//
// 戻り値:
//   - 作成した TemporalTemplateWorkflowStarter。
func NewTemporalTemplateWorkflowStarter(temporalClient client.Client) *TemporalTemplateWorkflowStarter {
	// クライアントを持たせて返す。
	return &TemporalTemplateWorkflowStarter{temporalClient: temporalClient}
}

// StartTemplateApply は、テンプレートの実行の Workflow を開始する(開始だけを行い、完了は待たない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - applyID: 実行の ID。
//
// 戻り値:
//   - Workflow を開始できなかったときのエラー。
func (starter *TemporalTemplateWorkflowStarter) StartTemplateApply(requestContext context.Context, applyID string) error {
	// 実行の ID から決まる Workflow ID で、期限つきで開始する。
	_, executeError := starter.temporalClient.ExecuteWorkflow(requestContext, client.StartWorkflowOptions{
		ID:                       sharedworkflow.NewApplyTemplateWorkflowID(applyID),
		TaskQueue:                sharedworkflow.TaskQueueGeneral,
		WorkflowExecutionTimeout: sharedworkflow.ApplyTemplateWorkflowTimeout,
	}, sharedworkflow.ApplyTemplateWorkflowName, sharedworkflow.ApplyTemplateInput{ApplyID: applyID})
	if executeError != nil {
		return fmt.Errorf("テンプレートの実行の Workflow を開始できませんでした: %w", executeError)
	}
	return nil
}
