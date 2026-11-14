package service

import (
	"context"
	"launchs/shared/apperror"
)

// Delete は、deploy の削除を受け付け、実体の削除を開始する。
//
// 処理の流れ:
//  1. deploy の状態を deleting にする(反映中・起動中・削除中なら conflict。delete_failed からは、やり直しになる)
//  2. 削除の Workflow を開始する(完了は待たない。完了すると、レコードが消えて GET が 404 になる)
//  3. 手順 2 に失敗したら、状態を元に戻す(削除中のまま残さない)
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 削除する deploy の ID。
//
// 戻り値:
//   - deploy が無い、または他のユーザーのものなら not_found。削除を受け付けられない状態なら conflict。
//   - Workflow を開始できなかったときは internal_error(状態は元に戻してある)。
func (deployService *DeployService) Delete(requestContext context.Context, userID string, deployID string) error {
	// 1. 状態を deleting にする。元の状態は、開始に失敗したときの巻き戻しに使う。
	previousStatus, markError := deployService.deployStore.MarkDeleting(requestContext, userID, deployID)
	if markError != nil {
		return markError
	}

	// 2. 削除の Workflow を開始する。
	startError := deployService.deployWorkflowStarter.StartDeletion(requestContext, deployID)
	if startError == nil {
		return nil
	}

	// 3. 開始できなかったので、状態を元に戻す。リクエストが中断されていても戻せるよう、context を切り離す。
	if revertError := deployService.deployStore.RevertDeleting(context.WithoutCancel(requestContext), deployID, previousStatus); revertError != nil {
		// 戻せなかったときは、両方の原因を残す(deleting のまま残る)。
		return apperror.Wrap(apperror.KindInternal, "deploy の削除を開始できませんでした", joinErrors(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "deploy の削除を開始できませんでした", startError)
}
