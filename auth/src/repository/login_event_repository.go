package repository

import (
	"context"
	"time"

	"launchs/auth/src/model"
	"launchs/shared/apperror"

	"gorm.io/gorm"
)

// LoginEventRepository は、login_events テーブル(ログイン履歴)を担当する。
type LoginEventRepository struct {
	database *gorm.DB
}

// NewLoginEventRepository は、LoginEventRepository を作る。
//
// 引数:
//   - database: auth データベースの接続。
//
// 戻り値:
//   - 作成した LoginEventRepository。
func NewLoginEventRepository(database *gorm.DB) *LoginEventRepository {
	return &LoginEventRepository{database: database}
}

// Create は、履歴を1件保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - event: 保存する履歴(ID と OccurredAt は、データベースが決める)。
//
// 戻り値:
//   - データベースのエラー。
func (loginEventRepository *LoginEventRepository) Create(requestContext context.Context, event model.LoginEvent) error {
	// 保存する(ID と日時は、データベースの既定値を使う)。
	createError := loginEventRepository.database.WithContext(requestContext).Omit("id", "occurred_at").Create(&event).Error
	if createError != nil {
		return apperror.Wrap(apperror.KindInternal, "ログイン履歴を保存できませんでした", createError)
	}
	return nil
}

// ListByUser は、ユーザーの履歴を、新しい順に取得する(そのユーザーへの試行だけ)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザーの ID。
//   - limit: 取得する件数の上限。
//
// 戻り値:
//   - 履歴(新しい順)。データベースのエラーは internal_error。
func (loginEventRepository *LoginEventRepository) ListByUser(requestContext context.Context, userID string, limit int) ([]model.LoginEvent, error) {
	// 空の ID では、どのユーザーの履歴も返さない。
	if userID == "" {
		return []model.LoginEvent{}, nil
	}
	// そのユーザーの履歴を、新しい順に取る。
	var events []model.LoginEvent
	findError := loginEventRepository.database.WithContext(requestContext).
		Where("user_id = ?", userID).Order("occurred_at DESC, id DESC").Limit(limit).Find(&events).Error
	if findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ログイン履歴を取得できませんでした", findError)
	}
	return events, nil
}

// DeleteOlderThan は、古い履歴を消す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - olderThan: この時間より前の履歴を消す。
//
// 戻り値:
//   - 消した数。データベースのエラーは internal_error。
func (loginEventRepository *LoginEventRepository) DeleteOlderThan(requestContext context.Context, olderThan time.Duration) (int64, error) {
	// 古い履歴を消す。
	result := loginEventRepository.database.WithContext(requestContext).Exec(
		"DELETE FROM login_events WHERE occurred_at < now() - make_interval(secs => ?)", olderThan.Seconds())
	if result.Error != nil {
		return 0, apperror.Wrap(apperror.KindInternal, "古いログイン履歴を消せませんでした", result.Error)
	}
	return result.RowsAffected, nil
}
