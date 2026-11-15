package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"launchs/auth/src/model"
	"launchs/shared/apperror"
)

// checkRateLimit は、IP ごとのレート制限を確かめる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - kind: 種類("login" か "signup")。
//   - clientIP: クライアントの IP。
//   - limit: 時間枠の中で許す回数。
//   - window: 時間枠。
//
// 戻り値:
//   - 上限を超えたときは RateLimitedError。データベースのエラーは、そのまま。
func (authService *AuthService) checkRateLimit(requestContext context.Context, kind string, clientIP string, limit int, window time.Duration) error {
	// 記録して、判定する。
	allowed, retryAfter, hitError := authService.rateLimitStore.Hit(requestContext, kind, clientIP, limit, window)
	if hitError != nil {
		return hitError
	}
	if !allowed {
		return &RateLimitedError{RetryAfter: retryAfter}
	}
	return nil
}

// maxUserAgentLength は、履歴に保存する User-Agent の長さの上限(バイト)。
const maxUserAgentLength = 256

// record は、ログイン履歴を1件残す。
//
// 履歴の保存に失敗しても、ログインやサインアップは、失敗にしない(警告だけ残す)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザーの ID(存在しないユーザー名なら空)。
//   - userNameLower: 試行で指定された名前(小文字)。
//   - kind: 種類(signup か login)。
//   - failureReason: 失敗の理由(成功なら空)。
//   - clientIP: クライアントの IP。
//   - userAgent: クライアントの User-Agent。
func (authService *AuthService) record(requestContext context.Context, userID string, userNameLower string, kind string, failureReason string, clientIP string, userAgent string) {
	// 履歴の内容を組み立てる(User-Agent は、長いときは切り詰める)。
	if len(userAgent) > maxUserAgentLength {
		userAgent = userAgent[:maxUserAgentLength]
	}
	event := model.LoginEvent{UserNameLower: userNameLower, Kind: kind, Result: model.LoginEventSuccess, IP: clientIP, UserAgent: userAgent}
	if userID != "" {
		event.UserID = &userID
	}
	if failureReason != "" {
		event.Result, event.Reason = model.LoginEventFailure, &failureReason
	}
	// 保存する(失敗は、警告だけ)。
	if createError := authService.loginEventStore.Create(requestContext, event); createError != nil {
		authService.appLogger.Warn("ログイン履歴を保存できなかった", "kind", kind, "error", createError)
	}
}

// issueRefreshToken は、refresh token を発行して、有効なものとして保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - user: 持ち主のユーザー。
//
// 戻り値:
//   - refresh token の文字列。
func (authService *AuthService) issueRefreshToken(requestContext context.Context, user model.User) (string, error) {
	// 発行して、jti を DB に保存する。
	issued, issueError := authService.tokenService.IssueRefresh(user.ID, user.UserName)
	if issueError != nil {
		return "", apperror.Wrap(apperror.KindInternal, "トークンを発行できませんでした", issueError)
	}
	if saveError := authService.refreshTokenStore.Create(requestContext, issued.ID, user.ID, issued.ExpiresAt); saveError != nil {
		return "", saveError
	}
	return issued.Token, nil
}

// HashInvitationCode は、招待コードを、保存用のハッシュ(SHA-256 の16進数)にする。
//
// 引数:
//   - invitationCode: 招待コード。
//
// 戻り値:
//   - ハッシュ。
func HashInvitationCode(invitationCode string) string {
	// 前後の空白を除いて、SHA-256 にする。
	digest := sha256.Sum256([]byte(strings.TrimSpace(invitationCode)))
	return hex.EncodeToString(digest[:])
}
