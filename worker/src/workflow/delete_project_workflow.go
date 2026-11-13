// Package workflow は、Temporal の Workflow(処理の順序を決めるもの)を実装する。
//
// Workflow は、決定的に書く。時刻、乱数、入出力は直接使わず、全て Activity に出す。
package workflow

import (
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

const (
	// activityMaximumRetryInterval は、リトライの間隔の上限。間隔は倍々に増えるが、これで頭打ちにする。
	activityMaximumRetryInterval = time.Minute

	// activityInitialRetryInterval は、最初のリトライまでの待ち時間。以降は倍々に増える。
	activityInitialRetryInterval = 2 * time.Second

	// compensationRetryWindow は、削除失敗の記録(補償)を、失敗してもリトライし続ける最長の時間。
	compensationRetryWindow = 10 * time.Minute
)

// DeleteProjectWorkflow は、プロジェクトの実体を削除する。
//
// 処理の順序:
//  1. k8s の namespace を削除する(配下の deploy、service、ingress、volume のデータごと消える)
//  2. DB のレコードを削除する(これが終わると、プロジェクトの GET は 404 になる)
//
// どちらの Activity も冪等で、失敗したときは、リトライの期限(DeleteProjectRetryWindow)まで
// リトライする(間隔は最大1分)。k8s の一時的な障害なら、復旧すれば削除が完了する。
//
// 期限までに終わらなかったとき、または、リトライしても無駄なエラー(名前の形の不正)のときは、
// プロジェクトを delete_failed にしてから、Workflow を失敗で終える。delete_failed にすると、
// 利用者は、もう一度削除を呼んでやり直せる。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 削除するプロジェクトの ID。
//
// 戻り値:
//   - 削除に失敗したときのエラー(プロジェクトは delete_failed にしてある)。
//     delete_failed にもできなかったときは、両方の原因を含む。
func DeleteProjectWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.DeleteProjectInput) error {
	// 削除の手順(namespace、レコード)を実行する。
	deleteError := deleteProjectEntities(workflowContext, input)
	if deleteError == nil {
		return nil
	}

	// 失敗したので、プロジェクトを delete_failed にする。Workflow がキャンセルされていても実行できるよう、
	// キャンセルから切り離した context で呼ぶ。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	compensationContext := sdkworkflow.WithActivityOptions(disconnectedContext, sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.DeleteProjectActivityTimeout,
		ScheduleToCloseTimeout: compensationRetryWindow,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumInterval:    activityMaximumRetryInterval,
			InitialInterval:    activityInitialRetryInterval,
			BackoffCoefficient: 2,
		},
	})
	markError := sdkworkflow.ExecuteActivity(compensationContext, sharedworkflow.MarkProjectDeleteFailedActivityName,
		sharedworkflow.MarkProjectDeleteFailedInput{ProjectID: input.ProjectID}).Get(compensationContext, nil)
	// 記録にも失敗したときは、両方の原因を返す(プロジェクトは deleting のまま残る)。
	if markError != nil {
		return errors.Join(deleteError, fmt.Errorf("削除失敗の記録もできませんでした: %w", markError))
	}
	return deleteError
}

// deleteProjectEntities は、namespace、レコードの順に、プロジェクトの実体を削除する。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 削除するプロジェクトの ID。
//
// 戻り値:
//   - いずれかの Activity が、リトライの期限までに成功しなかったときのエラー。
func deleteProjectEntities(workflowContext sdkworkflow.Context, input sharedworkflow.DeleteProjectInput) error {
	// Activity の実行時間の上限とリトライの方針を決める。
	activityContext := sdkworkflow.WithActivityOptions(workflowContext, sdkworkflow.ActivityOptions{
		StartToCloseTimeout: sharedworkflow.DeleteProjectActivityTimeout,
		HeartbeatTimeout:    sharedworkflow.DeleteProjectActivityHeartbeatTimeout,
		// リトライの待ち時間も含めた、最初の開始からの期限。回数は無制限(MaximumAttempts を 0)にして、
		// 期限までなら、復旧を待ってリトライし続ける。
		ScheduleToCloseTimeout: sharedworkflow.DeleteProjectRetryWindow,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumInterval:    activityMaximumRetryInterval,
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
