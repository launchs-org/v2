// Package config は、認証コンテナの設定(環境変数)を読み込む。
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sharedconfig "launchs/shared/config"
	"launchs/shared/logger"
)

const (
	// 既定値(回数と時間は、仮の値。環境変数で変えられる)。
	defaultHTTPPort              = 8080
	defaultLogLevelText          = "info"
	defaultSignupMode            = "open"
	defaultLoginMaxFailures      = 5
	defaultLoginLockDuration     = 15 * time.Minute
	defaultLoginIPLimit          = 20
	defaultLoginIPWindow         = time.Minute
	defaultSignupIPLimit         = 10
	defaultSignupIPWindow        = time.Hour
	defaultLoginHistoryRetention = 180 * 24 * time.Hour
	defaultHashMaxConcurrency    = 4
)

// AuthConfig は、認証コンテナの設定。
type AuthConfig struct {
	// HTTPPort は、待ち受けるポート番号(環境変数 HTTP_PORT)。
	HTTPPort int

	// LogLevel は、出力するログの最低のレベル(環境変数 LOG_LEVEL)。
	LogLevel slog.Level

	// DatabaseURL は、auth データベースの接続先(環境変数 DATABASE_URL)。パスワードを含むので、ログに出さない。
	DatabaseURL string

	// KeysDirectory は、署名鍵(<kid>.pem)を置くディレクトリ(環境変数 AUTH_KEYS_DIR)。
	KeysDirectory string

	// ActiveKeyID は、署名に使う鍵の kid(環境変数 AUTH_ACTIVE_KID)。空なら、kid の辞書順で最後のもの。
	ActiveKeyID string

	// KeysAutoGenerate は、鍵が1つも無いとき、作るか(環境変数 AUTH_KEYS_AUTOGENERATE=true。開発用)。
	KeysAutoGenerate bool

	// SignupMode は、サインアップの公開範囲(環境変数 AUTH_SIGNUP_MODE。open か invite)。
	SignupMode string

	// TrustedProxyCIDRs は、X-Forwarded-For を信頼するプロキシの CIDR(環境変数 AUTH_TRUSTED_PROXY_CIDRS。カンマ区切り)。
	TrustedProxyCIDRs []string

	// LoginMaxFailures は、ロックするまでの、ユーザー名ごとの連続した失敗の回数(環境変数 AUTH_LOGIN_MAX_FAILURES)。
	LoginMaxFailures int

	// LoginLockDuration は、ロックの時間(環境変数 AUTH_LOGIN_LOCK_DURATION)。
	LoginLockDuration time.Duration

	// LoginIPLimit と LoginIPWindow は、IP ごとのログイン試行の上限と時間枠(環境変数 AUTH_LOGIN_IP_LIMIT、AUTH_LOGIN_IP_WINDOW)。
	LoginIPLimit  int
	LoginIPWindow time.Duration

	// LoginHistoryRetention は、ログイン履歴を残す期間(環境変数 AUTH_LOGIN_HISTORY_RETENTION)。
	LoginHistoryRetention time.Duration

	// SignupIPLimit と SignupIPWindow は、IP ごとのサインアップの上限と時間枠(環境変数 AUTH_SIGNUP_IP_LIMIT、AUTH_SIGNUP_IP_WINDOW)。
	SignupIPLimit  int
	SignupIPWindow time.Duration

	// HashMaxConcurrency は、パスワードのハッシュを、同時に計算できる数(環境変数 AUTH_HASH_MAX_CONCURRENCY)。メモリを守るための上限。
	HashMaxConcurrency int
}

// LoadAuthConfig は、環境変数から、認証コンテナの設定を読み込む。
//
// 設定に問題があっても止まらず、見つかった全ての問題をまとめて報告する。
//
// 引数:
//   - reader: 環境変数を読む EnvironmentReader。
//
// 戻り値:
//   - 読み込んだ設定。問題があったときは、ゼロ値。
//   - エラー。問題があったとき、全ての問題の説明を含む。
func LoadAuthConfig(reader *sharedconfig.EnvironmentReader) (AuthConfig, error) {
	// 環境変数を読む。
	databaseURL := reader.RequiredString("DATABASE_URL")
	keysDirectory := reader.RequiredString("AUTH_KEYS_DIR")
	activeKeyID := reader.OptionalString("AUTH_ACTIVE_KID", "")
	autoGenerateText := reader.OptionalString("AUTH_KEYS_AUTOGENERATE", "false")
	signupMode := reader.OptionalString("AUTH_SIGNUP_MODE", defaultSignupMode)
	trustedProxyText := reader.OptionalString("AUTH_TRUSTED_PROXY_CIDRS", "")
	httpPort := reader.OptionalInt("HTTP_PORT", defaultHTTPPort)
	logLevelText := reader.OptionalString("LOG_LEVEL", defaultLogLevelText)
	loginMaxFailures := reader.OptionalInt("AUTH_LOGIN_MAX_FAILURES", defaultLoginMaxFailures)
	loginLockDuration := reader.OptionalDuration("AUTH_LOGIN_LOCK_DURATION", defaultLoginLockDuration)
	loginIPLimit := reader.OptionalInt("AUTH_LOGIN_IP_LIMIT", defaultLoginIPLimit)
	loginIPWindow := reader.OptionalDuration("AUTH_LOGIN_IP_WINDOW", defaultLoginIPWindow)
	signupIPLimit := reader.OptionalInt("AUTH_SIGNUP_IP_LIMIT", defaultSignupIPLimit)
	signupIPWindow := reader.OptionalDuration("AUTH_SIGNUP_IP_WINDOW", defaultSignupIPWindow)
	hashMaxConcurrency := reader.OptionalInt("AUTH_HASH_MAX_CONCURRENCY", defaultHashMaxConcurrency)
	loginHistoryRetention := reader.OptionalDuration("AUTH_LOGIN_HISTORY_RETENTION", defaultLoginHistoryRetention)
	loadError := reader.Err()

	// ポート番号の範囲を確かめる。
	if httpPort < 1 || httpPort > 65535 {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 HTTP_PORT は 1〜65535 でなければならない: %d", httpPort))
	}
	// ログレベルを変換する。
	logLevel, levelError := logger.ParseLevel(logLevelText)
	if levelError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 LOG_LEVEL: %w", levelError))
	}
	// 鍵の自動作成の指定は、true か false。
	if autoGenerateText != "true" && autoGenerateText != "false" {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 AUTH_KEYS_AUTOGENERATE は true か false でなければならない: %q", autoGenerateText))
	}
	// サインアップの公開範囲は、open か invite。
	if signupMode != "open" && signupMode != "invite" {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 AUTH_SIGNUP_MODE は open か invite でなければならない: %q", signupMode))
	}
	// 回数と時間は、正の値でなければならない。
	for name, valid := range map[string]bool{
		"AUTH_LOGIN_MAX_FAILURES": loginMaxFailures > 0, "AUTH_LOGIN_LOCK_DURATION": loginLockDuration > 0,
		"AUTH_LOGIN_IP_LIMIT": loginIPLimit > 0, "AUTH_LOGIN_IP_WINDOW": loginIPWindow > 0,
		"AUTH_LOGIN_HISTORY_RETENTION": loginHistoryRetention > 0, "AUTH_SIGNUP_IP_LIMIT": signupIPLimit > 0, "AUTH_SIGNUP_IP_WINDOW": signupIPWindow > 0, "AUTH_HASH_MAX_CONCURRENCY": hashMaxConcurrency > 0,
	} {
		if !valid {
			loadError = errors.Join(loadError, fmt.Errorf("環境変数 %s は、正の値でなければならない", name))
		}
	}
	// 問題があれば、ゼロ値とエラーを返す。
	if loadError != nil {
		return AuthConfig{}, loadError
	}
	return AuthConfig{
		HTTPPort: httpPort, LogLevel: logLevel, DatabaseURL: databaseURL, KeysDirectory: keysDirectory, ActiveKeyID: activeKeyID,
		KeysAutoGenerate: autoGenerateText == "true", SignupMode: signupMode, TrustedProxyCIDRs: splitCommaSeparated(trustedProxyText),
		LoginMaxFailures: loginMaxFailures, LoginLockDuration: loginLockDuration, LoginIPLimit: loginIPLimit, LoginIPWindow: loginIPWindow,
		SignupIPLimit: signupIPLimit, SignupIPWindow: signupIPWindow, LoginHistoryRetention: loginHistoryRetention,
		HashMaxConcurrency: hashMaxConcurrency,
	}, nil
}

// splitCommaSeparated は、カンマ区切りの文字列を、前後の空白を除いて分け、空の要素を捨てる。
//
// 引数:
//   - text: カンマ区切りの文字列。
//
// 戻り値:
//   - 要素の一覧。
func splitCommaSeparated(text string) []string {
	// カンマで分けて、前後の空白を除き、空でないものだけを集める。
	items := []string{}
	for _, item := range strings.Split(text, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}
