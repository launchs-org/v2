package repository

import (
	"context"
	"launchs/shared/apperror"
	"launchs/shared/model"
	"time"

	"gorm.io/gorm"
)

// PruneEvents は、Pod の状態変化のイベントのうち、保存の上限を超えたものを削除する。
//
// 削除するもの(どちらも、古いものから):
//   - 発生した時刻が cutoff より前のもの(保存期間を過ぎたもの)
//   - 1つの deploy につき、新しい順に maxPerDeploy 件を超えたもの
//
// 繰り返し実行しても壊れない。deploy の行ごと消えるものは、外部キーの CASCADE で消えるので、ここでは扱わない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - cutoff: この時刻より前に発生したイベントを削除する。
//   - maxPerDeploy: 1つの deploy に残す、イベントの最大の件数(1以上)。
//
// 戻り値:
//   - 削除した件数。
//   - 引数が不正なとき(時刻が空、件数が1未満)、またはデータベースのエラー。
func (observationRepository *DeployObservationRepository) PruneEvents(requestContext context.Context, cutoff time.Time, maxPerDeploy int) (int64, error) {
	// 空の時刻だと全てが対象外(または対象)になり、件数が0以下だと全て消えるので、先に拒否する。
	if cutoff.IsZero() || maxPerDeploy < 1 {
		return 0, newEmptyConditionError()
	}
	// 削除した件数を、トランザクションの外へ持ち出す入れ物。
	var deletedCount int64
	// 2つの削除を、1つのトランザクションで行う。
	transactionError := observationRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// ----- 保存期間を過ぎたものを削除する -----
		expired := transaction.Where("occurred_at < ?", cutoff).Delete(&model.DeployPodEvent{})
		if expired.Error != nil {
			return expired.Error
		}
		// ----- 1つの deploy につき、新しい順に maxPerDeploy 件を超えたものを削除する -----
		overflow := transaction.Exec(`
			DELETE FROM deploy_pod_events
			WHERE id IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (PARTITION BY deploy_id ORDER BY id DESC) AS position
					FROM deploy_pod_events
				) ranked
				WHERE position > ?
			)`, maxPerDeploy)
		if overflow.Error != nil {
			return overflow.Error
		}
		deletedCount = expired.RowsAffected + overflow.RowsAffected
		return nil
	})
	if transactionError != nil {
		return 0, apperror.Wrap(apperror.KindInternal, "古い Pod のイベントを削除できませんでした", transactionError)
	}
	return deletedCount, nil
}
