package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// DeployObservationRepository は、k8s の実際の状態(deploy_observations、deploy_pods、deploy_pod_events)の
// 読み書きを担当する。書き込むのは、watcher だけ。
type DeployObservationRepository struct {
	database *gorm.DB
}

// NewDeployObservationRepository は、DeployObservationRepository を作る。
//
// 引数:
//   - database: 使うデータベースの接続。
//
// 戻り値:
//   - 作成した DeployObservationRepository。
func NewDeployObservationRepository(database *gorm.DB) *DeployObservationRepository {
	return &DeployObservationRepository{database: database}
}

// UpsertDeployment は、Deployment の観測値を、保存する(あれば上書きする)。
//
// deploy の行がまだ無い(作成の途中で、先に k8s の側に現れた)、またはすでに無いときは、何も保存せず false を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - observation: 観測値。
//
// 戻り値:
//   - 保存したら true。deploy の行が無くて保存しなかったら false。
//   - データベースのエラー。
func (observationRepository *DeployObservationRepository) UpsertDeployment(requestContext context.Context, observation model.DeployObservation) (bool, error) {
	// 条件の値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(observation.DeployID) {
		return false, newEmptyConditionError()
	}
	// 主キーが同じ行があれば、全ての列を上書きして保存する。
	saveError := observationRepository.database.WithContext(requestContext).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "deploy_id"}}, UpdateAll: true}).
		Create(&observation).Error
	// deploy の行が無いとき(外部キー違反)は、保存しないだけで、エラーにはしない。
	if errors.Is(saveError, gorm.ErrForeignKeyViolated) {
		return false, nil
	}
	if saveError != nil {
		return false, apperror.Wrap(apperror.KindInternal, "Deployment の観測値を保存できませんでした", saveError)
	}
	return true, nil
}

// DeleteDeployment は、Deployment の観測値を削除する。無ければ何もしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 観測値を削除する deploy の ID。
//
// 戻り値:
//   - データベースのエラー。
func (observationRepository *DeployObservationRepository) DeleteDeployment(requestContext context.Context, deployID string) error {
	// 条件の値が空だと、全ての行が対象になるので、先に拒否する。
	if hasEmpty(deployID) {
		return newEmptyConditionError()
	}
	// 該当の行を削除する。
	deleteError := observationRepository.database.WithContext(requestContext).
		Where(&model.DeployObservation{DeployID: deployID}).
		Delete(&model.DeployObservation{}).Error
	if deleteError != nil {
		return apperror.Wrap(apperror.KindInternal, "Deployment の観測値を削除できませんでした", deleteError)
	}
	return nil
}

// ListObservedDeployIDs は、観測値がある deploy の ID を全て返す。
//
// 戻り値:
//   - deploy の ID の一覧。
//   - データベースのエラー。
func (observationRepository *DeployObservationRepository) ListObservedDeployIDs(requestContext context.Context) ([]string, error) {
	// 主キーだけを読む。
	var deployIDs []string
	listError := observationRepository.database.WithContext(requestContext).
		Model(&model.DeployObservation{}).Pluck("deploy_id", &deployIDs).Error
	if listError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "Deployment の観測値の一覧を取得できませんでした", listError)
	}
	return deployIDs, nil
}

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

// FindDeployment は、deploy の Deployment の、観測値を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - 観測値。
//   - 観測値がまだ無いときは false(watcher が、まだ観測していない)。
//   - データベースのエラー。
func (observationRepository *DeployObservationRepository) FindDeployment(requestContext context.Context, deployID string) (model.DeployObservation, bool, error) {
	// 条件の値が空だと、先頭の1件が返るので、先に拒否する。
	if hasEmpty(deployID) {
		return model.DeployObservation{}, false, newEmptyConditionError()
	}
	// ID で1件探す。
	var observation model.DeployObservation
	findError := observationRepository.database.WithContext(requestContext).
		Where(&model.DeployObservation{DeployID: deployID}).Take(&observation).Error
	// 無いときは、エラーにせず、無いことだけを伝える。
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.DeployObservation{}, false, nil
	}
	if findError != nil {
		return model.DeployObservation{}, false, apperror.Wrap(apperror.KindInternal, "Deployment の観測値を取得できませんでした", findError)
	}
	return observation, true, nil
}

// TouchHeartbeat は、watcher が生きていることを示す、心拍を書く(データベースの時計で、いまの時刻にする)。
//
// 戻り値:
//   - データベースのエラー。
func (observationRepository *DeployObservationRepository) TouchHeartbeat(requestContext context.Context) error {
	// 1行だけの行を、あれば更新し、無ければ作る。時刻は、データベースの now() を使う。
	touchError := observationRepository.database.WithContext(requestContext).Exec(`
		INSERT INTO watcher_heartbeats (id, beat_at) VALUES (1, now())
		ON CONFLICT (id) DO UPDATE SET beat_at = now()`).Error
	if touchError != nil {
		return apperror.Wrap(apperror.KindInternal, "watcher の心拍を書けませんでした", touchError)
	}
	return nil
}

// HeartbeatAge は、最後の心拍から、どれだけ時間が経ったかを返す(データベースの時計で計算する)。
//
// 戻り値:
//   - 最後の心拍からの経過時間。
//   - 心拍がまだ一度も書かれていないときは false。
//   - データベースのエラー。
func (observationRepository *DeployObservationRepository) HeartbeatAge(requestContext context.Context) (time.Duration, bool, error) {
	// 経過時間(秒)を、データベースの時計で計算して読む。行が無ければ、結果は0行になる。
	var seconds []float64
	queryError := observationRepository.database.WithContext(requestContext).
		Raw("SELECT EXTRACT(EPOCH FROM (now() - beat_at))::float8 FROM watcher_heartbeats WHERE id = 1").
		Scan(&seconds).Error
	if queryError != nil {
		return 0, false, apperror.Wrap(apperror.KindInternal, "watcher の心拍を取得できませんでした", queryError)
	}
	// 一度も書かれていない。
	if len(seconds) == 0 {
		return 0, false, nil
	}
	return time.Duration(seconds[0] * float64(time.Second)), true, nil
}
