package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// countUsage は、ユーザーの使用量を数える。数え方は dev-docs/auth.md §8.2。
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

// lockAccountQuota は、ユーザーの account_quotas の行(無ければ作る)を、FOR UPDATE でロックして読む。
//
// 作成・拡張・上書きの設定の、どのトランザクションでも、最初に呼ぶ(ロックの順番は、クォータが先)。ロックは、トランザクションの終わりで外れる。
//
// 引数:
//   - transaction: 使っているトランザクション。
//   - userID: 持ち主のユーザー ID。
//
// 戻り値:
//   - ロックした行。データベースのエラーは internal_error。
func lockAccountQuota(transaction *gorm.DB, userID string) (model.AccountQuota, error) {
	// 行が無ければ作る(あれば何もしない)。
	if insertError := transaction.Exec("INSERT INTO account_quotas (user_id) VALUES (?) ON CONFLICT (user_id) DO NOTHING", userID).Error; insertError != nil {
		return model.AccountQuota{}, apperror.Wrap(apperror.KindInternal, "クォータを確認できませんでした", insertError)
	}
	// ロックして読む。
	var row model.AccountQuota
	if findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).Take(&row).Error; findError != nil {
		return model.AccountQuota{}, apperror.Wrap(apperror.KindInternal, "クォータを確認できませんでした", findError)
	}
	return row, nil
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
	// ロックするときは、行が無ければ作ってから、ロックして読む。読むだけなら、行が無くてもよい(既定を使う)。
	override := model.AccountQuota{UserID: userID}
	if lock {
		lockedRow, lockError := lockAccountQuota(database, userID)
		if lockError != nil {
			return model.QuotaLimits{}, model.QuotaUsage{}, lockError
		}
		override = lockedRow
	} else if findError := database.Where("user_id = ?", userID).Take(&override).Error; findError != nil && !errors.Is(findError, gorm.ErrRecordNotFound) {
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

// GetOverrides は、ユーザーの上限の上書き(管理者が設定したもの)を返す(ロックしない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//
// 戻り値:
//   - 上書き一式(設定が無ければ、全て既定)。データベースのエラーは internal_error。
func (quotaRepository *QuotaRepository) GetOverrides(requestContext context.Context, userID string) (model.QuotaOverrides, error) {
	// 空のユーザー ID は、Gorm が条件を無視しないよう、先に拒否する。
	if hasEmpty(userID) {
		return model.QuotaOverrides{}, newEmptyConditionError()
	}
	// 行を読む(無ければ、全て既定)。
	database := quotaRepository.database.WithContext(requestContext)
	var row model.AccountQuota
	if findError := database.Where("user_id = ?", userID).Take(&row).Error; findError != nil && !errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.QuotaOverrides{}, apperror.Wrap(apperror.KindInternal, "クォータの上書きを取得できませんでした", findError)
	}
	var specRows []model.AccountSpecQuota
	if findError := database.Where("user_id = ?", userID).Find(&specRows).Error; findError != nil {
		return model.QuotaOverrides{}, apperror.Wrap(apperror.KindInternal, "クォータの上書きを取得できませんでした", findError)
	}
	return model.QuotaOverridesOf(row, specRows), nil
}

// SetOverrides は、ユーザーの上限の上書きを、一式で置き換える(指定しなかった項目は、既定に戻る)。
//
// ユーザーのクォータの行をロックして行うので、同じユーザーの作成・拡張と、同時に起きても、判定が食い違わない。
// 既にある使用量が、新しい上限を超えていても、消さない(新しく作れなくなるだけ)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - overrides: 新しい上書き一式。
//
// 戻り値:
//   - 値が範囲外、存在しないスペックなら invalid_request。データベースのエラーは internal_error。
func (quotaRepository *QuotaRepository) SetOverrides(requestContext context.Context, userID string, overrides model.QuotaOverrides) error {
	// 空のユーザー ID は、Gorm が条件を無視しないよう、先に拒否する。
	if hasEmpty(userID) {
		return newEmptyConditionError()
	}
	// 確認と更新を、1つのトランザクションで行う。
	return quotaRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 存在するスペックの名前を読んで、値を確かめる。
		var specs []model.DeploySpec
		if findError := transaction.Find(&specs).Error; findError != nil {
			return apperror.Wrap(apperror.KindInternal, "スペックを取得できませんでした", findError)
		}
		specNames := make([]string, 0, len(specs))
		for _, spec := range specs {
			specNames = append(specNames, spec.Name)
		}
		if validateError := overrides.Validate(specNames); validateError != nil {
			return validateError
		}
		// クォータの行をロックして、項目を置き換える(nil も書くので、全ての列を書く)。
		if _, lockError := lockAccountQuota(transaction, userID); lockError != nil {
			return lockError
		}
		row := overrides.AccountQuota(userID)
		if saveError := transaction.Save(&row).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "クォータの上書きを保存できませんでした", saveError)
		}
		// スペックごとの上書きを、全て消してから、指定されたものを入れ直す。
		if deleteError := transaction.Where("user_id = ?", userID).Delete(&model.AccountSpecQuota{}).Error; deleteError != nil {
			return apperror.Wrap(apperror.KindInternal, "クォータの上書きを保存できませんでした", deleteError)
		}
		for specName, maxDeploys := range overrides.MaxDeploysBySpec {
			specRow := model.AccountSpecQuota{UserID: userID, Spec: specName, MaxDeploys: maxDeploys}
			if createError := transaction.Create(&specRow).Error; createError != nil {
				return apperror.Wrap(apperror.KindInternal, "クォータの上書きを保存できませんでした", createError)
			}
		}
		return nil
	})
}
