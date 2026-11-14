package crashloop

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"

	"launchs/shared/workflow"
)

// TemporalStarter は、Temporal で、deploy を止める Workflow を開始する。
type TemporalStarter struct {
	temporalClient client.Client
}

// NewTemporalStarter は、TemporalStarter を作る。
//
// 引数:
//   - temporalClient: Temporal のクライアント。
//
// 戻り値:
//   - 作成した TemporalStarter。
func NewTemporalStarter(temporalClient client.Client) *TemporalStarter {
	return &TemporalStarter{temporalClient: temporalClient}
}

// StartStop は、StopCrashLoopWorkflow を開始する。完了は待たない。
//
// Workflow ID は「stop-crash-loop-{deployId}-{uuid}」。実行するのは、ワーカーの一般の Task Queue。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - input: 止める deploy と理由。
//
// 戻り値:
//   - Temporal に開始を頼めなかったときのエラー。
func (starter *TemporalStarter) StartStop(requestContext context.Context, input workflow.StopCrashLoopInput) error {
	// Workflow ID、Task Queue、全体のタイムアウトを決めて、開始を頼む。
	_, executeError := starter.temporalClient.ExecuteWorkflow(requestContext, client.StartWorkflowOptions{
		ID:                       workflow.NewStopCrashLoopWorkflowID(input.DeployID),
		TaskQueue:                workflow.TaskQueueGeneral,
		WorkflowExecutionTimeout: workflow.StopCrashLoopWorkflowTimeout,
	}, workflow.StopCrashLoopWorkflowName, input)
	// 開始できなかったときは、原因を包んで返す。
	if executeError != nil {
		return fmt.Errorf("deploy を止める Workflow を開始できませんでした: %w", executeError)
	}
	return nil
}
