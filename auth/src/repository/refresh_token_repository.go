package repository

import (
	"context"
	"time"

	"launchs/auth/src/model"
	"launchs/shared/apperror"

	"gorm.io/gorm"
)

// RefreshTokenRepository は、refresh_tokens テーブルの読み書きを担当する。
type RefreshTokenRepository struct {
	database *gorm.DB
}

// NewRefreshTokenRepository は、RefreshTokenRepository を作る。
//
// 引数:
//   - database: auth データベースの接続。
//
// 戻り値:
//   - 作成した RefreshTokenRepository。
func NewRefreshTokenRepository(database *gorm.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{database: database}
}

// Create は、発行した refresh token を、有効なものとして保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - jti: トークンの jti。
//   - userID: 持ち主のユーザーの ID。
//   - expiresAt: 有効期限。
//
// 戻り値:
//   - データベースのエラー。
func (refreshTokenRepository *RefreshTokenRepository) Create(requestContext context.Context, jti string, userID string, expiresAt time.Time) error {
	// 保存する。
	record := model.RefreshToken{JTI: jti, UserID: userID, ExpiresAt: expiresAt}
	if createError := refreshTokenRepository.database.WithContext(requestContext).Create(&record).Error; createError != nil {
		return apperror.Wrap(apperror.KindInternal, "refresh token を保存できませんでした", createError)
	}
	return nil
}

// FindActiveOwner は、jti が、有効(無効にされておらず、期限が切れていない)なら、その持ち主のユーザーの ID を返す。
//
// 期限は、データベースの時計で判定する(複数のコンテナで、時計がずれても、判定が同じになるため)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - jti: トークンの jti。
//
// 戻り値:
//   - 持ち主のユーザーの ID と、有効だったか。データベースのエラーは internal_error。
func (refreshTokenRepository *RefreshTokenRepository) FindActiveOwner(requestContext context.Context, jti string) (string, bool, error) {
	// 空の jti は、有効なトークンではない。
	if jti == "" {
		return "", false, nil
	}
	// 有効な行を探す。
	var userIDs []string
	findError := refreshTokenRepository.database.WithContext(requestContext).Model(&model.RefreshToken{}).
		Where("jti = ? AND revoked_at IS NULL AND expires_at > now()", jti).Limit(1).Pluck("user_id", &userIDs).Error
	if findError != nil {
		return "", false, apperror.Wrap(apperror.KindInternal, "refresh token を確認できませんでした", findError)
	}
	if len(userIDs) == 0 {
		return "", false, nil
	}
	return userIDs[0], true, nil
}

// Revoke は、jti を無効にする(ログアウト)。すでに無効、または期限切れのものは、対象にしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - jti: トークンの jti。
//
// 戻り値:
//   - 無効にしたら true。すでに無効・期限切れ・無いときは false。データベースのエラーは internal_error。
func (refreshTokenRepository *RefreshTokenRepository) Revoke(requestContext context.Context, jti string) (bool, error) {
	// 空の jti は、対象が無い。
	if jti == "" {
		return false, nil
	}
	// 有効なものだけを、無効にする。
	result := refreshTokenRepository.database.WithContext(requestContext).Model(&model.RefreshToken{}).
		Where("jti = ? AND revoked_at IS NULL AND expires_at > now()", jti).
		Update("revoked_at", gorm.Expr("now()"))
	if result.Error != nil {
		return false, apperror.Wrap(apperror.KindInternal, "refresh token を無効にできませんでした", result.Error)
	}
	return result.RowsAffected > 0, nil
}

// RevokeAllForUser は、ユーザーの有効な refresh token を、全て無効にする(BAN のとき)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザーの ID。
//
// 戻り値:
//   - 無効にした数。データベースのエラーは internal_error。
func (refreshTokenRepository *RefreshTokenRepository) RevokeAllForUser(requestContext context.Context, userID string) (int64, error) {
	// 空の ID では、何もしない。
	if userID == "" {
		return 0, nil
	}
	// そのユーザーの、有効なものだけを、無効にする。
	result := refreshTokenRepository.database.WithContext(requestContext).Model(&model.RefreshToken{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", gorm.Expr("now()"))
	if result.Error != nil {
		return 0, apperror.Wrap(apperror.KindInternal, "refresh token を無効にできませんでした", result.Error)
	}
	return result.RowsAffected, nil
}

// DeleteExpired は、期限が切れた refresh token の行を消す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - 消した数。データベースのエラーは internal_error。
func (refreshTokenRepository *RefreshTokenRepository) DeleteExpired(requestContext context.Context) (int64, error) {
	// 期限が切れたものを消す。
	result := refreshTokenRepository.database.WithContext(requestContext).Where("expires_at <= now()").Delete(&model.RefreshToken{})
	if result.Error != nil {
		return 0, apperror.Wrap(apperror.KindInternal, "期限切れの refresh token を消せませんでした", result.Error)
	}
	return result.RowsAffected, nil
}
