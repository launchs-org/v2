package workflow

import (
	"errors"
	"fmt"

	"go.temporal.io/sdk/temporal"
	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// CreateDeployWorkflow は、deploy を k8s の Deployment として作り、起動し終えるのを待って、結果を記録する。
//
// 処理の順序:
//  1. プロジェクトの namespace を用意する(無ければ作る)
//  2. Deployment を作る
//  3. Deployment が起動し終える(Ready)まで待つ(最長 300 秒)
//  4. 成功なら、status を active にする
//
// 1 と 2 は、k8s の一時的な障害なら、リトライの期限(2分)までリトライする。
// 3 は、300 秒(仕様のタイムアウト)で Ready にならなければ失敗とする。待っている間にワーカーが入れ替わっても、
// 期限内なら別のワーカーが引き継ぐ。
// どこかで失敗したら、status を failed にして、Workflow を失敗で終える。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 起動する deploy の ID。
//
// 戻り値:
//   - 起動に失敗したときのエラー(status は failed にしてある)。failed にもできなかったときは、両方の原因を含む。
func CreateDeployWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.CreateDeployInput) error {
	// 起動の手順(namespace、Deployment、待機)を実行する。
	startError := startDeploy(workflowContext, input)

	// 結果を DB に記録する。Workflow がキャンセルされていても記録できるよう、切り離した context で呼ぶ。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	recordContext := sdkworkflow.WithActivityOptions(disconnectedContext, sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.DeleteProjectActivityTimeout,
		ScheduleToCloseTimeout: compensationRetryWindow,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumInterval:    activityMaximumRetryInterval,
			InitialInterval:    activityInitialRetryInterval,
			BackoffCoefficient: 2,
		},
	})
	markError := sdkworkflow.ExecuteActivity(recordContext, sharedworkflow.MarkDeployResultActivityName,
		sharedworkflow.MarkDeployResultInput{DeployID: input.DeployID, Succeeded: startError == nil}).Get(recordContext, nil)

	// 記録に失敗したときは、起動の失敗と合わせて、両方の原因を返す(deploy は pending のまま残る)。
	if markError != nil {
		return errors.Join(startError, fmt.Errorf("起動の結果を記録できませんでした: %w", markError))
	}
	return startError
}

// startDeploy は、namespace を用意し、Deployment を作り、起動し終えるのを待つ。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 起動する deploy の ID。
//
// 戻り値:
//   - いずれかの手順に失敗したとき、または、300 秒までに Ready にならなかったときのエラー。
func startDeploy(workflowContext sdkworkflow.Context, input sharedworkflow.CreateDeployInput) error {
	// k8s への依頼(namespace と Deployment の作成)の方針。期限までは、リトライを続ける。
	infrastructureContext := sdkworkflow.WithActivityOptions(workflowContext, sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.DeleteProjectActivityTimeout,
		ScheduleToCloseTimeout: sharedworkflow.DeployInfrastructureRetryWindow,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumInterval:    activityMaximumRetryInterval,
			InitialInterval:    activityInitialRetryInterval,
			BackoffCoefficient: 2,
		},
	})

	// 1. プロジェクトの namespace を用意する。deploy の ID から、プロジェクトの ID を調べる必要は無い。
	//    Activity が DB から読むので、ここでは deploy の ID だけを渡す。
	if namespaceError := sdkworkflow.ExecuteActivity(infrastructureContext, sharedworkflow.EnsureProjectNamespaceActivityName,
		sharedworkflow.EnsureProjectNamespaceInput{DeployID: input.DeployID}).Get(infrastructureContext, nil); namespaceError != nil {
		return fmt.Errorf("namespace を用意できませんでした: %w", namespaceError)
	}

	// 2. Deployment を作る。作った Deployment の設定の世代を受け取り、起動を待つときの下限にする。
	var generation int64
	if applyError := sdkworkflow.ExecuteActivity(infrastructureContext, sharedworkflow.ApplyDeploymentActivityName,
		sharedworkflow.ApplyDeploymentInput{DeployID: input.DeployID}).Get(infrastructureContext, &generation); applyError != nil {
		return fmt.Errorf("Deployment を作れませんでした: %w", applyError)
	}

	// 3. 起動し終えるのを待つ。仕様のタイムアウト(300秒)を、リトライを含めた全体の期限(ScheduleToClose)にする。
	//    ワーカーの再起動などで Activity が途切れても、期限内なら別のワーカーが引き継ぐ(正常に起動できる
	//    deploy を、ワーカーの入れ替えだけで failed にしないため)。期限を超えたら、失敗とする(リトライしない)。
	waitContext := sdkworkflow.WithActivityOptions(workflowContext, sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.DeployStartTimeout,
		ScheduleToCloseTimeout: sharedworkflow.DeployStartTimeout,
		HeartbeatTimeout:       sharedworkflow.DeleteProjectActivityHeartbeatTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    activityInitialRetryInterval,
			MaximumInterval:    activityMaximumRetryInterval,
			BackoffCoefficient: 2,
		},
	})
	if waitError := sdkworkflow.ExecuteActivity(waitContext, sharedworkflow.WaitDeploymentReadyActivityName,
		sharedworkflow.WaitDeploymentReadyInput{DeployID: input.DeployID, MinGeneration: generation}).Get(waitContext, nil); waitError != nil {
		return fmt.Errorf("Deployment が起動し終えませんでした: %w", waitError)
	}
	return nil
}
