package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/auth/src/model"
	"launchs/shared/apperror"
	"launchs/shared/pagination"
)

// InvitationRepository は、招待の保存と取得(管理者用)。招待を使う処理は、UserRepository.Create が、ユーザーの作成と同じトランザクションで行う。
type InvitationRepository struct {
	database *gorm.DB
}

// NewInvitationRepository は、InvitationRepository を作る。
//
// 引数:
//   - database: 使うデータベース。
//
// 戻り値:
//   - 作成した InvitationRepository。
func NewInvitationRepository(database *gorm.DB) *InvitationRepository {
	// データベースを持たせて返す。
	return &InvitationRepository{database: database}
}

// Create は、招待を1件保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - invitation: 保存する招待(ID とコードのハッシュは、呼び出し側で作っておく)。
//
// 戻り値:
//   - データベースのエラーは internal_error。
func (invitationRepository *InvitationRepository) Create(requestContext context.Context, invitation *model.Invitation) error {
	// 保存する。
	if createError := invitationRepository.database.WithContext(requestContext).Create(invitation).Error; createError != nil {
		return apperror.Wrap(apperror.KindInternal, "招待を保存できませんでした", createError)
	}
	return nil
}

// List は、招待を、新しい順に取得する(コードは、保存していないので、返せない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 1ページの件数。
//   - after: 2ページ目以降の、前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - 招待の一覧と、次のページの位置(最後のページなら nil)。データベースのエラーは internal_error。
func (invitationRepository *InvitationRepository) List(requestContext context.Context, limit int, after *pagination.Position) ([]model.Invitation, *pagination.Position, error) {
	// 新しい順に、確認のため1件多く読む。
	query := invitationRepository.database.WithContext(requestContext).Order("created_at DESC, id DESC").Limit(limit + 1)
	// 2ページ目以降は、前のページの最後より後ろから読む。
	if after != nil {
		query = query.Where("(created_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}
	var invitations []model.Invitation
	if findError := query.Find(&invitations).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "招待の一覧を取得できませんでした", findError)
	}
	// 次のページが無ければ、位置は nil。
	if len(invitations) <= limit {
		return invitations, nil, nil
	}
	// 確認のために多く読んだ1件を外して、次のページの位置を返す。
	invitations = invitations[:limit]
	lastInvitation := invitations[limit-1]
	return invitations, &pagination.Position{CreatedAt: lastInvitation.CreatedAt, ID: lastInvitation.ID}, nil
}

// Revoke は、招待を取り消す。行をロックして読み、モデルに取り消しを任せる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - invitationID: 招待の ID。
//
// 戻り値:
//   - 招待が無ければ not_found。有効でない(使用済み、期限切れ、取り消し済み)なら conflict。
func (invitationRepository *InvitationRepository) Revoke(requestContext context.Context, invitationID string) error {
	// 空の値は、Gorm が条件を無視しないよう、先に拒否する。
	if strings.TrimSpace(invitationID) == "" {
		return apperror.New(apperror.KindNotFound, "招待が見つかりません")
	}
	// 取得と更新を、1つのトランザクションで行う(ユーザーの作成での消費と、行のロックで競合を避ける)。
	return invitationRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		var invitation model.Invitation
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", invitationID).Take(&invitation).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "招待が見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "招待を取得できませんでした", findError)
		}
		// モデルに取り消しを任せる。
		if revokeError := invitation.Revoke(time.Now()); revokeError != nil {
			return revokeError
		}
		// 取り消しを保存する。
		if saveError := transaction.Model(&model.Invitation{}).Where("id = ?", invitationID).Update("revoked_at", invitation.RevokedAt).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "招待を取り消せませんでした", saveError)
		}
		return nil
	})
}
