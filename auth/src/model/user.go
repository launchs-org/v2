// Package model は、auth データベースのテーブルに対応するモデルを定義する。
package model

import (
	"regexp"
	"strings"
	"time"

	"launchs/shared/apperror"
)

const (
	// UserIDPrefix は、ユーザーの ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。
	UserIDPrefix = "userid-"

	// UserStatusActive と UserStatusBanned は、ユーザーの状態。
	UserStatusActive = "active"
	UserStatusBanned = "banned"

	// RoleUser と RoleAdmin は、ユーザーの役割。
	RoleUser  = "user"
	RoleAdmin = "admin"

	// MaxUserNameLength は、ユーザー名の長さの上限。
	MaxUserNameLength = 64
)

// userNamePattern は、ユーザー名として受け付ける形(英数字とハイフンだけ)。
var userNamePattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// User は、users テーブルの1行。
type User struct {
	// ID は、ユーザーの ID("userid-" + UUID v4)。
	ID string `gorm:"column:id;primaryKey"`

	// UserName は、利用者が指定した名前。
	UserName string `gorm:"column:user_name"`

	// UserNameLower は、重複の判定に使う、小文字の名前。
	UserNameLower string `gorm:"column:user_name_lower"`

	// PasswordHash は、argon2id のハッシュ。
	PasswordHash string `gorm:"column:password_hash"`

	// Status は、状態(active か banned)。
	Status string `gorm:"column:status"`

	// Role は、役割(user か admin)。
	Role string `gorm:"column:role"`

	// CreatedAt は、作成日時。
	CreatedAt time.Time `gorm:"column:created_at"`

	// UpdatedAt は、更新日時。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("users")。
func (User) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "users"
}

// IsActive は、ユーザーがログインできる状態かを返す。
//
// 戻り値:
//   - active なら true。
func (user User) IsActive() bool {
	// banned のユーザーは、ログインできない。
	return user.Status == UserStatusActive
}

// NormalizeUserName は、重複の判定に使う、小文字の名前を返す。
//
// 引数:
//   - userName: ユーザー名。
//
// 戻り値:
//   - 小文字にした名前。
func NormalizeUserName(userName string) string {
	// 大文字小文字を区別しないので、小文字にそろえる。
	return strings.ToLower(userName)
}

// ValidateUserName は、ユーザー名の形(英数字とハイフンだけ、1〜64文字)を確かめる。
//
// 引数:
//   - userName: 確かめる名前。
//
// 戻り値:
//   - 不正なら invalid_request。
func ValidateUserName(userName string) error {
	// 長さと文字の種類を確かめる。
	if len(userName) < 1 || len(userName) > MaxUserNameLength || !userNamePattern.MatchString(userName) {
		return apperror.New(apperror.KindInvalidRequest, "userName は、英数字とハイフンだけの1〜64文字でなければなりません")
	}
	return nil
}

// RefreshToken は、refresh_tokens テーブルの1行。refresh token の有効・無効を、jti ごとに管理する。
type RefreshToken struct {
	// JTI は、トークンの jti(主キー)。
	JTI string `gorm:"column:jti;primaryKey"`

	// UserID は、持ち主のユーザーの ID。
	UserID string `gorm:"column:user_id"`

	// IssuedAt は、発行日時。
	IssuedAt time.Time `gorm:"column:issued_at"`

	// ExpiresAt は、有効期限。
	ExpiresAt time.Time `gorm:"column:expires_at"`

	// RevokedAt は、無効にした日時。有効なら nil。
	RevokedAt *time.Time `gorm:"column:revoked_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("refresh_tokens")。
func (RefreshToken) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "refresh_tokens"
}

// Invitation は、invitations テーブルの1行。招待制のときの招待。
type Invitation struct {
	// CodeHash は、招待コードのハッシュ(主キー)。
	CodeHash string `gorm:"column:code_hash;primaryKey"`

	// CreatedBy は、発行した管理者の ID。
	CreatedBy *string `gorm:"column:created_by"`

	// ExpiresAt は、有効期限。
	ExpiresAt time.Time `gorm:"column:expires_at"`

	// UsedBy は、使ったユーザーの ID。未使用なら nil。
	UsedBy *string `gorm:"column:used_by"`

	// UsedAt は、使った日時。未使用なら nil。
	UsedAt *time.Time `gorm:"column:used_at"`

	// CreatedAt は、作成日時。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("invitations")。
func (Invitation) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "invitations"
}

const (
	// LoginEventKindSignup と LoginEventKindLogin は、ログイン履歴の種類。
	LoginEventKindSignup = "signup"
	LoginEventKindLogin  = "login"

	// LoginEventSuccess と LoginEventFailure は、ログイン履歴の結果。
	LoginEventSuccess = "success"
	LoginEventFailure = "failure"

	// 失敗の理由。
	LoginReasonUnknownUser   = "unknown_user"
	LoginReasonWrongPassword = "wrong_password"
	LoginReasonLocked        = "locked"
	LoginReasonBanned        = "banned"
	LoginReasonInvalidInput  = "invalid_input"
)

// LoginEvent は、login_events テーブルの1行。サインアップとログインの、成功と失敗の履歴。
type LoginEvent struct {
	// ID は、行の ID(連番)。
	ID int64 `gorm:"column:id;primaryKey"`

	// UserID は、持ち主のユーザーの ID。存在しないユーザー名への試行は nil。
	UserID *string `gorm:"column:user_id"`

	// UserNameLower は、試行で指定された名前(小文字)。
	UserNameLower string `gorm:"column:user_name_lower"`

	// Kind は、種類(signup か login)。
	Kind string `gorm:"column:kind"`

	// Result は、結果(success か failure)。
	Result string `gorm:"column:result"`

	// Reason は、失敗の理由。成功のときは nil。
	Reason *string `gorm:"column:reason"`

	// IP は、クライアントの IP。
	IP string `gorm:"column:ip"`

	// UserAgent は、クライアントの User-Agent(長いときは切り詰める)。
	UserAgent string `gorm:"column:user_agent"`

	// OccurredAt は、試行の日時。
	OccurredAt time.Time `gorm:"column:occurred_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("login_events")。
func (LoginEvent) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "login_events"
}
