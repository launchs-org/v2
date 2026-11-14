package service

import (
	"context"
	"launchs/shared/apperror"
)

// Scale は、deploy のレプリカ数を変え、k8s への反映を開始する。
//
// 処理の流れ:
//  1. replicas を新しい値にして、状態を applying にする(反映中・起動中・削除の途中なら conflict)。
//     今と同じ数で失敗もしていないなら、何も変えず、成功を返す
//  2. スケーリングの Workflow を開始する(完了は待たない。結果は status で確かめる)
//  3. 手順 2 に失敗したら、replicas と状態を元に戻す
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: スケーリングする deploy の ID。
//   - replicas: 新しいレプリカ数。
//
// 戻り値:
//   - deploy が無い、または他のユーザーのものなら not_found。上記の場合は conflict。
//   - Workflow を開始できなかったときは internal_error(元に戻してある)。
func (deployService *DeployService) Scale(requestContext context.Context, userID string, deployID string, replicas int) error {
	// 1. replicas と状態を更新する。
	start, beginError := deployService.deployStore.BeginScale(requestContext, userID, deployID, replicas)
	if beginError != nil {
		return beginError
	}
	// 変える必要が無かったときは、Workflow も要らない。
	if !start.Changed {
		return nil
	}

	// 2. Workflow を開始する。
	startError := deployService.deployWorkflowStarter.StartScale(requestContext, deployID)
	if startError == nil {
		return nil
	}

	// 3. 開始できなかったので、元に戻す。リクエストが中断されていても戻せるよう、context を切り離す。
	revertError := deployService.deployStore.RevertScale(context.WithoutCancel(requestContext), deployID, start)
	if revertError != nil {
		// 戻せなかったときは、両方の原因を残す(applying のまま残る)。
		return apperror.Wrap(apperror.KindInternal, "スケーリングを開始できませんでした", joinErrors(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "スケーリングを開始できませんでした", startError)
}
