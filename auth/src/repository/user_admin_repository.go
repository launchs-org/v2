package repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/auth/src/model"
	"launchs/shared/apperror"
	"launchs/shared/pagination"
)

// HasAdmin は、管理者(状態を問わない)が1人でもいるかを返す。
//
// 停止中の管理者も数える。最初の管理者を作る処理が、再起動のたびに、同じ名前を作ろうとして、失敗し続けないため。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - 1人でもいれば true。データベースのエラーは internal_error。
func (userRepository *UserRepository) HasAdmin(requestContext context.Context) (bool, error) {
	// 管理者の数を数える。
	var count int64
	if countError := userRepository.database.WithContext(requestContext).Model(&model.User{}).Where("role = ?", model.RoleAdmin).Limit(1).Count(&count).Error; countError != nil {
		return false, apperror.Wrap(apperror.KindInternal, "管理者を確認できませんでした", countError)
	}
	return count > 0, nil
}

// List は、ユーザーを、新しい順に取得する(管理者用)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 1ページの件数。
//   - after: 2ページ目以降の、前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - ユーザーの一覧と、次のページの位置(最後のページなら nil)。データベースのエラーは internal_error。
func (userRepository *UserRepository) List(requestContext context.Context, limit int, after *pagination.Position) ([]model.User, *pagination.Position, error) {
	// 新しい順に、確認のため1件多く読む。
	query := userRepository.database.WithContext(requestContext).Order("created_at DESC, id DESC").Limit(limit + 1)
	// 2ページ目以降は、前のページの最後より後ろから読む。
	if after != nil {
		query = query.Where("(created_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}
	var users []model.User
	if findError := query.Find(&users).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "ユーザーの一覧を取得できませんでした", findError)
	}
	// 次のページが無ければ、位置は nil。
	if len(users) <= limit {
		return users, nil, nil
	}
	// 確認のために多く読んだ1件を外して、次のページの位置を返す。
	users = users[:limit]
	lastUser := users[limit-1]
	return users, &pagination.Position{CreatedAt: lastUser.CreatedAt, ID: lastUser.ID}, nil
}

// updateUser は、ユーザーを、ロックして読み、モデルの操作を呼び、保存する(同じトランザクションで、追加の処理も行える)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - condition: ユーザーを選ぶ条件("id = ?" か "user_name_lower = ?")。
//   - value: 条件の値。
//   - operation: モデルの操作。エラーを返したら、保存せずにそのエラーを返す。
//   - afterSave: 保存の後に、同じトランザクションで行う処理(不要なら nil)。
//
// 戻り値:
//   - ユーザーが無ければ not_found。操作や追加の処理が返したエラー。データベースのエラーは internal_error。
func (userRepository *UserRepository) updateUser(requestContext context.Context, condition string, value string, operation func(user *model.User) error, afterSave func(transaction *gorm.DB, user model.User) error) error {
	// 空の値は、Gorm が条件を無視しないよう、先に拒否する。
	if strings.TrimSpace(value) == "" {
		return apperror.New(apperror.KindNotFound, "ユーザーが見つかりません")
	}
	// 取得と更新を、1つのトランザクションで行う。
	return userRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む。
		var user model.User
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).Where(condition, value).Take(&user).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "ユーザーが見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "ユーザーを取得できませんでした", findError)
		}
		// モデルに操作を任せる。
		if operationError := operation(&user); operationError != nil {
			return operationError
		}
		// 変えたユーザーを保存する。
		if saveError := transaction.Save(&user).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "ユーザーを更新できませんでした", saveError)
		}
		// 追加の処理があれば、同じトランザクションで行う。
		if afterSave != nil {
			return afterSave(transaction, user)
		}
		return nil
	})
}

// Ban は、ユーザーを停止する。同じトランザクションで、そのユーザーの有効な refresh token を全て無効にする。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - actorID: 操作する管理者の ID。
//   - userID: 停止するユーザーの ID。
//
// 戻り値:
//   - ユーザーが無ければ not_found。自分自身なら invalid_request。すでに停止中なら conflict。
func (userRepository *UserRepository) Ban(requestContext context.Context, actorID string, userID string) error {
	// モデルに停止を任せ、保存の後に、refresh token を全て無効にする。
	return userRepository.updateUser(requestContext, "id = ?", userID,
		func(user *model.User) error { return user.Ban(actorID) },
		func(transaction *gorm.DB, user model.User) error {
			revokeError := transaction.Exec("UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = ? AND revoked_at IS NULL", user.ID).Error
			if revokeError != nil {
				return apperror.Wrap(apperror.KindInternal, "refresh token を無効にできませんでした", revokeError)
			}
			return nil
		})
}

// Unban は、停止中のユーザーを、使える状態に戻す(無効にした refresh token は、戻らない。ログインし直す)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザーの ID。
//
// 戻り値:
//   - ユーザーが無ければ not_found。停止中でなければ conflict。
func (userRepository *UserRepository) Unban(requestContext context.Context, userID string) error {
	// モデルに任せる。
	return userRepository.updateUser(requestContext, "id = ?", userID, func(user *model.User) error { return user.Unban() }, nil)
}

// PromoteToAdmin は、ユーザー名(小文字)で指定したユーザーを、管理者にする。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userNameLower: 小文字のユーザー名。
//
// 戻り値:
//   - ユーザーが無ければ not_found。すでに管理者、または停止中なら conflict。
func (userRepository *UserRepository) PromoteToAdmin(requestContext context.Context, userNameLower string) error {
	// モデルに任せる。
	return userRepository.updateUser(requestContext, "user_name_lower = ?", userNameLower, func(user *model.User) error { return user.PromoteToAdmin() }, nil)
}
