package service

import (
	"context"
	"time"

	"launchs/auth/src/model"
)

// UserStore は、ユースケースが使う、ユーザーの保存先。
type UserStore interface {
	// Create は、ユーザーを保存する(招待のハッシュがあれば、同じトランザクションで使用済みにする)。
	Create(requestContext context.Context, user *model.User, invitationCodeHash string) error

	// FindByNameLower は、小文字の名前で、ユーザーを探す。
	FindByNameLower(requestContext context.Context, userNameLower string) (model.User, bool, error)

	// FindByID は、ID で、ユーザーを探す。
	FindByID(requestContext context.Context, userID string) (model.User, bool, error)

	// UpdatePasswordHash は、パスワードのハッシュを、新しいものにする。
	UpdatePasswordHash(requestContext context.Context, userID string, passwordHash string) error
}

// RefreshTokenStore は、ユースケースが使う、refresh token の有効・無効の管理。
type RefreshTokenStore interface {
	// Create は、発行した refresh token を、有効なものとして保存する。
	Create(requestContext context.Context, jti string, userID string, expiresAt time.Time) error

	// FindActiveOwner は、jti が有効なら、持ち主のユーザーの ID を返す。
	FindActiveOwner(requestContext context.Context, jti string) (string, bool, error)

	// Revoke は、jti を無効にする。無効にしたら true。
	Revoke(requestContext context.Context, jti string) (bool, error)
}

// LoginFailureStore は、ユースケースが使う、ユーザー名ごとのログイン失敗とロックの管理。
type LoginFailureStore interface {
	// IsLocked は、ユーザー名が、いまロック中かを返す。
	IsLocked(requestContext context.Context, userNameLower string) (bool, error)

	// RecordFailure は、失敗を1回記録し、上限に達したらロックする。
	RecordFailure(requestContext context.Context, userNameLower string, maxFailures int, lockDuration time.Duration) error

	// Reset は、失敗の記録を消す。
	Reset(requestContext context.Context, userNameLower string) error
}

// RateLimitStore は、ユースケースが使う、IP ごとのレート制限。
type RateLimitStore interface {
	// Hit は、試行を1回記録し、許されるかと、許されないときの待ち時間を返す。
	Hit(requestContext context.Context, kind string, ip string, limit int, window time.Duration) (bool, time.Duration, error)
}

// LoginEventStore は、ユースケースが使う、ログイン履歴の保存先。
type LoginEventStore interface {
	// Create は、履歴を1件保存する。
	Create(requestContext context.Context, event model.LoginEvent) error

	// ListByUser は、ユーザーの履歴を、新しい順に取得する。
	ListByUser(requestContext context.Context, userID string, limit int) ([]model.LoginEvent, error)
}

// PasswordHasher は、ユースケースが使う、パスワードのハッシュの作成と検証。
type PasswordHasher interface {
	// Hash は、パスワードのハッシュを作る。
	Hash(plainPassword string) (string, error)

	// Verify は、パスワードがハッシュと一致するか、ハッシュを作り直すべきか、を返す。
	Verify(plainPassword string, encodedHash string) (bool, bool, error)
}
