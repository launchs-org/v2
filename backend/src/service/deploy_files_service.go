package service

import (
	"context"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// UpdateFiles は、deploy のファイルの構成を、まだ適用しない値として保存する(一括置き換え)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//   - files: 新しいファイルの構成(形は、確かめてから保存する。空は、全て外す)。
//
// 戻り値:
//   - 形が不正、またはマウント先と同じ path なら invalid_request。deploy が無ければ not_found。反映中・削除の途中なら conflict。
func (deployService *DeployService) UpdateFiles(requestContext context.Context, userID string, deployID string, files []model.FileSpec) error {
	// ファイルの形(個数、path、内容の大きさ、重複)を確かめる(マウントとの重なりは、保存先が、行をロックして確かめる)。
	if validateError := model.ValidateFiles(files, nil); validateError != nil {
		return validateError
	}
	// マウントとの重なりの確認を含めて、保存先に任せる。
	return deployService.deployStore.SavePendingFiles(requestContext, userID, deployID, files)
}

// DiscardFiles は、pending のファイルの構成を破棄する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - deploy が無ければ not_found。反映中・削除の途中、または pending が無ければ conflict。
func (deployService *DeployService) DiscardFiles(requestContext context.Context, userID string, deployID string) error {
	// 保存先に任せる。
	return deployService.deployStore.DiscardPendingFiles(requestContext, userID, deployID)
}

// ApplyFiles は、pending のファイルの反映を始める。
//
// 流れ: 状態を applying にする → Workflow を開始する。Workflow を開始できなかったときは、状態を元に戻す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 反映する deploy の ID。
//
// 戻り値:
//   - deploy が無ければ not_found。反映中・削除の途中・起動中、pending が無い、マウントと重なるなら conflict。
//   - Workflow を開始できなかったときは internal_error。
func (deployService *DeployService) ApplyFiles(requestContext context.Context, userID string, deployID string) error {
	// 1. 状態を applying にする。
	previousStatus, beginError := deployService.deployStore.BeginFilesApply(requestContext, userID, deployID)
	if beginError != nil {
		return beginError
	}
	// 2. Workflow を開始する。
	startError := deployService.deployWorkflowStarter.StartFilesApply(requestContext, deployID)
	if startError == nil {
		return nil
	}
	// 3. 開始できなかったので、状態を元に戻す(リクエストが中断されていても戻せるよう、context を切り離す)。
	if revertError := deployService.deployStore.RevertApply(context.WithoutCancel(requestContext), deployID, previousStatus); revertError != nil {
		return apperror.Wrap(apperror.KindInternal, "ファイルの反映を開始できませんでした", joinErrors(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "ファイルの反映を開始できませんでした", startError)
}
