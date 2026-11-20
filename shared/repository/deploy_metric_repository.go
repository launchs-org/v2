package repository

import (
	"context"
	"encoding/json"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/model"

	"gorm.io/gorm"
)

// DeployMetricRepository は、deploy の観測値(CPU、メモリなど。deploy_metric_samples)の読み書きを担当する。
//
// 書くのは watcher だけ。backend は、集計して読むだけ。
type DeployMetricRepository struct {
	database *gorm.DB
}

// NewDeployMetricRepository は、DeployMetricRepository を作る。
//
// 引数:
//   - database: アプリのデータベースの接続。
func NewDeployMetricRepository(database *gorm.DB) *DeployMetricRepository {
	// 接続を持たせて返す。
	return &DeployMetricRepository{database: database}
}

// MetricScope は、メトリクスを集計する範囲(deploy 1 つ、プロジェクト全体、プロジェクトの中のタグ)。
type MetricScope struct {
	// DeployID は、deploy 1 つに絞るときの ID。
	DeployID string

	// ProjectID は、プロジェクトに絞るときの ID。
	ProjectID string

	// Tag は、プロジェクトの中で、タグが付いた deploy に絞るときのタグ。
	Tag string
}

// MetricPoint は、ある時刻の、範囲全体の合計の観測値。
type MetricPoint struct {
	// At は、観測した時刻。
	At time.Time `gorm:"column:at"`

	// CPUMillicores は、CPU の使用量の合計(ミリコア)。測れていなければ nil。
	CPUMillicores *int64 `gorm:"column:cpu_millicores"`

	// MemoryBytes は、メモリの使用量の合計(バイト)。測れていなければ nil。
	MemoryBytes *int64 `gorm:"column:memory_bytes"`

	// Pods は、Pod の数の合計。
	Pods int64 `gorm:"column:pods"`

	// ReadyPods は、Ready の Pod の数の合計。
	ReadyPods int64 `gorm:"column:ready_pods"`

	// Restarts は、コンテナの再起動の回数の合計。
	Restarts int64 `gorm:"column:restarts"`
}

// InsertSamples は、観測値を保存する(同じ deploy と時刻が、すでにあれば、何もしない)。
//
// プロジェクトの ID は、deploys から引く。deploy がもう無ければ、その行は保存しない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - samples: 保存する観測値(ProjectID は無視する)。
//
// 戻り値:
//   - データベースのエラーは internal_error。
func (repository *DeployMetricRepository) InsertSamples(requestContext context.Context, samples []model.DeployMetricSample) error {
	// 1 つの取引で、全部を書く(途中で失敗したら、全部やめる)。
	return repository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 観測値を 1 件ずつ、deploys からプロジェクトの ID を引いて保存する。
		for _, sample := range samples {
			insertError := transaction.Exec(
				`INSERT INTO deploy_metric_samples (deploy_id, project_id, sampled_at, cpu_millicores, memory_bytes, pods, ready_pods, restarts)
				 SELECT id, project_id, ?, ?, ?, ?, ?, ? FROM deploys WHERE id = ? ON CONFLICT DO NOTHING`,
				sample.SampledAt, sample.CPUMillicores, sample.MemoryBytes, sample.Pods, sample.ReadyPods, sample.Restarts, sample.DeployID).Error
			if insertError != nil {
				return apperror.Wrap(apperror.KindInternal, "メトリクスを保存できませんでした", insertError)
			}
		}
		return nil
	})
}

// PruneSamples は、cutoff より古い観測値を削除する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - cutoff: これより前の観測値を消す。
//
// 戻り値:
//   - 消した件数。データベースのエラーは internal_error。
func (repository *DeployMetricRepository) PruneSamples(requestContext context.Context, cutoff time.Time) (int64, error) {
	// 古い行を削除する。
	result := repository.database.WithContext(requestContext).Where("sampled_at < ?", cutoff).Delete(&model.DeployMetricSample{})
	if result.Error != nil {
		return 0, apperror.Wrap(apperror.KindInternal, "古いメトリクスを削除できませんでした", result.Error)
	}
	return result.RowsAffected, nil
}

// Series は、範囲の合計の観測値を、時刻の昇順で返す。
//
// 持ち主の deploy だけが対象。deploy が無い、他人のもの、観測が無いときは、空の一覧(権限の確認は、呼び出し側が行う)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - scope: 集計する範囲。
//   - since: この時刻以降だけを返す。
//
// 戻り値:
//   - 時刻ごとの合計(1 つの時刻に、範囲の deploy の値を足したもの)。データベースのエラーは internal_error。
func (repository *DeployMetricRepository) Series(requestContext context.Context, userID string, scope MetricScope, since time.Time) ([]MetricPoint, error) {
	// 条件の値が空なら、空の一覧を返す。
	if hasEmpty(userID) {
		return []MetricPoint{}, nil
	}
	// 共通の条件(持ち主と、時刻)を組み立てる。
	query := repository.database.WithContext(requestContext).Table("deploy_metric_samples AS sample").
		Select(`sample.sampled_at AS at, CAST(SUM(sample.cpu_millicores) AS bigint) AS cpu_millicores,
			CAST(SUM(sample.memory_bytes) AS bigint) AS memory_bytes, CAST(SUM(sample.pods) AS bigint) AS pods,
			CAST(SUM(sample.ready_pods) AS bigint) AS ready_pods, CAST(SUM(sample.restarts) AS bigint) AS restarts`).
		Joins("JOIN deploys ON deploys.id = sample.deploy_id").
		Where("deploys.user_id = ? AND sample.sampled_at >= ?", userID, since)
	// 範囲に応じて、絞り込む。
	if scope.DeployID != "" {
		query = query.Where("deploys.id = ?", scope.DeployID)
	}
	if scope.ProjectID != "" {
		query = query.Where("deploys.project_id = ?", scope.ProjectID)
	}
	if scope.Tag != "" {
		encoded, _ := json.Marshal([]string{scope.Tag})
		query = query.Where("deploys.tags @> ?::jsonb", string(encoded))
	}
	// 時刻ごとに足して、昇順に読む。
	points := []MetricPoint{}
	if scanError := query.Group("sample.sampled_at").Order("sample.sampled_at ASC").Scan(&points).Error; scanError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "メトリクスを取得できませんでした", scanError)
	}
	return points, nil
}
