package config

import (
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
		minimal.LoginHistoryRetention != 180*24*time.Hour || minimal.KeysAutoGenerate || minimal.ActiveKeyID != "" || len(minimal.TrustedProxyCIDRs) != 0 {
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
		"AUTH_SIGNUP_MODE": "closed", "AUTH_KEYS_AUTOGENERATE": "yes", "AUTH_LOGIN_MAX_FAILURES": "0", "AUTH_LOGIN_IP_WINDOW": "-1m", "HTTP_PORT": "70000", "LOG_LEVEL": "loud",
	}))
	if invalidError == nil {
		testing.Fatal("エラーのはず")
	}
	for _, expected := range []string{"DATABASE_URL", "AUTH_KEYS_DIR", "AUTH_SIGNUP_MODE", "AUTH_KEYS_AUTOGENERATE", "AUTH_LOGIN_MAX_FAILURES", "AUTH_LOGIN_IP_WINDOW", "HTTP_PORT", "LOG_LEVEL"} {
		if !strings.Contains(invalidError.Error(), expected) {
			testing.Errorf("%s の問題を、報告するはず: %v", expected, invalidError)
		}
	}
}
