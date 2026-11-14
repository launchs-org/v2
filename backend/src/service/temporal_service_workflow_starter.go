package service

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"

	sharedworkflow "launchs/shared/workflow"
)

// TemporalServiceWorkflowStarter は、Temporal で、service の作成・削除の Workflow を開始する。
type TemporalServiceWorkflowStarter struct {
	temporalClient client.Client
}

// NewTemporalServiceWorkflowStarter は、TemporalServiceWorkflowStarter を作る。
//
// 引数:
//   - temporalClient: Temporal のクライアント。
//
// 戻り値:
//   - 作成した TemporalServiceWorkflowStarter。
func NewTemporalServiceWorkflowStarter(temporalClient client.Client) *TemporalServiceWorkflowStarter {
	return &TemporalServiceWorkflowStarter{temporalClient: temporalClient}
}

// StartServiceCreation は、CreateServiceWorkflow を開始する。完了は待たない。
//
// Workflow ID は「create-{serviceId}-{uuid}」。実行するのは、ワーカーの一般の Task Queue。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: 作成する service の ID。
//
// 戻り値:
//   - Temporal に開始を頼めなかったときのエラー。
func (starter *TemporalServiceWorkflowStarter) StartServiceCreation(requestContext context.Context, serviceID string) error {
	// Workflow ID、Task Queue、全体のタイムアウトを決めて、開始を頼む。
	_, executeError := starter.temporalClient.ExecuteWorkflow(requestContext, client.StartWorkflowOptions{
		ID:                       sharedworkflow.NewCreateServiceWorkflowID(serviceID),
		TaskQueue:                sharedworkflow.TaskQueueGeneral,
		WorkflowExecutionTimeout: sharedworkflow.CreateServiceWorkflowTimeout,
	}, sharedworkflow.CreateServiceWorkflowName, sharedworkflow.CreateServiceInput{ServiceID: serviceID})
	// 開始できなかったときは、原因を包んで返す。
	if executeError != nil {
		return fmt.Errorf("service の作成の Workflow を開始できませんでした: %w", executeError)
	}
	return nil
}

// StartServiceDeletion は、DeleteServiceWorkflow を開始する。完了は待たない。
//
// Workflow ID は「delete-{serviceId}-{uuid}」。全体のタイムアウトは、プロジェクトの削除と同じ。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: 削除する service の ID。
//
// 戻り値:
//   - Temporal に開始を頼めなかったときのエラー。
func (starter *TemporalServiceWorkflowStarter) StartServiceDeletion(requestContext context.Context, serviceID string) error {
	// Workflow ID、Task Queue、全体のタイムアウトを決めて、開始を頼む。
	_, executeError := starter.temporalClient.ExecuteWorkflow(requestContext, client.StartWorkflowOptions{
		ID:                       sharedworkflow.NewDeleteServiceWorkflowID(serviceID),
		TaskQueue:                sharedworkflow.TaskQueueGeneral,
		WorkflowExecutionTimeout: sharedworkflow.DeleteProjectWorkflowTimeout,
	}, sharedworkflow.DeleteServiceWorkflowName, sharedworkflow.DeleteServiceInput{ServiceID: serviceID})
	// 開始できなかったときは、原因を包んで返す。
	if executeError != nil {
		return fmt.Errorf("service の削除の Workflow を開始できませんでした: %w", executeError)
	}
	return nil
}
