package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// lastUsedUpdateInterval は、最後に使った日時を更新する最短の間隔(書き込みを減らす)。
const lastUsedUpdateInterval = time.Minute

// APITokenRepository は、API トークンの有効・無効の管理を、データベースで行う。
type APITokenRepository struct {
	database *gorm.DB
}

// NewAPITokenRepository は、APITokenRepository を作る。
//
// 引数:
//   - database: データベースの接続。
//
// 戻り値:
//   - 作成した APITokenRepository。
func NewAPITokenRepository(database *gorm.DB) *APITokenRepository {
	// 接続を持たせて返す。
	return &APITokenRepository{database: database}
}

// Create は、発行した API トークンを、有効なものとして保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - token: 保存する行。
//
// 戻り値:
//   - 保存できなかったときは internal_error。
func (repository *APITokenRepository) Create(requestContext context.Context, token *model.APIToken) error {
	// 1行を追加する。
	if createError := repository.database.WithContext(requestContext).Create(token).Error; createError != nil {
		return apperror.Wrap(apperror.KindInternal, "API トークンを保存できませんでした", createError)
	}
	return nil
}

// ListByUser は、利用者の API トークンを、新しい順に返す(失効したものも含む)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//
// 戻り値:
//   - トークンの一覧。取得できなかったときは internal_error。
func (repository *APITokenRepository) ListByUser(requestContext context.Context, userID string) ([]model.APIToken, error) {
	// 条件が空だと、他の人のものまで返るので、先に拒否する。
	if hasEmpty(userID) {
		return nil, newEmptyConditionError()
	}
	// 利用者のものだけを、新しい順に取る。
	var tokens []model.APIToken
	if findError := repository.database.WithContext(requestContext).Where("user_id = ?", userID).Order("created_at DESC").Find(&tokens).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "API トークンを取得できませんでした", findError)
	}
	return tokens, nil
}

// Revoke は、API トークンを失効させる(持ち主のものだけ)。すでに失効していても、成功にする。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - tokenID: 失効させるトークンの ID。
//
// 戻り値:
//   - 無い、または他のユーザーのものなら not_found。それ以外のデータベースのエラーは internal_error。
func (repository *APITokenRepository) Revoke(requestContext context.Context, userID string, tokenID string) error {
	// 条件が空のときは拒否する。
	if hasEmpty(userID, tokenID) {
		return newEmptyConditionError()
	}
	// まだ失効していないものだけ、失効の日時を入れる。
	result := repository.database.WithContext(requestContext).Model(&model.APIToken{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", tokenID, userID).Update("revoked_at", time.Now())
	if result.Error != nil {
		return apperror.Wrap(apperror.KindInternal, "API トークンを失効できませんでした", result.Error)
	}
	// 更新が 0 行のときは、無いか、すでに失効している。持ち主の行があるかで、見分ける。
	if result.RowsAffected == 0 {
		var count int64
		if countError := repository.database.WithContext(requestContext).Model(&model.APIToken{}).Where("id = ? AND user_id = ?", tokenID, userID).Count(&count).Error; countError != nil {
			return apperror.Wrap(apperror.KindInternal, "API トークンを確認できませんでした", countError)
		}
		if count == 0 {
			return apperror.New(apperror.KindNotFound, "API トークンが見つかりません")
		}
	}
	return nil
}

// IsActive は、API トークンが有効(保存されていて、失効していない)かを確かめ、使った日時を更新する。
//
// 期限は、トークンの claim が持つので、ここでは見ない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - tokenID: トークンの ID(jti)。
//
// 戻り値:
//   - 有効なら true。無い、または失効しているなら false。データベースのエラーは internal_error。
func (repository *APITokenRepository) IsActive(requestContext context.Context, tokenID string) (bool, error) {
	// 条件が空のときは、有効ではない。
	if hasEmpty(tokenID) {
		return false, nil
	}
	// 行を読む。
	var token model.APIToken
	findResult := repository.database.WithContext(requestContext).Where("id = ?", tokenID).Limit(1).Find(&token)
	if findResult.Error != nil {
		return false, apperror.Wrap(apperror.KindInternal, "API トークンを確認できませんでした", findResult.Error)
	}
	// 無い、または失効しているなら、無効。
	if findResult.RowsAffected == 0 || token.RevokedAt != nil {
		return false, nil
	}
	// 最後に使った日時は、間隔をあけて更新する(失敗しても、認証は成功にする)。
	now := time.Now()
	if token.LastUsedAt == nil || now.Sub(*token.LastUsedAt) > lastUsedUpdateInterval {
		_ = repository.database.WithContext(requestContext).Model(&model.APIToken{}).Where("id = ?", tokenID).Update("last_used_at", now).Error
	}
	return true, nil
}
