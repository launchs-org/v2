package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// countUsage は、ユーザーの使用量を数える。数え方は docs/auth.md §8.2。
//
// プロジェクトと deploy は deleting 以外、volume は全て数える。スペックごとの deploy の数や、volume の大きさの足し算は、モデルのメソッドに任せる。
// どの操作も、クォータの行をロックしてから呼ぶので、数えている間に、同じユーザーの作成・拡張は入り込まない。
//
// 引数:
//   - database: 使うデータベース。
//   - userID: 持ち主のユーザー ID。
//
// 戻り値:
//   - 使用量。データベースのエラーは internal_error。
func countUsage(database *gorm.DB, userID string) (model.QuotaUsage, error) {
	// 使用量を、足していく入れ物。
	var usage model.QuotaUsage
	// プロジェクトの数を数える(deleting 以外)。
	var projectCount int64
	if countError := database.Model(&model.Project{}).Where("user_id = ? AND status <> ?", userID, model.ProjectStatusDeleting).Count(&projectCount).Error; countError != nil {
		return model.QuotaUsage{}, apperror.Wrap(apperror.KindInternal, "使用量を数えられませんでした", countError)
	}
	usage.Projects = int(projectCount)
	// deploy を読んで、数と、スペックごとの数を足す(deleting 以外。replicas は数えない)。
	var deploys []model.Deploy
	if findError := database.Select("spec").Where("user_id = ? AND status <> ?", userID, model.DeployStatusDeleting).Find(&deploys).Error; findError != nil {
		return model.QuotaUsage{}, apperror.Wrap(apperror.KindInternal, "使用量を数えられませんでした", findError)
	}
	for _, deploy := range deploys {
		usage.AddDeploy(deploy.Spec)
	}
	// volume を読んで、数と、大きさを足す(全て)。
	var volumes []model.Volume
	if findError := database.Select("size_mb", "pending_size_mb").Where("user_id = ?", userID).Find(&volumes).Error; findError != nil {
		return model.QuotaUsage{}, apperror.Wrap(apperror.KindInternal, "使用量を数えられませんでした", findError)
	}
	for index := range volumes {
		usage.AddVolume(&volumes[index])
	}
	return usage, nil
}

// loadQuota は、ユーザーの上限と使用量を読む。
//
// lock が true のときは、ユーザーの account_quotas の行(無ければ作る)を FOR UPDATE でロックしてから、使用量を数える。
// 作成・拡張のトランザクションの最初で呼ぶと、同じユーザーの同時のリクエストが、順番に判定されるので、上限を超えない。
// ロックは、トランザクションの終わりで外れる。
//
// 引数:
//   - database: 使うデータベース(lock が true のときは、トランザクション)。
//   - userID: 持ち主のユーザー ID。
//   - lock: ロックするか。
//
// 戻り値:
//   - ユーザーに適用される上限と、使用量。
//   - データベースのエラーは internal_error。
func loadQuota(database *gorm.DB, userID string, lock bool) (model.QuotaLimits, model.QuotaUsage, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID) {
		return model.QuotaLimits{}, model.QuotaUsage{}, newEmptyConditionError()
	}
	// ロックするときは、行が無ければ作ってから、ロックして読む。
	override := model.AccountQuota{UserID: userID}
	if lock {
		if insertError := database.Exec("INSERT INTO account_quotas (user_id) VALUES (?) ON CONFLICT (user_id) DO NOTHING", userID).Error; insertError != nil {
			return model.QuotaLimits{}, model.QuotaUsage{}, apperror.Wrap(apperror.KindInternal, "クォータを確認できませんでした", insertError)
		}
		if findError := database.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).Take(&override).Error; findError != nil {
			return model.QuotaLimits{}, model.QuotaUsage{}, apperror.Wrap(apperror.KindInternal, "クォータを確認できませんでした", findError)
		}
	} else if findError := database.Where("user_id = ?", userID).Take(&override).Error; findError != nil && !errors.Is(findError, gorm.ErrRecordNotFound) {
		// 読むだけのときは、行が無くてもよい(既定を使う)。
		return model.QuotaLimits{}, model.QuotaUsage{}, apperror.Wrap(apperror.KindInternal, "クォータを確認できませんでした", findError)
	}
	// 既定を読む(無ければ、マイグレーションが流れていない)。
	var defaults model.QuotaDefaults
	if findError := database.Take(&defaults).Error; findError != nil {
		return model.QuotaLimits{}, model.QuotaUsage{}, apperror.Wrap(apperror.KindInternal, "クォータの既定を取得できませんでした", findError)
	}
	// スペックの一覧(スペックごとの既定の上限を持つ)と、ユーザーのスペックごとの上書きを読む。
	var specs []model.DeploySpec
	if findError := database.Find(&specs).Error; findError != nil {
		return model.QuotaLimits{}, model.QuotaUsage{}, apperror.Wrap(apperror.KindInternal, "スペックを取得できませんでした", findError)
	}
	var specOverrides []model.AccountSpecQuota
	if findError := database.Where("user_id = ?", userID).Find(&specOverrides).Error; findError != nil {
		return model.QuotaLimits{}, model.QuotaUsage{}, apperror.Wrap(apperror.KindInternal, "クォータを確認できませんでした", findError)
	}
	// 使用量を数える。
	usage, countError := countUsage(database, userID)
	if countError != nil {
		return model.QuotaLimits{}, model.QuotaUsage{}, countError
	}
	return defaults.LimitsFor(override, specs, specOverrides), usage, nil
}

// QuotaRepository は、ユーザーの上限と使用量を読む。作成・拡張の判定は、各リソースの repository が、同じトランザクションの中で行う。
type QuotaRepository struct {
	database *gorm.DB
}

// NewQuotaRepository は、QuotaRepository を作る。
//
// 引数:
//   - database: 使うデータベース。
//
// 戻り値:
//   - 作成した QuotaRepository。
func NewQuotaRepository(database *gorm.DB) *QuotaRepository {
	// データベースを持たせて返す。
	return &QuotaRepository{database: database}
}

// Get は、ユーザーの上限と、いまの使用量を返す(ロックしない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//
// 戻り値:
//   - 上限と使用量。データベースのエラーは internal_error。
func (quotaRepository *QuotaRepository) Get(requestContext context.Context, userID string) (model.QuotaLimits, model.QuotaUsage, error) {
	// ロックせずに読む。
	return loadQuota(quotaRepository.database.WithContext(requestContext), userID, false)
}
