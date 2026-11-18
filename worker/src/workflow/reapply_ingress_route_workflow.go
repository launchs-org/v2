package workflow

import (
	"fmt"

	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// ReapplyIngressRouteWorkflow は、現在のルールのまま、IngressRoute を作り直す Workflow(アクセスの集計の切り替え)。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 対象の ingress の ID。
//
// 戻り値:
//   - 作り直せなかったときのエラー。
func ReapplyIngressRouteWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.ReapplyIngressRouteInput) error {
	// 1 つの Activity で、現在のルールを反映する。
	activityContext := sdkworkflow.WithActivityOptions(workflowContext, infrastructureOptions())
	if applyError := sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.ReapplyIngressRouteActivityName, input).Get(activityContext, nil); applyError != nil {
		return fmt.Errorf("IngressRoute を作り直せませんでした: %w", applyError)
	}
	return nil
}
