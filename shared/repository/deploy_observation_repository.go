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
