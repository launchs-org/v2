package service

import (
	"context"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// UpdateConfigMapMounts は、deploy の ConfigMap のマウント構成を、まだ適用しない値として保存する(一括置き換え)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//   - mounts: 新しいマウント構成(形は、確かめてから保存する。空は、全て外す。revision の省略(0)は、保存先が決める)。
//
// 戻り値:
//   - 形が不正、ConfigMap や key が無い、path が重なるなら invalid_request。deploy が無ければ not_found。反映中・削除の途中なら conflict。
func (deployService *DeployService) UpdateConfigMapMounts(requestContext context.Context, userID string, deployID string, mounts []model.ConfigMapMountSpec) error {
	// 形(個数、ID、key、path、mode、重複)を確かめて、mode を埋める。
	validated, validateError := model.ValidateConfigMapMounts(mounts)
	if validateError != nil {
		return validateError
	}
	// ConfigMap の実在と版の決定、他のファイルとの重なりの確認を含めて、保存先に任せる。
	return deployService.deployStore.SavePendingConfigMapMounts(requestContext, userID, deployID, validated)
}

// DiscardConfigMapMounts は、pending の ConfigMap のマウント構成を破棄する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - deploy が無ければ not_found。反映中・削除の途中、または pending が無ければ conflict。
func (deployService *DeployService) DiscardConfigMapMounts(requestContext context.Context, userID string, deployID string) error {
	// 保存先に任せる。
	return deployService.deployStore.DiscardPendingConfigMapMounts(requestContext, userID, deployID)
}

// ApplyConfigMapMounts は、pending の ConfigMap のマウントの反映を始める。
//
// 流れ: 状態を applying にする → Workflow を開始する。Workflow を開始できなかったときは、状態を元に戻す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 反映する deploy の ID。
//
// 戻り値:
//   - deploy が無ければ not_found。反映中・削除の途中・起動中、pending が無い、ConfigMap が不正、path が重なるなら conflict。
//   - Workflow を開始できなかったときは internal_error。
func (deployService *DeployService) ApplyConfigMapMounts(requestContext context.Context, userID string, deployID string) error {
	// 1. 状態を applying にする。
	previousStatus, beginError := deployService.deployStore.BeginConfigMapMountsApply(requestContext, userID, deployID)
	if beginError != nil {
		return beginError
	}
	// 2. Workflow を開始する。
	startError := deployService.deployWorkflowStarter.StartConfigMapMountsApply(requestContext, deployID)
	if startError == nil {
		return nil
	}
	// 3. 開始できなかったので、状態を元に戻す(リクエストが中断されていても戻せるよう、context を切り離す)。
	if revertError := deployService.deployStore.RevertApply(context.WithoutCancel(requestContext), deployID, previousStatus); revertError != nil {
		return apperror.Wrap(apperror.KindInternal, "ConfigMap のマウントの反映を開始できませんでした", joinErrors(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "ConfigMap のマウントの反映を開始できませんでした", startError)
}

// GetConfigMapMounts は、deploy と、マウントしている ConfigMap の最新の版(画面が「新しい版があります」と出すため)を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - deploy と、ConfigMap の ID から ConfigMap の行(最新の版と名前を持つ)への対応。
//   - deploy が無ければ not_found。
func (deployService *DeployService) GetConfigMapMounts(requestContext context.Context, userID string, deployID string) (model.Deploy, map[string]model.ConfigMap, error) {
	// deploy を取得する(他のユーザーのものは、見つからない扱いになる)。
	deploy, findError := deployService.deployStore.FindByID(requestContext, userID, deployID)
	if findError != nil {
		return model.Deploy{}, nil, findError
	}
	// 現在と pending のマウントから、ConfigMap の ID を集める。
	idSet := map[string]bool{}
	for _, raw := range [][]byte{deploy.ConfigMapMounts, deploy.PendingConfigMapMounts} {
		mounts, parseError := model.ParseConfigMapMounts(raw)
		if parseError != nil {
			return model.Deploy{}, nil, apperror.Wrap(apperror.KindInternal, "ConfigMap のマウントを読めませんでした", parseError)
		}
		for _, mount := range mounts {
			idSet[mount.ConfigMapID] = true
		}
	}
	configMapIDs := make([]string, 0, len(idSet))
	for configMapID := range idSet {
		configMapIDs = append(configMapIDs, configMapID)
	}
	// 最新の版と名前を、まとめて取得する。
	configMaps, latestError := deployService.deployStore.FindConfigMapLatestRevisions(requestContext, deploy.ProjectID, configMapIDs)
	if latestError != nil {
		return model.Deploy{}, nil, latestError
	}
	return deploy, configMaps, nil
}
