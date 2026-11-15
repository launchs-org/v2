package model

import (
	"time"

	"launchs/shared/apperror"
)

const (
	// InvitationIDPrefix は、招待の ID の接頭辞。
	InvitationIDPrefix = "invitation-"

	// InvitationStatusActive 以下は、招待の状態(保存はせず、Status で、列から求める)。
	InvitationStatusActive  = "active"
	InvitationStatusUsed    = "used"
	InvitationStatusExpired = "expired"
	InvitationStatusRevoked = "revoked"

	// DefaultInvitationLifetime と MaxInvitationLifetime は、招待の有効期間の既定と上限。
	DefaultInvitationLifetime = 7 * 24 * time.Hour
	MaxInvitationLifetime     = 30 * 24 * time.Hour

	// MaxInvitationNoteLength は、招待のメモの長さの上限(バイト)。
	MaxInvitationNoteLength = 200
)

// Ban は、ユーザーを停止中(banned)にする。
//
// 引数:
//   - actorID: 操作する管理者の ID。自分自身は、停止できない(誰も管理できなくなるのを防ぐ)。
//
// 戻り値:
//   - 自分自身なら invalid_request。すでに停止中なら conflict。
func (user *User) Ban(actorID string) error {
	// 自分自身は、停止できない。
	if user.ID == actorID {
		return apperror.New(apperror.KindInvalidRequest, "自分自身は停止できません")
	}
	// すでに停止中なら、何も変えない。
	if user.Status == UserStatusBanned {
		return apperror.New(apperror.KindConflict, "すでに停止されています")
	}
	// 停止中にする。
	user.Status = UserStatusBanned
	return nil
}

// Unban は、停止中のユーザーを、使える状態(active)に戻す。
//
// 戻り値:
//   - 停止中でなければ conflict。
func (user *User) Unban() error {
	// 停止中でなければ、戻すものが無い。
	if user.Status != UserStatusBanned {
		return apperror.New(apperror.KindConflict, "停止されていません")
	}
	// 使える状態に戻す。
	user.Status = UserStatusActive
	return nil
}

// PromoteToAdmin は、ユーザーを管理者にする。
//
// 戻り値:
//   - すでに管理者なら conflict。停止中のユーザーは、管理者にできない(conflict)。
func (user *User) PromoteToAdmin() error {
	// すでに管理者なら、何も変えない。
	if user.Role == RoleAdmin {
		return apperror.New(apperror.KindConflict, "すでに管理者です")
	}
	// 停止中のユーザーは、管理者にしない(先に解除する)。
	if !user.IsActive() {
		return apperror.New(apperror.KindConflict, "停止中のユーザーは、管理者にできません")
	}
	// 管理者にする。
	user.Role = RoleAdmin
	return nil
}

// CanAdminister は、ユーザーが、管理の操作をできるか(使える状態の管理者か)を返す。
//
// 戻り値:
//   - できるなら true。
func (user User) CanAdminister() bool {
	// 管理者で、停止中でないこと。
	return user.Role == RoleAdmin && user.IsActive()
}

// Status は、招待の状態を、列から求める。取り消し、使用済み、期限切れ、有効の順に判定する。
//
// 引数:
//   - now: 現在の時刻。
//
// 戻り値:
//   - 招待の状態。
func (invitation Invitation) Status(now time.Time) string {
	// 取り消しが最優先。
	if invitation.RevokedAt != nil {
		return InvitationStatusRevoked
	}
	// 使用済みか。
	if invitation.UsedAt != nil {
		return InvitationStatusUsed
	}
	// 期限が切れているか。
	if !invitation.ExpiresAt.After(now) {
		return InvitationStatusExpired
	}
	return InvitationStatusActive
}

// Revoke は、招待を取り消す。
//
// 引数:
//   - now: 現在の時刻。
//
// 戻り値:
//   - 有効でない(使用済み、期限切れ、すでに取り消し済み)なら conflict。
func (invitation *Invitation) Revoke(now time.Time) error {
	// 有効な招待だけを、取り消せる。
	if status := invitation.Status(now); status != InvitationStatusActive {
		return apperror.New(apperror.KindConflict, "この招待は、取り消せません(状態: "+status+")")
	}
	// 取り消した日時を入れる。
	invitation.RevokedAt = &now
	return nil
}
