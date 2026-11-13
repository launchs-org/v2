package service

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"

	sharedworkflow "launchs/shared/workflow"
)

// TemporalProjectDeletionStarter は、Temporal で、プロジェクト削除の Workflow を開始する。
type TemporalProjectDeletionStarter struct {
	temporalClient client.Client
}

// NewTemporalProjectDeletionStarter は、TemporalProjectDeletionStarter を作る。
//
// 引数:
//   - temporalClient: Temporal のクライアント。
//
// 戻り値:
//   - 作成した TemporalProjectDeletionStarter。
func NewTemporalProjectDeletionStarter(temporalClient client.Client) *TemporalProjectDeletionStarter {
	return &TemporalProjectDeletionStarter{temporalClient: temporalClient}
}

// StartProjectDeletion は、DeleteProjectWorkflow を開始する。完了は待たない。
//
// Workflow ID は「delete-{projectId}-{uuid}」。実行するのは、ワーカーの一般の Task Queue。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - projectID: 削除するプロジェクトの ID。
//
// 戻り値:
//   - Temporal に開始を頼めなかったときのエラー。
func (starter *TemporalProjectDeletionStarter) StartProjectDeletion(requestContext context.Context, projectID string) error {
	// Workflow ID、Task Queue、全体のタイムアウトを決めて、開始を頼む。
	_, executeError := starter.temporalClient.ExecuteWorkflow(requestContext, client.StartWorkflowOptions{
		ID:                       sharedworkflow.NewDeleteProjectWorkflowID(projectID),
		TaskQueue:                sharedworkflow.TaskQueueGeneral,
		WorkflowExecutionTimeout: sharedworkflow.DeleteProjectWorkflowTimeout,
	}, sharedworkflow.DeleteProjectWorkflowName, sharedworkflow.DeleteProjectInput{ProjectID: projectID})
	// 開始できなかったときは、原因を包んで返す。
	if executeError != nil {
		return fmt.Errorf("削除の Workflow を開始できませんでした: %w", executeError)
	}
	return nil
}
