package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	sharedconfig "launchs/shared/config"
)

// newTestReader は、固定の環境変数を持つ EnvironmentReader を作る(テスト用)。
//
// 引数:
//   - environment: 環境変数の名前と値の組。
//
// 戻り値:
//   - 指定した環境変数だけを読める EnvironmentReader。
func newTestReader(environment map[string]string) *sharedconfig.EnvironmentReader {
	// 指定した map だけを環境変数として返す。
	return sharedconfig.NewEnvironmentReader(func(name string) (string, bool) {
		value, isSet := environment[name]
		return value, isSet
	})
}

// TestLoadAuthConfig は、必須の環境変数、既定値、値の置き換え、不正な値の拒否(まとめて報告)を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestLoadAuthConfig(testing *testing.T) {
	// 最小の環境変数だけだと、既定値になる。
	minimal, minimalError := LoadAuthConfig(newTestReader(map[string]string{"DATABASE_URL": "postgres://db", "AUTH_KEYS_DIR": "/keys"}))
	if minimalError != nil || minimal.HTTPPort != 8080 || minimal.SignupMode != "open" || minimal.LoginMaxFailures != 5 || minimal.LoginLockDuration != 15*time.Minute ||
		minimal.LoginIPLimit != 20 || minimal.LoginIPWindow != time.Minute || minimal.SignupIPLimit != 10 || minimal.SignupIPWindow != time.Hour ||
		minimal.LoginHistoryRetention != 180*24*time.Hour || minimal.KeysAutoGenerate || minimal.ActiveKeyID != "" || len(minimal.TrustedProxyCIDRs) != 0 || minimal.HashMaxConcurrency != 4 {
		testing.Fatalf("既定値のはず: %+v %v", minimal, minimalError)
	}
	// 値を指定すると、置き換わる。
	custom, customError := LoadAuthConfig(newTestReader(map[string]string{
		"DATABASE_URL": "postgres://db", "AUTH_KEYS_DIR": "/keys", "AUTH_ACTIVE_KID": "key-1", "AUTH_KEYS_AUTOGENERATE": "true", "AUTH_SIGNUP_MODE": "invite",
		"AUTH_TRUSTED_PROXY_CIDRS": " 10.0.0.0/8, ,192.168.0.0/16 ", "AUTH_LOGIN_MAX_FAILURES": "3", "AUTH_LOGIN_LOCK_DURATION": "1m", "AUTH_LOGIN_HISTORY_RETENTION": "720h", "HTTP_PORT": "9000",
	}))
	if customError != nil || !custom.KeysAutoGenerate || custom.SignupMode != "invite" || custom.ActiveKeyID != "key-1" || custom.LoginMaxFailures != 3 ||
		custom.LoginLockDuration != time.Minute || custom.LoginHistoryRetention != 720*time.Hour || custom.HTTPPort != 9000 || !reflect.DeepEqual(custom.TrustedProxyCIDRs, []string{"10.0.0.0/8", "192.168.0.0/16"}) {
		testing.Fatalf("置き換わるはず: %+v %v", custom, customError)
	}
	// 必須が無い、不正な値は、まとめてエラーにする。
	_, invalidError := LoadAuthConfig(newTestReader(map[string]string{
		"AUTH_SIGNUP_MODE": "closed", "AUTH_KEYS_AUTOGENERATE": "yes", "AUTH_LOGIN_MAX_FAILURES": "0", "AUTH_LOGIN_IP_WINDOW": "-1m", "AUTH_HASH_MAX_CONCURRENCY": "0", "HTTP_PORT": "70000", "LOG_LEVEL": "loud",
	}))
	if invalidError == nil {
		testing.Fatal("エラーのはず")
	}
	for _, expected := range []string{"DATABASE_URL", "AUTH_KEYS_DIR", "AUTH_SIGNUP_MODE", "AUTH_KEYS_AUTOGENERATE", "AUTH_LOGIN_MAX_FAILURES", "AUTH_LOGIN_IP_WINDOW", "AUTH_HASH_MAX_CONCURRENCY", "HTTP_PORT", "LOG_LEVEL"} {
		if !strings.Contains(invalidError.Error(), expected) {
			testing.Errorf("%s の問題を、報告するはず: %v", expected, invalidError)
		}
	}
}

// TestLoadAuthConfigBootstrapAdmin は、最初の管理者の設定が、指定なし・名前とパスワード・パスワードのファイル(末尾の改行を除く)で読めること、
// 名前だけ、パスワードだけ、パスワードの二重の指定、読めないファイルを、エラーにすることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestLoadAuthConfigBootstrapAdmin(testing *testing.T) {
	// 必須の環境変数と、パスワードのファイルを用意する。
	base := map[string]string{"DATABASE_URL": "postgres://db", "AUTH_KEYS_DIR": "/keys"}
	with := func(extra map[string]string) map[string]string {
		merged := map[string]string{}
		for name, value := range base {
			merged[name] = value
		}
		for name, value := range extra {
			merged[name] = value
		}
		return merged
	}
	passwordFile := filepath.Join(testing.TempDir(), "admin-password")
	if writeError := os.WriteFile(passwordFile, []byte("from file password\n"), 0o600); writeError != nil {
		testing.Fatal(writeError)
	}
	// 指定なしなら、作らない設定。
	if none, noneError := LoadAuthConfig(newTestReader(base)); noneError != nil || none.BootstrapAdminName != "" || none.BootstrapAdminPassword != "" {
		testing.Fatalf("指定なしは、空のはず: %+v %v", none, noneError)
	}
	// 環境変数で、名前とパスワード。
	if fromEnv, envError := LoadAuthConfig(newTestReader(with(map[string]string{"AUTH_BOOTSTRAP_ADMIN_NAME": "root", "AUTH_BOOTSTRAP_ADMIN_PASSWORD": "env password"}))); envError != nil || fromEnv.BootstrapAdminName != "root" || fromEnv.BootstrapAdminPassword != "env password" {
		testing.Fatalf("環境変数から読めるはず: %+v %v", fromEnv, envError)
	}
	// ファイルから(末尾の改行は除く)。
	if fromFile, fileError := LoadAuthConfig(newTestReader(with(map[string]string{"AUTH_BOOTSTRAP_ADMIN_NAME": "root", "AUTH_BOOTSTRAP_ADMIN_PASSWORD_FILE": passwordFile}))); fileError != nil || fromFile.BootstrapAdminPassword != "from file password" {
		testing.Fatalf("ファイルから読めるはず: %+v %v", fromFile, fileError)
	}
	// 不正な組み合わせは、エラー。
	invalidCases := map[string]struct {
		extra map[string]string
		piece string
	}{
		"名前だけ":        {map[string]string{"AUTH_BOOTSTRAP_ADMIN_NAME": "root"}, "両方指定するか"},
		"パスワードだけ":     {map[string]string{"AUTH_BOOTSTRAP_ADMIN_PASSWORD": "pw"}, "両方指定するか"},
		"ファイルだけ":      {map[string]string{"AUTH_BOOTSTRAP_ADMIN_PASSWORD_FILE": passwordFile}, "両方指定するか"},
		"パスワードの二重の指定": {map[string]string{"AUTH_BOOTSTRAP_ADMIN_NAME": "root", "AUTH_BOOTSTRAP_ADMIN_PASSWORD": "pw", "AUTH_BOOTSTRAP_ADMIN_PASSWORD_FILE": passwordFile}, "同時に指定できない"},
		"読めないファイル":    {map[string]string{"AUTH_BOOTSTRAP_ADMIN_NAME": "root", "AUTH_BOOTSTRAP_ADMIN_PASSWORD_FILE": "/nonexistent/file"}, "ファイルを読めない"},
	}
	for name, invalidCase := range invalidCases {
		if _, invalidError := LoadAuthConfig(newTestReader(with(invalidCase.extra))); invalidError == nil || !strings.Contains(invalidError.Error(), invalidCase.piece) {
			testing.Errorf("%s: %q を含むエラーのはず: %v", name, invalidCase.piece, invalidError)
		}
	}
}

// TestLoadAuthConfigTurnstile は、Turnstile の設定が、指定なし(無効)・サイトキーとシークレットキーの両方(有効)・ホスト名と URL の指定で読めること、
// 片方だけ、不正な URL を、エラーにすることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestLoadAuthConfigTurnstile(testing *testing.T) {
	// 必須の環境変数に、指定を足す。
	with := func(extra map[string]string) map[string]string {
		merged := map[string]string{"DATABASE_URL": "postgres://db", "AUTH_KEYS_DIR": "/keys"}
		for name, value := range extra {
			merged[name] = value
		}
		return merged
	}
	// 指定なしは、無効。
	if none, noneError := LoadAuthConfig(newTestReader(with(nil))); noneError != nil || none.TurnstileSiteKey != "" || none.TurnstileSecretKey != "" || none.TurnstileVerifyURL != "" || len(none.TurnstileHostnames) != 0 {
		testing.Fatalf("指定なしは、無効のはず: %+v %v", none, noneError)
	}
	// 両方を指定すると、有効。ホスト名は、カンマ区切りで、空白と空の要素を除く。
	enabled, enabledError := LoadAuthConfig(newTestReader(with(map[string]string{
		"AUTH_TURNSTILE_SITE_KEY": "site-1", "AUTH_TURNSTILE_SECRET_KEY": "secret-1", "AUTH_TURNSTILE_VERIFY_URL": "http://localhost:9999/verify", "AUTH_TURNSTILE_HOSTNAMES": " app.example, ,localhost ",
	})))
	if enabledError != nil || enabled.TurnstileSiteKey != "site-1" || enabled.TurnstileSecretKey != "secret-1" || enabled.TurnstileVerifyURL != "http://localhost:9999/verify" || !reflect.DeepEqual(enabled.TurnstileHostnames, []string{"app.example", "localhost"}) {
		testing.Fatalf("読めるはず: %+v %v", enabled, enabledError)
	}
	// 不正な指定は、エラー。
	invalidCases := map[string]struct {
		extra map[string]string
		piece string
	}{
		"サイトキーだけ":        {map[string]string{"AUTH_TURNSTILE_SITE_KEY": "site-1"}, "両方指定するか"},
		"シークレットだけ":       {map[string]string{"AUTH_TURNSTILE_SECRET_KEY": "secret-1"}, "両方指定するか"},
		"URL が http でない": {map[string]string{"AUTH_TURNSTILE_SITE_KEY": "s", "AUTH_TURNSTILE_SECRET_KEY": "k", "AUTH_TURNSTILE_VERIFY_URL": "ftp://x"}, "AUTH_TURNSTILE_VERIFY_URL"},
	}
	for name, invalidCase := range invalidCases {
		if _, invalidError := LoadAuthConfig(newTestReader(with(invalidCase.extra))); invalidError == nil || !strings.Contains(invalidError.Error(), invalidCase.piece) {
			testing.Errorf("%s: %q を含むエラーのはず: %v", name, invalidCase.piece, invalidError)
		}
	}
}
