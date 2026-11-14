package activity

import (
	"context"
	"launchs/shared/model"
	"launchs/shared/workflow"
	"time"

	"go.temporal.io/sdk/activity"
)

// ObservationReader は、起動待ちが必要とする、watcher が DB に書いた観測値の読み取り。
//
// 実体は shared/repository の DeployObservationRepository(書き込むのは watcher だけ)。
type ObservationReader interface {
	// FindDeployment は、deploy の Deployment の観測値を返す。まだ無いときは false。
	FindDeployment(requestContext context.Context, deployID string) (model.DeployObservation, bool, error)

	// HeartbeatAge は、watcher の最後の心拍からの経過時間を返す。一度も無いときは false。
	HeartbeatAge(requestContext context.Context) (time.Duration, bool, error)
}

// WaitDeploymentReady は、Deployment が起動し終える(Ready になる)まで待つ。
//
// 判断の元は、watcher が DB に書いた観測値(k8s には、問い合わせない)。ただし、watcher が止まっている
// (最後の心拍が古い、または一度も無い)ときは、観測値を信用せず、k8s に直接問い合わせる。
// watcher の障害で、正常な deploy が failed にならないようにするため。
//
// 観測値では、次を全て満たしたとき Ready とする(k8s に直接問い合わせたときと同じ条件)。
//   - Deployment の設定の世代が、input.MinGeneration 以上で、k8s が処理済み(observedGeneration が generation 以上)
//   - Pod の総数、最新の設定の Pod の数、準備完了の数、使える数が、全て、希望するレプリカ数と同じ
//
// 一定の間隔で確かめ、待つ間は、Temporal に生きていることを伝える。Activity の実行時間の上限
// (Workflow が決める。仕様では300秒)までに Ready にならなければ、タイムアウトで失敗する。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 起動を待つ deploy の ID と、設定の世代の下限。
//
// 戻り値:
//   - 期限までに Ready にならなかったとき、または DB や k8s に問い合わせられなかったときのエラー。
func (createDeployActivities *CreateDeployActivities) WaitDeploymentReady(requestContext context.Context, input workflow.WaitDeploymentReadyInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	createDeployActivities.appLogger.Info("Deployment の起動を待つ",
		"deploy_id", input.DeployID, "min_generation", input.MinGeneration, "activity", workflow.WaitDeploymentReadyActivityName)

	// namespace の名前(プロジェクトの ID)と、希望するレプリカ数を知るため、deploy を読む。
	deploy, findError := createDeployActivities.deployStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}

	// Ready になるまで、間隔を空けて確かめ続ける。
	previousSource := ""
	for {
		// ----- 起動し終えているかを確かめる(観測値、または k8s) -----
		ready, source, readyError := createDeployActivities.checkReady(requestContext, deploy, input.MinGeneration)
		if readyError != nil {
			return readyError
		}
		// 確かめた元が変わったとき(watcher が止まった、または戻った)は、ログに残す。
		if source != previousSource {
			createDeployActivities.appLogger.Info("起動の確認の元", "deploy_id", input.DeployID, "source", source)
			previousSource = source
		}
		// 終えていれば、完了。
		if ready {
			return nil
		}

		// ----- まだなので、生きていることを伝えて、少し待つ(キャンセルや期限なら、そこで終わる) -----
		activity.RecordHeartbeat(requestContext)
		select {
		case <-requestContext.Done():
			return requestContext.Err()
		case <-time.After(createDeployActivities.deploymentPollInterval):
		}
	}
}

// checkReady は、deploy の Deployment が起動し終えているかを、1回確かめる。
//
// watcher の心拍が新しいときは、DB の観測値で判断する(source は "db")。
// 心拍が古い、または一度も無いときは、k8s に直接問い合わせる(source は "k8s")。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deploy: 起動を待つ deploy(namespace と、希望するレプリカ数に使う)。
//   - minGeneration: Deployment の設定の世代の下限。
//
// 戻り値:
//   - Ready なら true。
//   - 判断に使った元("db" または "k8s")。
//   - DB や k8s に問い合わせられなかったときのエラー。
func (createDeployActivities *CreateDeployActivities) checkReady(requestContext context.Context, deploy model.Deploy, minGeneration int64) (bool, string, error) {
	// watcher の最後の心拍からの経過時間を調べる。
	age, found, ageError := createDeployActivities.observationReader.HeartbeatAge(requestContext)
	if ageError != nil {
		return false, "", ageError
	}

	// 心拍が新しいとき: watcher が生きているので、DB の観測値で判断する。
	if found && age < model.WatcherHeartbeatStaleAfter {
		observation, exists, observationError := createDeployActivities.observationReader.FindDeployment(requestContext, deploy.ID)
		if observationError != nil {
			return false, "", observationError
		}
		// まだ観測されていないときは、Ready ではない(観測されるのを待つ)。
		if !exists {
			return false, "db", nil
		}
		return observationReady(observation, deploy.Replicas, minGeneration), "db", nil
	}

	// 心拍が古い、または無いとき: watcher を信用せず、k8s に直接問い合わせる。
	ready, readyError := createDeployActivities.deploymentOperator.DeploymentReady(requestContext, deploy.ProjectID, deploy.ID)
	return ready, "k8s", readyError
}

// observationReady は、Deployment の観測値が、起動し終えた(Ready)状態かを返す。
//
// 引数:
//   - observation: watcher が書いた観測値。
//   - desiredReplicas: 希望するレプリカ数。
//   - minGeneration: 設定の世代の下限。
//
// 戻り値:
//   - 設定の世代が下限以上で処理済みで、全てのレプリカ数が、希望する数と同じなら true。
func observationReady(observation model.DeployObservation, desiredReplicas int, minGeneration int64) bool {
	// 更新した設定の世代が、まだ観測されていない(古い設定の観測値)なら、Ready とみなさない。
	if observation.Generation < minGeneration || observation.ObservedGeneration < observation.Generation {
		return false
	}
	// 総数、最新の設定の数、準備完了、使える数が、全て、希望する数と同じ。
	return observation.Replicas == desiredReplicas && observation.UpdatedReplicas == desiredReplicas &&
		observation.ReadyReplicas == desiredReplicas && observation.AvailableReplicas == desiredReplicas
}
