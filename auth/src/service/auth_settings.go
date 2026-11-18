package service

import (
	"context"
	"time"
)

const (
	// SignupModeOpen と SignupModeInvite は、サインアップの公開範囲。
	SignupModeOpen   = "open"
	SignupModeInvite = "invite"

	// rateLimitKindLogin と rateLimitKindSignup は、IP ごとのレート制限の種類。
	rateLimitKindLogin  = "login"
	rateLimitKindSignup = "signup"
)

// busyRetryAfter は、パスワードの計算が混み合っているときに、利用者に待たせる時間。
const busyRetryAfter = time.Second

// CaptchaVerifier は、人間であることの確認(Cloudflare Turnstile)のトークンを検証するもの。
type CaptchaVerifier interface {
	// Verify は、トークンを検証する。通れば nil。無効なら invalid_request、確認のサービスに届かなければ service_unavailable。
	Verify(requestContext context.Context, token string, remoteIP string) error
}

// Settings は、認証のユースケースの設定。回数と時間は、仮の値で、環境変数で変えられる。
type Settings struct {
	// SignupMode は、サインアップの公開範囲(open か invite)。
	SignupMode string

	// LoginMaxFailures と LoginLockDuration は、ユーザー名ごとの、ロックする失敗の回数と、ロックの時間。
	LoginMaxFailures  int
	LoginLockDuration time.Duration

	// LoginIPLimit と LoginIPWindow は、IP ごとのログイン試行の上限と、時間枠。
	LoginIPLimit  int
	LoginIPWindow time.Duration

	// SignupIPLimit と SignupIPWindow は、IP ごとのサインアップの上限と、時間枠。
	SignupIPLimit  int
	SignupIPWindow time.Duration

	// Captcha は、人間であることの確認。nil なら、無効(トークンを要求しない)。
	// 有効なときは、ログインとサインアップで、IP のレート制限の後に確かめる。失敗しても、ユーザー名のロックや、ログイン履歴には、数えない。
	Captcha CaptchaVerifier
}
