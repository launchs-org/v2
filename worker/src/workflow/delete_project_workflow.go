// Package workflow は、Temporal の Workflow(処理の順序を決めるもの)を実装する。
//
// Workflow は、決定的に書く。時刻、乱数、入出力は直接使わず、全て Activity に出す。
package workflow

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

const (
	// activityMaximumAttempts は、Activity 1つを、失敗したときに試す最大の回数。
	activityMaximumAttempts = 5

	// activityInitialRetryInterval は、最初のリトライまでの待ち時間。以降は倍々に増える。
	activityInitialRetryInterval = 2 * time.Second
)

// DeleteProjectWorkflow は、プロジェクトの実体を削除する。
//
// 処理の順序:
//  1. k8s の namespace を削除する(配下の deploy、service、ingress、volume のデータごと消える)
//  2. DB のレコードを削除する(これが終わると、プロジェクトの GET は 404 になる)
//
// どちらの Activity も冪等で、失敗したときはリトライする。リトライしても失敗が続いたら、
// Workflow は失敗で終わり、プロジェクトは deleting のまま残る(あとから再実行できる)。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 削除するプロジェクトの ID。
//
// 戻り値:
//   - いずれかの Activity が、リトライしても失敗したときのエラー。
func DeleteProjectWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.DeleteProjectInput) error {
	// Activity の実行時間の上限とリトライの方針を決める。
	activityContext := sdkworkflow.WithActivityOptions(workflowContext, sdkworkflow.ActivityOptions{
		StartToCloseTimeout: sharedworkflow.DeleteProjectActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    activityMaximumAttempts,
			InitialInterval:    activityInitialRetryInterval,
			BackoffCoefficient: 2,
		},
	})

	// 1. k8s の namespace を削除する。
	namespaceError := sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.DeleteProjectNamespaceActivityName,
		sharedworkflow.DeleteProjectNamespaceInput{ProjectID: input.ProjectID}).Get(activityContext, nil)
	if namespaceError != nil {
		return fmt.Errorf("namespace を削除できませんでした: %w", namespaceError)
	}

	// 2. DB のレコードを削除する。
	recordsError := sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.DeleteProjectRecordsActivityName,
		sharedworkflow.DeleteProjectRecordsInput{ProjectID: input.ProjectID}).Get(activityContext, nil)
	if recordsError != nil {
		return fmt.Errorf("レコードを削除できませんでした: %w", recordsError)
	}
	return nil
}
