package repository

import (
	"context"
	"strconv"
	"strings"
	"time"

	"launchs/auth/src/model"
	"launchs/shared/apperror"
	"launchs/shared/pagination"

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

// ListAll は、全ユーザーのログイン履歴を、新しい順に取得する(管理者の画面用。存在しない名前への試行も含む)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 取得する件数の上限。
//   - after: 前のページの最後の位置。最初のページは nil(ID は、行の連番を文字列にしたもの)。
//   - result: 結果で絞り込む(success か failure)。空なら絞り込まない。
//   - userNameKeyword: ユーザー名に含まれる文字列で絞り込む(大文字小文字は区別しない)。空なら絞り込まない。
//
// 戻り値:
//   - 履歴(新しい順)と、次のページの位置(最後のページなら nil)。データベースのエラーは internal_error。
func (loginEventRepository *LoginEventRepository) ListAll(requestContext context.Context, limit int, after *pagination.Position, result string, userNameKeyword string) ([]model.LoginEvent, *pagination.Position, error) {
	// 新しい順に、確認のため1件多く読む。
	query := loginEventRepository.database.WithContext(requestContext).Order("occurred_at DESC, id DESC").Limit(limit + 1)
	// 結果で絞り込む。
	if result != "" {
		query = query.Where("result = ?", result)
	}
	// ユーザー名で絞り込む(LIKE の記号は、そのままの文字として扱う)。
	if keyword := strings.ToLower(strings.TrimSpace(userNameKeyword)); keyword != "" {
		escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(keyword)
		query = query.Where(`user_name_lower LIKE ? ESCAPE '\'`, "%"+escaped+"%")
	}
	// 2ページ目以降は、前のページの最後より後ろから読む。
	if after != nil {
		lastID, parseError := strconv.ParseInt(after.ID, 10, 64)
		if parseError != nil {
			return nil, nil, apperror.New(apperror.KindInvalidRequest, "cursor が不正です")
		}
		query = query.Where("(occurred_at, id) < (?, ?)", after.CreatedAt, lastID)
	}
	var events []model.LoginEvent
	if findError := query.Find(&events).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "ログイン履歴を取得できませんでした", findError)
	}
	// 次のページが無ければ、位置は nil。
	if len(events) <= limit {
		return events, nil, nil
	}
	// 確認のために多く読んだ1件を外して、次のページの位置を返す。
	events = events[:limit]
	lastEvent := events[limit-1]
	return events, &pagination.Position{CreatedAt: lastEvent.OccurredAt, ID: strconv.FormatInt(lastEvent.ID, 10)}, nil
}
