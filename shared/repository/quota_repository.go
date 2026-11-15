package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// usageQuery は、ユーザーの使用量を、1つの SQL で数える(引数は、全て同じユーザー ID を6回)。
// 数え方は docs/auth.md §8.2: プロジェクトと deploy は deleting 以外、volume は全て(大きさは、size と pending の大きい方)。
const usageQuery = `
SELECT
  (SELECT count(*) FROM projects WHERE user_id = ? AND status <> 'deleting') AS projects,
  (SELECT count(*) FROM deploys WHERE user_id = ? AND status <> 'deleting') AS deploys,
  COALESCE((SELECT sum(deploy.replicas * spec.cpu_millicores) FROM deploys deploy JOIN deploy_specs spec ON spec.name = deploy.spec
            WHERE deploy.user_id = ? AND deploy.status <> 'deleting'), 0) AS cpu_millicores,
  COALESCE((SELECT sum(deploy.replicas * spec.memory_mib) FROM deploys deploy JOIN deploy_specs spec ON spec.name = deploy.spec
            WHERE deploy.user_id = ? AND deploy.status <> 'deleting'), 0) AS memory_mib,
  (SELECT count(*) FROM volumes WHERE user_id = ?) AS volumes,
  COALESCE((SELECT sum(GREATEST(size_mb, COALESCE(pending_size_mb, 0))) FROM volumes WHERE user_id = ?), 0) AS volume_total_mb`

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
	// 使用量を数える。
	var usage model.QuotaUsage
	if countError := database.Raw(usageQuery, userID, userID, userID, userID, userID, userID).Scan(&usage).Error; countError != nil {
		return model.QuotaLimits{}, model.QuotaUsage{}, apperror.Wrap(apperror.KindInternal, "使用量を数えられませんでした", countError)
	}
	return defaults.LimitsFor(override), usage, nil
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
