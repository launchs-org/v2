package workflow

import (
	"errors"
	"fmt"

	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// ApplyServicePortsWorkflow は、service の pending のポートを、k8s の Service に反映し、結果を確定する。
//
// 処理の順序(仕様どおり、実際の反映を先に試み、結果が決まってから DB の正式なポートを更新する):
//  1. pending のポートで、k8s の Service を更新する(Pod は再起動しない)
//  2. 失敗したときだけ、k8s の Service を、現在の(動いていた)ポートに戻す
//  3. 結果を確定する(成功なら、ポートを pending の内容にして pending を空にし、deploy を active に戻す。
//     失敗なら、ポートと pending は変えず、deploy を failed にする)
//
// 1 は、k8s の一時的な障害なら、リトライの期限(2分)までリトライする(仕様の applying の 300 秒に収まる)。
// 2 と 3 は、キャンセルされていても実行できるよう、切り離した context で、補償と同じ期限(10分)までリトライする。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 反映する service の ID。
//
// 戻り値:
//   - 反映に失敗したときのエラー(結果は確定してある)。確定もできなかったときは、両方の原因を含む。
func ApplyServicePortsWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.ApplyServicePortsInput) error {
	activityInput := sharedworkflow.ApplyServicePortsActivityInput{ServiceID: input.ServiceID}

	// ===== 1. pending のポートで、k8s の Service を更新する =====
	applyContext := sdkworkflow.WithActivityOptions(workflowContext, infrastructureOptions())
	applyError := sdkworkflow.ExecuteActivity(applyContext, sharedworkflow.ApplyServicePortsActivityName, activityInput).Get(applyContext, nil)
	if applyError != nil {
		applyError = fmt.Errorf("Service を更新できませんでした: %w", applyError)
	}

	// 以降は、キャンセルされていても実行できるよう、切り離した context で呼ぶ。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	compensationContext := sdkworkflow.WithActivityOptions(disconnectedContext, compensationOptions())

	// ===== 2. 失敗したときだけ、k8s の Service を、元のポートに戻す(戻せなくても、確定は続ける) =====
	if applyError != nil {
		rollbackError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.RollbackServicePortsActivityName, activityInput).Get(compensationContext, nil)
		if rollbackError != nil {
			applyError = errors.Join(applyError, fmt.Errorf("Service を元のポートに戻せませんでした: %w", rollbackError))
		}
	}

	// ===== 3. 結果を DB に確定する =====
	commitError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.CommitServicePortsResultActivityName,
		sharedworkflow.CommitServicePortsResultInput{ServiceID: input.ServiceID, Succeeded: applyError == nil}).Get(compensationContext, nil)
	// 確定に失敗したときは、反映の失敗と合わせて、両方の原因を返す(deploy は applying のまま残る)。
	if commitError != nil {
		return errors.Join(applyError, fmt.Errorf("ポートの反映の結果を確定できませんでした: %w", commitError))
	}
	return applyError
}
