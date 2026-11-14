package repository

import (
	"context"
	"launchs/shared/apperror"
	"launchs/shared/model"

	"gorm.io/gorm"
)

// MarkDeleting は、deploy の状態を、削除中(deleting)にする。
//
// 通常の状態(active、scaled_zero、failed)と、前回の削除に失敗して止まっている(delete_failed)ものが対象。
// 後者は、削除のやり直しになる。反映中(applying)、起動中(pending)、すでに削除中は conflict。
// 実体の削除は、ここでは行わない(Workflow が非同期に行う)。行をロックするので、同時に2つの削除が来ても、1つだけが受け付けられる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 削除する deploy の ID。
//
// 戻り値:
//   - 更新する前の状態。Workflow を開始できなかったときに、戻すために使う。
//   - 無い、または他のユーザーのものなら not_found。削除を受け付けられない状態なら conflict。
func (deployRepository *DeployRepository) MarkDeleting(requestContext context.Context, userID string, deployID string) (string, error) {
	// 更新前の状態を、トランザクションの外へ持ち出す入れ物。
	previousStatus := ""
	// 取得と更新を、1つのトランザクションで行う。
	transactionError := deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして取得する。
		deploy, lockError := lockDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// モデルに、削除の受け付けを任せる(状態の規則は、モデルが持つ)。
		acceptedStatus, beginError := deploy.BeginDelete()
		if beginError != nil {
			return beginError
		}
		// 変えた deploy を保存する。
		if saveError := transaction.Save(&deploy).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "deploy を削除中にできませんでした", saveError)
		}
		previousStatus = acceptedStatus
		return nil
	})
	// 失敗したら、そのエラーを返す。
	if transactionError != nil {
		return "", transactionError
	}
	return previousStatus, nil
}

// RevertDeleting は、削除中(deleting)にした deploy を、削除を受け付ける前の状態に戻す。
//
// 削除の Workflow を開始できなかったときに使う。削除中でないとき、または無いときは、何もしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 状態を戻す deploy の ID。
//   - previousStatus: 戻す先の状態(MarkDeleting が返したもの)。
//
// 戻り値:
//   - データベースのエラー。
func (deployRepository *DeployRepository) RevertDeleting(requestContext context.Context, deployID string, previousStatus string) error {
	// 条件の値が空のときは拒否し、削除中のときだけ、モデルに前の状態へ戻させる。
	if hasEmpty(previousStatus) {
		return newEmptyConditionError()
	}
	_, revertError := transitionDeploy(deployRepository.database, requestContext, deployID, func(deploy *model.Deploy) bool {
		return deploy.RevertDeleting(previousStatus)
	})
	return revertError
}

// MarkDeleteFailed は、削除中(deleting)の deploy を、削除に失敗した状態(delete_failed)にする。
//
// 削除の Workflow が、リトライの期限までに終えられなかったときの補償。削除中でないとき、または無いときは、何もしない(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 削除に失敗した deploy の ID。
//
// 戻り値:
//   - データベースのエラー。
func (deployRepository *DeployRepository) MarkDeleteFailed(requestContext context.Context, deployID string) error {
	// 削除中のときだけ、モデルに delete_failed へ変えさせる。
	_, markError := transitionDeploy(deployRepository.database, requestContext, deployID, func(deploy *model.Deploy) bool {
		return deploy.MarkDeleteFailed()
	})
	return markError
}

// DeleteRecord は、deploy を物理削除する。削除の Workflow が、最後に使う。
//
// 削除中(deleting)の deploy だけを消す(誤って消さないための安全装置)。繰り返し実行されても壊れない。
// service、volume のマウント設定、ログのテーブルができたら、配下の削除に、ここへ足す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 削除する deploy の ID。
//
// 戻り値:
//   - データベースのエラー。
func (deployRepository *DeployRepository) DeleteRecord(requestContext context.Context, deployID string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(deployID) {
		return newEmptyConditionError()
	}
	// 削除中の deploy だけを物理削除する。
	deleteError := deployRepository.database.WithContext(requestContext).
		Where(&model.Deploy{ID: deployID, Status: model.DeployStatusDeleting}).
		Delete(&model.Deploy{}).Error
	// データベースのエラーは、内部エラーにして返す。
	if deleteError != nil {
		return apperror.Wrap(apperror.KindInternal, "deploy を削除できませんでした", deleteError)
	}
	return nil
}
