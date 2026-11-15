package service

import "time"

const (
	// SignupModeOpen と SignupModeInvite は、サインアップの公開範囲。
	SignupModeOpen   = "open"
	SignupModeInvite = "invite"

	// rateLimitKindLogin と rateLimitKindSignup は、IP ごとのレート制限の種類。
	rateLimitKindLogin  = "login"
	rateLimitKindSignup = "signup"
)

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
}
