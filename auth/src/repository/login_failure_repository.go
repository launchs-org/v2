package repository

import (
	"context"
	"time"

	"launchs/shared/apperror"

	"gorm.io/gorm"
)

// LoginFailureRepository は、login_failures テーブル(ユーザー名ごとのログイン失敗とロック)を担当する。
type LoginFailureRepository struct {
	database *gorm.DB
}

// NewLoginFailureRepository は、LoginFailureRepository を作る。
//
// 引数:
//   - database: auth データベースの接続。
//
// 戻り値:
//   - 作成した LoginFailureRepository。
func NewLoginFailureRepository(database *gorm.DB) *LoginFailureRepository {
	return &LoginFailureRepository{database: database}
}

// IsLocked は、ユーザー名が、いまロック中かを返す(データベースの時計で判定する)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userNameLower: 小文字にしたユーザー名。
//
// 戻り値:
//   - ロック中なら true。データベースのエラーは internal_error。
func (loginFailureRepository *LoginFailureRepository) IsLocked(requestContext context.Context, userNameLower string) (bool, error) {
	// ロック中の行の数を数える。
	var count int64
	countError := loginFailureRepository.database.WithContext(requestContext).Table("login_failures").
		Where("user_name_lower = ? AND locked_until > now()", userNameLower).Count(&count).Error
	if countError != nil {
		return false, apperror.Wrap(apperror.KindInternal, "ロックの状態を確認できませんでした", countError)
	}
	return count > 0, nil
}

// RecordFailure は、ログインの失敗を1回記録し、回数が上限に達したら、ロックする(回数は 0 に戻す)。
//
// 行の更新は、1つの SQL で行うので、同時に失敗が来ても、数え落とさない。存在しないユーザー名にも、同じように記録する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userNameLower: 小文字にしたユーザー名。
//   - maxFailures: ロックする失敗の回数。
//   - lockDuration: ロックする時間。
//
// 戻り値:
//   - データベースのエラー。
func (loginFailureRepository *LoginFailureRepository) RecordFailure(requestContext context.Context, userNameLower string, maxFailures int, lockDuration time.Duration) error {
	// 追加する。すでにあれば、回数を増やし、上限に達したら、ロックして、回数を 0 に戻す。
	execError := loginFailureRepository.database.WithContext(requestContext).Exec(`
		INSERT INTO login_failures (user_name_lower, failed_count, locked_until, updated_at)
		VALUES (?, CASE WHEN ? <= 1 THEN 0 ELSE 1 END, CASE WHEN ? <= 1 THEN now() + make_interval(secs => ?) END, now())
		ON CONFLICT (user_name_lower) DO UPDATE SET
			failed_count = CASE WHEN login_failures.failed_count + 1 >= ? THEN 0 ELSE login_failures.failed_count + 1 END,
			locked_until = CASE WHEN login_failures.failed_count + 1 >= ? THEN now() + make_interval(secs => ?) ELSE login_failures.locked_until END,
			updated_at = now()`,
		userNameLower, maxFailures, maxFailures, lockDuration.Seconds(), maxFailures, maxFailures, lockDuration.Seconds()).Error
	if execError != nil {
		return apperror.Wrap(apperror.KindInternal, "ログインの失敗を記録できませんでした", execError)
	}
	return nil
}

// Reset は、ユーザー名の失敗の記録を消す(ログインに成功したとき)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userNameLower: 小文字にしたユーザー名。
//
// 戻り値:
//   - データベースのエラー。
func (loginFailureRepository *LoginFailureRepository) Reset(requestContext context.Context, userNameLower string) error {
	// 空の名前では、何もしない。
	if userNameLower == "" {
		return nil
	}
	// その行を消す。
	deleteError := loginFailureRepository.database.WithContext(requestContext).Exec("DELETE FROM login_failures WHERE user_name_lower = ?", userNameLower).Error
	if deleteError != nil {
		return apperror.Wrap(apperror.KindInternal, "ログインの失敗の記録を消せませんでした", deleteError)
	}
	return nil
}

// DeleteStale は、ロックが終わっていて、しばらく更新が無い行を消す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - olderThan: この時間より前に更新された行を消す。
//
// 戻り値:
//   - 消した数。データベースのエラーは internal_error。
func (loginFailureRepository *LoginFailureRepository) DeleteStale(requestContext context.Context, olderThan time.Duration) (int64, error) {
	// ロック中でなく、古い行を消す。
	result := loginFailureRepository.database.WithContext(requestContext).Exec(
		"DELETE FROM login_failures WHERE (locked_until IS NULL OR locked_until <= now()) AND updated_at < now() - make_interval(secs => ?)", olderThan.Seconds())
	if result.Error != nil {
		return 0, apperror.Wrap(apperror.KindInternal, "古いログインの失敗の記録を消せませんでした", result.Error)
	}
	return result.RowsAffected, nil
}
