package repository

import (
	"context"
	"errors"
	"launchs/shared/apperror"
	"launchs/shared/model"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BeginCrashLoopStop は、異常な状態が続く deploy を止めるため、状態を applying にし、止める理由のイベントを記録する。
//
// active の deploy だけが対象(作成中・反映中・削除中・止まっているものには、何もしない)。行をロックして確かめるので、
// 同時にユーザーの操作が来ても、どちらか一方だけが進む。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 止める deploy の ID。
//   - podUID: 根拠になった Pod の UID。
//   - podName: 根拠になった Pod の名前。
//   - reason: 止める理由(イベントの reason)。
//   - message: 補足(イベントの message)。
//   - occurredAt: 発生した時刻。
//
// 戻り値:
//   - 状態を applying にして、イベントを記録したら true。active でなかった(無い、または別の処理が進んでいる)ときは false。
//   - データベースのエラー。
func (deployRepository *DeployRepository) BeginCrashLoopStop(requestContext context.Context, deployID string, podUID string, podName string, reason string, message string, occurredAt time.Time) (bool, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(deployID, podUID, podName) {
		return false, newEmptyConditionError()
	}
	// 始めたかを、トランザクションの外へ持ち出す入れ物。
	began := false
	// 状態の確認と更新と、イベントの記録を、1つのトランザクションで行う。
	transactionError := deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む。無いとき、または active でないときは、何もしない。
		var deploy model.Deploy
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Deploy{ID: deployID}).Take(&deploy).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return nil
		}
		if findError != nil {
			return findError
		}
		if !deploy.CanStopForCrashLoop() {
			return nil
		}
		// 状態を applying にして、止める理由のイベントを記録する。
		if updateError := transaction.Model(&model.Deploy{}).Where(&model.Deploy{ID: deployID}).
			Updates(&model.Deploy{Status: model.DeployStatusApplying}).Error; updateError != nil {
			return updateError
		}
		stopEvent := model.DeployPodEvent{
			DeployID: deployID, PodUID: podUID, PodName: podName, Type: model.PodEventCrashLoopLimit,
			Reason: reason, Message: message, OccurredAt: occurredAt,
		}
		if eventError := transaction.Create(&stopEvent).Error; eventError != nil {
			return eventError
		}
		began = true
		return nil
	})
	if transactionError != nil {
		return false, apperror.Wrap(apperror.KindInternal, "deploy を止める準備ができませんでした", transactionError)
	}
	return began, nil
}

// FinishCrashLoopStop は、異常な状態が続く deploy を止め終えたとき、状態を failed にする。
//
// applying のときだけ変える(別の処理が先に変えていたら、何もしない)。replicas(希望する数)は変えない。
// ユーザーは、同じ数でスケールし直す(failed の deploy は、同じ数でもスケールできる)か、設定を直して適用して、やり直せる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 止めた deploy の ID。
//
// 戻り値:
//   - データベースのエラー。
func (deployRepository *DeployRepository) FinishCrashLoopStop(requestContext context.Context, deployID string) error {
	// applying のときだけ、failed にする。
	_, finishError := deployRepository.UpdateStatus(requestContext, deployID, model.DeployStatusApplying, model.DeployStatusFailed)
	return finishError
}
