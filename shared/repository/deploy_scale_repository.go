package repository

import (
	"context"
	"launchs/shared/apperror"
	"launchs/shared/model"

	"gorm.io/gorm"
)

// ScaleStart は、スケーリングを始める前の deploy の値。開始に失敗したとき、元に戻すために使う。
type ScaleStart struct {
	// Changed は、スケーリングを始めたか。replicas が同じで何も変える必要が無いときは false。
	Changed bool

	// PreviousReplicas は、変える前の replicas。
	PreviousReplicas int

	// PreviousStatus は、変える前の status。
	PreviousStatus string
}

// BeginScale は、スケーリングを始めるため、replicas を新しい値にし、状態を applying にする。
//
// スケーリングできるのは、通常の状態(active、scaled_zero、failed)の deploy だけ。行をロックして確かめる。
// replicas が今と同じで、failed でもないときは、何も変えない(Changed が false)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: スケーリングする deploy の ID。
//   - replicas: 新しいレプリカ数。
//
// 戻り値:
//   - 変える前の値(元に戻すために使う)。
//   - deploy が無い、または他のユーザーのものなら not_found。反映中・起動中・削除の途中なら conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (deployRepository *DeployRepository) BeginScale(requestContext context.Context, userID string, deployID string, replicas int) (ScaleStart, error) {
	// 変える前の値を、トランザクションの外へ持ち出す入れ物。
	var start ScaleStart
	// 取得と更新を、1つのトランザクションで行う。
	transactionError := deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む(持ち主の条件つき)。
		deploy, lockError := lockDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// 変える前の値を覚える。
		start = ScaleStart{PreviousReplicas: deploy.Replicas, PreviousStatus: deploy.Status}
		// モデルに、スケーリングの開始を任せる(状態の規則は、モデルが持つ)。
		changed, beginError := deploy.BeginScale(replicas)
		if beginError != nil {
			return beginError
		}
		// 何も変わらなかったときは、保存しない。
		if !changed {
			return nil
		}
		// 変えた deploy を保存する(0 も書くので、全ての列を書く)。
		if saveError := transaction.Save(&deploy).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "deploy を更新できませんでした", saveError)
		}
		start.Changed = true
		return nil
	})
	return start, transactionError
}

// RevertScale は、スケーリングの開始に失敗したとき、replicas と状態を元に戻す。
//
// applying の deploy だけを対象にする(別の処理が先に変えていたら、何もしない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 元に戻す deploy の ID。
//   - start: BeginScale が返した、変える前の値。
//
// 戻り値:
//   - データベースのエラー。
func (deployRepository *DeployRepository) RevertScale(requestContext context.Context, deployID string, start ScaleStart) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(deployID, start.PreviousStatus) {
		return newEmptyConditionError()
	}
	// applying のときだけ、replicas と status を戻す。
	revertError := deployRepository.database.WithContext(requestContext).
		Model(&model.Deploy{}).
		Where(&model.Deploy{ID: deployID, Status: model.DeployStatusApplying}).
		Select("replicas", "status").
		Updates(&model.Deploy{Replicas: start.PreviousReplicas, Status: start.PreviousStatus}).Error
	// データベースのエラーは、内部エラーにして返す。
	if revertError != nil {
		return apperror.Wrap(apperror.KindInternal, "スケーリングを元に戻せませんでした", revertError)
	}
	return nil
}
