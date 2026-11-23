// Package repository は、管理コンテナの、データベースの読み書きを担当する。
package repository

import (
	"context"

	"gorm.io/gorm"

	"launchs/shared/apperror"
)

// StatusCount は、状態ごとの件数。
type StatusCount struct {
	// Status は、状態。
	Status string `gorm:"column:status" json:"status"`

	// Count は、件数。
	Count int `gorm:"column:count" json:"count"`
}

// Overview は、全体の件数の概要。
type Overview struct {
	// Projects は、プロジェクトの状態ごとの数。
	Projects []StatusCount `json:"projects"`

	// Deploys は、deploy の状態ごとの数。
	Deploys []StatusCount `json:"deploys"`

	// Ingresses は、ingress の状態ごとの数。
	Ingresses []StatusCount `json:"ingresses"`

	// Volumes は、volume の状態ごとの数。
	Volumes []StatusCount `json:"volumes"`

	// Owners は、プロジェクトを持つユーザーの数。
	Owners int `json:"owners"`
}

// OverviewRepository は、全体の概要を集計する。
type OverviewRepository struct {
	database *gorm.DB
}

// NewOverviewRepository は、OverviewRepository を作る。
//
// 引数:
//   - database: app データベースの接続。
//
// 戻り値:
//   - 作成した OverviewRepository。
func NewOverviewRepository(database *gorm.DB) *OverviewRepository {
	// 接続を持たせて返す。
	return &OverviewRepository{database: database}
}

// countByStatus は、テーブルの行を、状態ごとに数える。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - table: 数えるテーブルの名前(呼び出し側が決めた固定の値だけを渡す)。
//
// 戻り値:
//   - 状態ごとの件数(状態の名前順)。データベースのエラーは internal_error。
func (overviewRepository *OverviewRepository) countByStatus(requestContext context.Context, table string) ([]StatusCount, error) {
	// 状態でまとめて数える。
	counts := []StatusCount{}
	queryError := overviewRepository.database.WithContext(requestContext).Table(table).
		Select("status, count(*) AS count").Group("status").Order("status").Scan(&counts).Error
	if queryError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "件数を集計できませんでした", queryError)
	}
	return counts, nil
}

// Get は、全体の概要を集計する(読むだけ)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - 概要。データベースのエラーは internal_error。
func (overviewRepository *OverviewRepository) Get(requestContext context.Context) (Overview, error) {
	// テーブルごとに、状態別の数を集める。
	overview := Overview{}
	var countError error
	if overview.Projects, countError = overviewRepository.countByStatus(requestContext, "projects"); countError != nil {
		return Overview{}, countError
	}
	if overview.Deploys, countError = overviewRepository.countByStatus(requestContext, "deploys"); countError != nil {
		return Overview{}, countError
	}
	if overview.Ingresses, countError = overviewRepository.countByStatus(requestContext, "ingresses"); countError != nil {
		return Overview{}, countError
	}
	if overview.Volumes, countError = overviewRepository.countByStatus(requestContext, "volumes"); countError != nil {
		return Overview{}, countError
	}
	// プロジェクトを持つユーザーの数を数える。
	ownerError := overviewRepository.database.WithContext(requestContext).Table("projects").Select("count(DISTINCT user_id)").Scan(&overview.Owners).Error
	if ownerError != nil {
		return Overview{}, apperror.Wrap(apperror.KindInternal, "件数を集計できませんでした", ownerError)
	}
	return overview, nil
}
