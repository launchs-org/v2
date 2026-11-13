package service

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"

	sharedworkflow "launchs/shared/workflow"
)

// TemporalDeployWorkflowStarter は、Temporal で、deploy の起動の Workflow を開始する。
type TemporalDeployWorkflowStarter struct {
	temporalClient client.Client
}

// NewTemporalDeployWorkflowStarter は、TemporalDeployWorkflowStarter を作る。
//
// 引数:
//   - temporalClient: Temporal のクライアント。
//
// 戻り値:
//   - 作成した TemporalDeployWorkflowStarter。
func NewTemporalDeployWorkflowStarter(temporalClient client.Client) *TemporalDeployWorkflowStarter {
	return &TemporalDeployWorkflowStarter{temporalClient: temporalClient}
}

// StartDeployCreation は、CreateDeployWorkflow を開始する。完了は待たない。
//
// Workflow ID は「create-{deployId}-{uuid}」。実行するのは、ワーカーの一般の Task Queue。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 起動する deploy の ID。
//
// 戻り値:
//   - Temporal に開始を頼めなかったときのエラー。
func (starter *TemporalDeployWorkflowStarter) StartDeployCreation(requestContext context.Context, deployID string) error {
	// Workflow ID、Task Queue、全体のタイムアウトを決めて、開始を頼む。
	_, executeError := starter.temporalClient.ExecuteWorkflow(requestContext, client.StartWorkflowOptions{
		ID:                       sharedworkflow.NewCreateDeployWorkflowID(deployID),
		TaskQueue:                sharedworkflow.TaskQueueGeneral,
		WorkflowExecutionTimeout: sharedworkflow.CreateDeployWorkflowTimeout,
	}, sharedworkflow.CreateDeployWorkflowName, sharedworkflow.CreateDeployInput{DeployID: deployID})
	// 開始できなかったときは、原因を包んで返す。
	if executeError != nil {
		return fmt.Errorf("deploy の起動の Workflow を開始できませんでした: %w", executeError)
	}
	return nil
}
