// Package repository は、auth データベースの読み書きを担当する。
//
// 検索の条件は、文字列で書く(`Where("列 = ?", 値)`)。構造体で条件を書くと、値が空のとき、Gorm が条件を無視して、
// 他の行が対象になってしまうため。
package repository

import (
	"context"
	"errors"

	"launchs/auth/src/model"
	"launchs/shared/apperror"

	"gorm.io/gorm"
)

// UserRepository は、users テーブルの読み書きを担当する。
type UserRepository struct {
	database *gorm.DB
}

// NewUserRepository は、UserRepository を作る。
//
// 引数:
//   - database: auth データベースの接続。
//
// 戻り値:
//   - 作成した UserRepository。
func NewUserRepository(database *gorm.DB) *UserRepository {
	return &UserRepository{database: database}
}

// Create は、ユーザーを保存する。招待コードのハッシュがあるときは、同じトランザクションで、招待を使用済みにする。
//
// 名前が重なったとき(大文字小文字を区別しない)や、招待が使えないとき(無い、期限切れ、使用済み)は、
// ユーザーも作らず、招待も使用済みにしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - user: 保存するユーザー(ID、UserName、UserNameLower、PasswordHash、Status、Role を、設定しておく)。
//   - invitationCodeHash: 使う招待コードのハッシュ。招待制でないときは、空。
//
// 戻り値:
//   - 名前が重なったときは conflict。招待が使えないときは invalid_request。それ以外のデータベースのエラーは internal_error。
func (userRepository *UserRepository) Create(requestContext context.Context, user *model.User, invitationCodeHash string) error {
	// 招待の消費とユーザーの保存を、1つのトランザクションで行う。
	return userRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 招待があるときは、使用済みにする(使えないなら、0行になる)。
		if invitationCodeHash != "" {
			consumed := transaction.Model(&model.Invitation{}).
				Where("code_hash = ? AND used_at IS NULL AND expires_at > now()", invitationCodeHash).
				Updates(map[string]any{"used_by": user.ID, "used_at": gorm.Expr("now()")})
			if consumed.Error != nil {
				return apperror.Wrap(apperror.KindInternal, "招待を確認できませんでした", consumed.Error)
			}
			if consumed.RowsAffected == 0 {
				return apperror.New(apperror.KindInvalidRequest, "招待コードが正しくないか、期限が切れています")
			}
		}
		// ユーザーを保存する。名前の重複は、conflict にする。
		createError := transaction.Create(user).Error
		if errors.Is(createError, gorm.ErrDuplicatedKey) {
			return apperror.New(apperror.KindConflict, "その userName は、すでに使われています")
		}
		if createError != nil {
			return apperror.Wrap(apperror.KindInternal, "ユーザーを保存できませんでした", createError)
		}
		return nil
	})
}

// FindByNameLower は、小文字の名前で、ユーザーを探す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userNameLower: 小文字にしたユーザー名。
//
// 戻り値:
//   - ユーザーと、見つかったか。データベースのエラーは internal_error。
func (userRepository *UserRepository) FindByNameLower(requestContext context.Context, userNameLower string) (model.User, bool, error) {
	// 空の名前は、どのユーザーでもない。
	if userNameLower == "" {
		return model.User{}, false, nil
	}
	// 小文字の名前で探す。
	var user model.User
	findError := userRepository.database.WithContext(requestContext).Where("user_name_lower = ?", userNameLower).Take(&user).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.User{}, false, nil
	}
	if findError != nil {
		return model.User{}, false, apperror.Wrap(apperror.KindInternal, "ユーザーを取得できませんでした", findError)
	}
	return user, true, nil
}

// FindByID は、ID で、ユーザーを探す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザーの ID。
//
// 戻り値:
//   - ユーザーと、見つかったか。データベースのエラーは internal_error。
func (userRepository *UserRepository) FindByID(requestContext context.Context, userID string) (model.User, bool, error) {
	// 空の ID は、どのユーザーでもない。
	if userID == "" {
		return model.User{}, false, nil
	}
	// ID で探す。
	var user model.User
	findError := userRepository.database.WithContext(requestContext).Where("id = ?", userID).Take(&user).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.User{}, false, nil
	}
	if findError != nil {
		return model.User{}, false, apperror.Wrap(apperror.KindInternal, "ユーザーを取得できませんでした", findError)
	}
	return user, true, nil
}

// UpdatePasswordHash は、ユーザーのパスワードのハッシュを、新しいものにする(パラメータを強くしたときの、作り直し)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザーの ID。
//   - passwordHash: 新しいハッシュ。
//
// 戻り値:
//   - データベースのエラー。
func (userRepository *UserRepository) UpdatePasswordHash(requestContext context.Context, userID string, passwordHash string) error {
	// 空の値で更新しない。
	if userID == "" || passwordHash == "" {
		return apperror.New(apperror.KindInternal, "更新する値が空です")
	}
	// ハッシュを更新する(更新日時は Gorm が更新する)。
	updateError := userRepository.database.WithContext(requestContext).Model(&model.User{}).
		Where("id = ?", userID).Update("password_hash", passwordHash).Error
	if updateError != nil {
		return apperror.Wrap(apperror.KindInternal, "パスワードのハッシュを更新できませんでした", updateError)
	}
	return nil
}
