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

// ApplyPod は、Pod の観測した状態を保存し、前の状態との差から、状態変化のイベントを追記する。
//
// 前の状態は、DB の行を、行ロックして読む。DB に前の状態がある(watcher が引き継いだ)ときも、
// 止まっていた間の変化が、差分としてイベントになる。deploy の行が無いときは、何も保存しない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - next: 観測した今の状態。
//
// 戻り値:
//   - 追記したイベント。deploy の行が無くて保存しなかったときは空。
//   - データベースのエラー。
func (observationRepository *DeployObservationRepository) ApplyPod(requestContext context.Context, next model.DeployPod) ([]model.DeployPodEvent, error) {
	// 条件の値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(next.PodUID, next.DeployID, next.ProjectID, next.PodName) {
		return nil, newEmptyConditionError()
	}
	// 追記したイベントを、トランザクションの外へ持ち出す入れ物。
	var appended []model.DeployPodEvent
	// 前の状態の読み込みから、保存、イベントの追記までを、1つのトランザクションで行う。
	transactionError := observationRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 前の状態を、行ロックして読む(無ければ nil のまま)。
		var previousRow model.DeployPod
		var previous *model.DeployPod
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.DeployPod{PodUID: next.PodUID}).
			Take(&previousRow).Error
		if findError == nil {
			previous = &previousRow
		} else if !errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindInternal, "Pod の前の状態を取得できませんでした", findError)
		}

		// 「安定して動いている状態でなくなった時刻」を、前の状態から決める。
		next.UnhealthySince = next.NextUnhealthySince(previous)

		// 今の状態を保存する(あれば全ての列を上書きする)。
		saveError := transaction.
			Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "pod_uid"}}, UpdateAll: true}).
			Create(&next).Error
		if saveError != nil {
			return saveError
		}
		// 差分から、状態変化のイベントを追記する。
		events := DiffPodEvents(previous, next)
		if len(events) > 0 {
			if eventError := transaction.Create(&events).Error; eventError != nil {
				return eventError
			}
		}
		appended = events
		return nil
	})
	// deploy の行が無いとき(外部キー違反)は、保存されないだけで、エラーにはしない。
	if errors.Is(transactionError, gorm.ErrForeignKeyViolated) {
		return nil, nil
	}
	if transactionError != nil {
		var appError *apperror.Error
		if errors.As(transactionError, &appError) {
			return nil, transactionError
		}
		return nil, apperror.Wrap(apperror.KindInternal, "Pod の状態を保存できませんでした", transactionError)
	}
	return appended, nil
}

// DeletePod は、消えた Pod の行を削除し、deleted のイベントを追記する。無ければ何もしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - projectID: Pod の namespace(プロジェクトの ID)。
//   - podName: Pod の名前。
//   - observedAt: 消えたことを観測した時刻。
//
// 戻り値:
//   - 削除した行があれば true。
//   - データベースのエラー。
func (observationRepository *DeployObservationRepository) DeletePod(requestContext context.Context, projectID string, podName string, observedAt time.Time) (bool, error) {
	// 条件の値が空だと、全ての行が対象になるので、先に拒否する。
	if hasEmpty(projectID, podName) {
		return false, newEmptyConditionError()
	}
	// 削除したかを、トランザクションの外へ持ち出す入れ物。
	deleted := false
	// 読み込み、イベントの追記、削除を、1つのトランザクションで行う。
	transactionError := observationRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 該当の Pod の行を、行ロックして読む。
		var pod model.DeployPod
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.DeployPod{ProjectID: projectID, PodName: podName}).
			Take(&pod).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return nil
		}
		if findError != nil {
			return findError
		}
		// deleted のイベントを追記して、行を削除する。
		deletedEvent := model.DeployPodEvent{
			DeployID: pod.DeployID, PodUID: pod.PodUID, PodName: pod.PodName,
			Type: model.PodEventDeleted, OccurredAt: observedAt,
		}
		if eventError := transaction.Create(&deletedEvent).Error; eventError != nil {
			return eventError
		}
		if deleteError := transaction.Where(&model.DeployPod{PodUID: pod.PodUID}).Delete(&model.DeployPod{}).Error; deleteError != nil {
			return deleteError
		}
		deleted = true
		return nil
	})
	if transactionError != nil {
		return false, apperror.Wrap(apperror.KindInternal, "Pod の行を削除できませんでした", transactionError)
	}
	return deleted, nil
}

// ListPods は、保存してある Pod の状態を全て返す。
//
// 戻り値:
//   - Pod の一覧。
//   - データベースのエラー。
func (observationRepository *DeployObservationRepository) ListPods(requestContext context.Context) ([]model.DeployPod, error) {
	// 全ての行を読む。
	var pods []model.DeployPod
	listError := observationRepository.database.WithContext(requestContext).Find(&pods).Error
	if listError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "Pod の一覧を取得できませんでした", listError)
	}
	return pods, nil
}

// ListPodEvents は、deploy の Pod の状態変化のイベントを、新しいものから返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: deploy の ID。
//   - limit: 返す最大の件数。
//
// 戻り値:
//   - イベントの一覧(新しい順)。
//   - データベースのエラー。
func (observationRepository *DeployObservationRepository) ListPodEvents(requestContext context.Context, deployID string, limit int) ([]model.DeployPodEvent, error) {
	// 条件の値が空だと、全ての行が対象になるので、先に拒否する。
	if hasEmpty(deployID) {
		return nil, newEmptyConditionError()
	}
	// 新しい順に、件数を絞って読む。
	var events []model.DeployPodEvent
	listError := observationRepository.database.WithContext(requestContext).
		Where(&model.DeployPodEvent{DeployID: deployID}).
		Order("id DESC").Limit(limit).Find(&events).Error
	if listError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "Pod のイベントを取得できませんでした", listError)
	}
	return events, nil
}

// ListPodsByDeploy は、deploy の、いま存在する Pod の状態を、作られた順に返す。
//
// 消えた Pod の行は残らないので、返るのは、いま存在する Pod(終了処理中を含む)だけ。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - Pod の一覧(作られた時刻の順。同じ時刻なら UID の順)。無ければ空。
//   - データベースのエラー。
func (observationRepository *DeployObservationRepository) ListPodsByDeploy(requestContext context.Context, deployID string) ([]model.DeployPod, error) {
	// 条件の値が空だと、全ての行が対象になるので、先に拒否する。
	if hasEmpty(deployID) {
		return nil, newEmptyConditionError()
	}
	// 作られた順に読む(順番が毎回同じになるよう、UID でも並べる)。
	var pods []model.DeployPod
	listError := observationRepository.database.WithContext(requestContext).
		Where(&model.DeployPod{DeployID: deployID}).
		Order("pod_created_at, pod_uid").Find(&pods).Error
	if listError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "Pod の一覧を取得できませんでした", listError)
	}
	return pods, nil
}
