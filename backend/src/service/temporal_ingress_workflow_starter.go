package service

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"

	sharedworkflow "launchs/shared/workflow"
)

// TemporalIngressWorkflowStarter は、Temporal で、ingress の Workflow を開始する IngressWorkflowStarter の実装。
type TemporalIngressWorkflowStarter struct {
	// temporalClient は、Temporal サーバーへのクライアント。
	temporalClient client.Client
}

// NewTemporalIngressWorkflowStarter は、TemporalIngressWorkflowStarter を作る。
//
// 引数:
//   - temporalClient: Temporal サーバーへのクライアント。
//
// 戻り値:
//   - 作成した TemporalIngressWorkflowStarter。
func NewTemporalIngressWorkflowStarter(temporalClient client.Client) *TemporalIngressWorkflowStarter {
	// クライアントを持たせて返す。
	return &TemporalIngressWorkflowStarter{temporalClient: temporalClient}
}

// StartRulesApply は、ルールの反映の Workflow を開始する(開始だけを行い、完了は待たない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: 反映する ingress の ID。
//
// 戻り値:
//   - Workflow を開始できなかったときのエラー。
func (starter *TemporalIngressWorkflowStarter) StartRulesApply(requestContext context.Context, ingressID string) error {
	// 実行ごとに別の ID で、期限つきで開始する。
	_, executeError := starter.temporalClient.ExecuteWorkflow(requestContext, client.StartWorkflowOptions{
		ID:                       sharedworkflow.NewApplyIngressRulesWorkflowID(ingressID),
		TaskQueue:                sharedworkflow.TaskQueueGeneral,
		WorkflowExecutionTimeout: sharedworkflow.ApplyIngressRulesWorkflowTimeout,
	}, sharedworkflow.ApplyIngressRulesWorkflowName, sharedworkflow.ApplyIngressRulesInput{IngressID: ingressID})
	if executeError != nil {
		return fmt.Errorf("ingress のルールの反映の Workflow を開始できませんでした: %w", executeError)
	}
	return nil
}

// StartIngressDeletion は、ingress の削除の Workflow を開始する(開始だけを行い、完了は待たない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: 削除する ingress の ID。
//
// 戻り値:
//   - Workflow を開始できなかったときのエラー。
func (starter *TemporalIngressWorkflowStarter) StartIngressDeletion(requestContext context.Context, ingressID string) error {
	// 実行ごとに別の ID で、期限つきで開始する。
	_, executeError := starter.temporalClient.ExecuteWorkflow(requestContext, client.StartWorkflowOptions{
		ID:                       sharedworkflow.NewDeleteIngressWorkflowID(ingressID),
		TaskQueue:                sharedworkflow.TaskQueueGeneral,
		WorkflowExecutionTimeout: sharedworkflow.DeleteProjectWorkflowTimeout,
	}, sharedworkflow.DeleteIngressWorkflowName, sharedworkflow.DeleteIngressInput{IngressID: ingressID})
	if executeError != nil {
		return fmt.Errorf("ingress の削除の Workflow を開始できませんでした: %w", executeError)
	}
	return nil
}
