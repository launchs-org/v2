package workflow

import (
	"time"

	"go.temporal.io/sdk/temporal"
	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// standardRetryPolicy は、全ての Activity に共通する、再試行の方針を返す。
//
// 間隔は、最初の再試行までを activityInitialRetryInterval(2秒)とし、そのあとは倍々に増えて、
// activityMaximumRetryInterval(1分)で頭打ちにする。回数は無制限(MaximumAttempts を 0)にして、
// 期限(ScheduleToCloseTimeout)までなら、復旧を待って再試行し続ける。
//
// 戻り値:
//   - 再試行の方針。
func standardRetryPolicy() *temporal.RetryPolicy {
	// 間隔と倍率を設定して返す。
	return &temporal.RetryPolicy{
		InitialInterval:    activityInitialRetryInterval,
		MaximumInterval:    activityMaximumRetryInterval,
		BackoffCoefficient: 2,
	}
}

// retryUntil は、失敗しても、window までは再試行し続ける Activity の設定を返す。
//
// 1回の実行は、DeleteProjectActivityTimeout(5分)まで。window は、再試行の待ち時間も含めた、最初の開始からの期限。
// 期限を超えたら、Activity は失敗とする。
//
// 引数:
//   - window: 再試行を続ける最長の時間。
//
// 戻り値:
//   - Activity の設定。
func retryUntil(window time.Duration) sdkworkflow.ActivityOptions {
	// 1回の実行の上限と、再試行の期限と、方針を設定して返す。
	return sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.DeleteProjectActivityTimeout,
		ScheduleToCloseTimeout: window,
		RetryPolicy:            standardRetryPolicy(),
	}
}

// retryUntilWithHeartbeat は、retryUntil に加えて、生きていることの連絡(ハートビート)を求める Activity の設定を返す。
//
// 連絡が DeleteProjectActivityHeartbeatTimeout(30秒)を超えて無いと、Activity は止まったとみなされ、再試行される。
// 長く待つ Activity(namespace の削除など)で使う。
//
// 引数:
//   - window: 再試行を続ける最長の時間。
//
// 戻り値:
//   - Activity の設定。
func retryUntilWithHeartbeat(window time.Duration) sdkworkflow.ActivityOptions {
	// retryUntil の設定に、ハートビートの期限を足して返す。
	options := retryUntil(window)
	options.HeartbeatTimeout = sharedworkflow.DeleteProjectActivityHeartbeatTimeout
	return options
}

// infrastructureOptions は、k8s への依頼(namespace や Deployment の作成・更新)の Activity の設定を返す。
// 一時的な障害なら、DeployInfrastructureRetryWindow(2分)まで再試行する。
//
// 戻り値:
//   - Activity の設定。
func infrastructureOptions() sdkworkflow.ActivityOptions {
	// 2分の期限で、再試行し続ける。
	return retryUntil(sharedworkflow.DeployInfrastructureRetryWindow)
}

// compensationOptions は、失敗や結果の記録(補償)の Activity の設定を返す。
// 記録できないと deploy や project が途中の状態のまま残るので、compensationRetryWindow(10分)まで再試行する。
//
// 戻り値:
//   - Activity の設定。
func compensationOptions() sdkworkflow.ActivityOptions {
	// 10分の期限で、再試行し続ける。
	return retryUntil(compensationRetryWindow)
}

// startupWaitOptions は、Deployment が起動し終える(Ready になる)のを待つ Activity の設定を返す。
//
// 仕様のタイムアウト(DeployStartTimeout。300秒)を、再試行を含めた全体の期限にする。ワーカーの再起動などで
// Activity が途切れても、期限内なら別のワーカーが引き継ぐ(正常に起動できる deploy を、ワーカーの入れ替えだけで
// failed にしないため)。期限を超えたら、失敗とする。
//
// 戻り値:
//   - Activity の設定。
func startupWaitOptions() sdkworkflow.ActivityOptions {
	// 1回の実行も、全体の期限も、300秒にして、ハートビートを求める。
	return sdkworkflow.ActivityOptions{
		StartToCloseTimeout:    sharedworkflow.DeployStartTimeout,
		ScheduleToCloseTimeout: sharedworkflow.DeployStartTimeout,
		HeartbeatTimeout:       sharedworkflow.DeleteProjectActivityHeartbeatTimeout,
		RetryPolicy:            standardRetryPolicy(),
	}
}
