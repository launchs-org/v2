package repository

import (
	"context"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// SavePendingImage は、deploy のイメージを、まだ適用しない値として保存する。
//
// 起動設定の反映の流れ(BeginConfigApply と、worker の Workflow)に載せるため、起動設定の pending が
// まだ無いときは、現在の環境変数・起動コマンド・起動引数を、そのまま pending にコピーする。
// すでに起動設定の pending があるときは、その内容を変えず、イメージだけを足す。
// 現在のイメージと同じものを送ったときは、イメージの pending は作らない(起動設定の pending は、そのまま)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: イメージを更新する deploy の ID。
//   - imageRef: 新しいイメージの参照。
//
// 戻り値:
//   - 現在と同じイメージで、変更が無かったときは false。pending を保存したときは true。
//   - deploy が無い、または他のユーザーのものなら not_found。反映中や削除の途中なら conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (deployRepository *DeployRepository) SavePendingImage(requestContext context.Context, userID string, deployID string, imageRef string) (bool, error) {
	// 変更があったかを、トランザクションの外へ持ち出す入れ物。
	changed := false
	// 取得と更新を、1つのトランザクションで行う。
	transactionError := deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 編集できる状態であることを、行をロックして確かめる。
		deploy, lockError := lockEditableDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// 現在のイメージと同じなら、何もしない(同じイメージを、反映し直さない)。
		if deploy.ImageRef == imageRef && deploy.PendingImageRef == nil {
			return nil
		}
		// 起動設定の pending が無いときは、現在の値をコピーして、反映の流れが使えるようにする。
		pendingToSave := model.Deploy{PendingImageRef: &imageRef, PendingEnv: deploy.PendingEnv, PendingCommand: deploy.PendingCommand, PendingArgs: deploy.PendingArgs}
		if len(deploy.PendingEnv) == 0 {
			pendingToSave.PendingEnv = deploy.Env
			pendingToSave.PendingCommand = deploy.Command
			pendingToSave.PendingArgs = deploy.Args
		}
		// 環境変数が空(NULL)のままだと、適用する値が無いことになるので、空のオブジェクトにする。
		if len(pendingToSave.PendingEnv) == 0 {
			pendingToSave.PendingEnv = []byte("{}")
		}
		// 4つの列を、nil のものも含めて、まとめて更新する(更新日時は Gorm が更新する)。
		updateError := transaction.Model(&model.Deploy{}).
			Where(&model.Deploy{ID: deployID}).
			Select("pending_env", "pending_command", "pending_args", "pending_image_ref").
			Updates(&pendingToSave).Error
		// データベースのエラーは、内部エラーにして返す。
		if updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "イメージの更新を保存できませんでした", updateError)
		}
		changed = true
		return nil
	})
	return changed, transactionError
}
