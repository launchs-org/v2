package service

import (
	"context"
	"encoding/json"
	"launchs/shared/apperror"

	"gorm.io/datatypes"
)

// UpdateConfig は、起動設定(環境変数、起動コマンド、起動引数)を、まだ適用しない値として保存する。
//
// 3つは、常にまとめて置き換える。コンテナへの反映は、適用(apply)を呼ぶまで行われない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 設定を保存する deploy の ID。
//   - env: 環境変数。
//   - command: 起動コマンド。nil ならイメージの既定。
//   - args: 起動引数。nil ならイメージの既定。
//
// 戻り値:
//   - deploy が無い、または他のユーザーのものなら not_found。反映中や削除の途中なら conflict。
//   - 保存できなかったときは internal_error。
func (deployService *DeployService) UpdateConfig(requestContext context.Context, userID string, deployID string, env map[string]string, command *string, args []string) error {
	// 環境変数を JSON にする。
	envJSON, envError := json.Marshal(env)
	if envError != nil {
		return apperror.Wrap(apperror.KindInternal, "環境変数を保存できませんでした", envError)
	}
	// 起動引数は、指定があるときだけ JSON にする(nil はイメージの既定として NULL で保存する)。
	var argsJSON datatypes.JSON
	if args != nil {
		encodedArgs, argsError := json.Marshal(args)
		if argsError != nil {
			return apperror.Wrap(apperror.KindInternal, "起動引数を保存できませんでした", argsError)
		}
		argsJSON = encodedArgs
	}
	// 保存を、保存先に任せる。
	return deployService.deployStore.SavePendingConfig(requestContext, userID, deployID, envJSON, command, argsJSON)
}

// DiscardConfig は、まだ適用していない起動設定を、適用せずに破棄する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 破棄する deploy の ID。
//
// 戻り値:
//   - deploy が無い、または他のユーザーのものなら not_found。
//     反映中や削除の途中、または破棄する値が無いなら conflict。
func (deployService *DeployService) DiscardConfig(requestContext context.Context, userID string, deployID string) error {
	// 破棄を、保存先に任せる。
	return deployService.deployStore.DiscardPendingConfig(requestContext, userID, deployID)
}

// ApplyConfig は、まだ適用していない起動設定を、コンテナに反映する処理を開始する。
//
// 処理の流れ:
//  1. deploy の状態を applying にする(適用する値が無い・反映中・削除の途中・起動中なら conflict)
//  2. 反映の Workflow を開始する(完了は待たない。結果は status で確かめる)
//  3. 手順 2 に失敗したら、状態を元に戻す(反映されない applying を残さない)
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 反映する deploy の ID。
//
// 戻り値:
//   - deploy が無い、または他のユーザーのものなら not_found。上記の場合は conflict。
//   - Workflow を開始できなかったときは internal_error(状態は元に戻してある)。
func (deployService *DeployService) ApplyConfig(requestContext context.Context, userID string, deployID string) error {
	// 1. 状態を applying にする。元の状態は、開始に失敗したときの巻き戻しに使う。
	previousStatus, beginError := deployService.deployStore.BeginConfigApply(requestContext, userID, deployID)
	if beginError != nil {
		return beginError
	}

	// 2. 反映の Workflow を開始する。
	startError := deployService.deployWorkflowStarter.StartConfigApply(requestContext, deployID)
	if startError == nil {
		return nil
	}

	// 3. 開始できなかったので、状態を元に戻す。リクエストが中断されていても戻せるよう、context を切り離す。
	revertContext := context.WithoutCancel(requestContext)
	if revertError := deployService.deployStore.RevertApply(revertContext, deployID, previousStatus); revertError != nil {
		// 戻せなかったときは、両方の原因を残す(applying のまま残る)。
		return apperror.Wrap(apperror.KindInternal, "起動設定の反映を開始できませんでした", joinErrors(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "起動設定の反映を開始できませんでした", startError)
}
