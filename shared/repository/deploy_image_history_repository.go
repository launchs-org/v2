package repository

import (
	"context"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// DeployImageHistoryRepository は、deploy のイメージの更新履歴を、データベースから読む。
// 履歴の書き込みは、反映の結果を確定する処理(DeployRepository.CommitConfigResult)の中で行う。
type DeployImageHistoryRepository struct {
	database *gorm.DB
}

// NewDeployImageHistoryRepository は、DeployImageHistoryRepository を作る。
//
// 引数:
//   - database: データベースの接続。
//
// 戻り値:
//   - 作成した DeployImageHistoryRepository。
func NewDeployImageHistoryRepository(database *gorm.DB) *DeployImageHistoryRepository {
	// 接続を持たせて返す。
	return &DeployImageHistoryRepository{database: database}
}

// List は、deploy のイメージの更新履歴を、新しい順に、最大 limit 件返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 対象の deploy の ID(持ち主の確認は、呼び出し側で済ませておく)。
//   - limit: 返す最大の件数。
//
// 戻り値:
//   - 履歴(新しい順)。無ければ空。
//   - データベースのエラー。
func (repository *DeployImageHistoryRepository) List(requestContext context.Context, deployID string, limit int) ([]model.DeployImageHistory, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(deployID) {
		return nil, newEmptyConditionError()
	}
	// 新しい順に、上限の件数まで読む。
	var rows []model.DeployImageHistory
	if findError := repository.database.WithContext(requestContext).
		Where(&model.DeployImageHistory{DeployID: deployID}).
		Order("id DESC").Limit(limit).Find(&rows).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "イメージの更新履歴を取得できませんでした", findError)
	}
	// 読んだ行を返す。
	return rows, nil
}
