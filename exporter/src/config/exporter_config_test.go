package config

import (
	"testing"
	"time"

	sharedconfig "launchs/shared/config"
)

// newReader は、与えた値だけを持つ環境変数の読み手を作る。
//
// 引数:
//   - values: 環境変数の名前と値。
//
// 戻り値:
//   - 読み手。
func newReader(values map[string]string) *sharedconfig.EnvironmentReader {
	// 表から調べる。
	return sharedconfig.NewEnvironmentReader(func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	})
}

// TestLoadExporterConfig は、既定値、上書き、不正な値の拒否を確かめる。
func TestLoadExporterConfig(testingT *testing.T) {
	// 必須の DATABASE_URL だけなら、既定値になる。
	loaded, loadError := LoadExporterConfig(newReader(map[string]string{"DATABASE_URL": "postgres://x"}))
	if loadError != nil || loaded.CheckInterval != 10*time.Second || loaded.DriveFlushRows != 6000 || loaded.CredentialKey != "" || loaded.SafeOptions.AllowHTTP || loaded.Google.HasClient() {
		testingT.Fatalf("既定値のはず: %+v %v", loaded, loadError)
	}
	// 上書きできる。
	overridden, overrideError := LoadExporterConfig(newReader(map[string]string{
		"DATABASE_URL": "postgres://x", "EXPORT_CHECK_INTERVAL": "30s", "EXPORT_DRIVE_FLUSH_ROWS": "100", "EXPORT_ALLOW_HTTP": "true",
		"GOOGLE_OAUTH_CLIENT_ID": "id", "GOOGLE_OAUTH_CLIENT_SECRET": "secret", "EXPORT_CREDENTIAL_KEY": "key",
	}))
	if overrideError != nil || overridden.CheckInterval != 30*time.Second || overridden.DriveFlushRows != 100 || !overridden.SafeOptions.AllowHTTP || !overridden.Google.HasClient() || overridden.CredentialKey != "key" {
		testingT.Fatalf("上書きできるはず: %+v %v", overridden, overrideError)
	}
	// 不正な値は、まとめて報告する。
	if _, badError := LoadExporterConfig(newReader(map[string]string{"DATABASE_URL": "x", "EXPORT_CHECK_INTERVAL": "0s", "EXPORT_ALLOW_HTTP": "yes", "EXPORT_DRIVE_FLUSH_ROWS": "-1"})); badError == nil {
		testingT.Errorf("不正な値は拒否するはず")
	}
	// DATABASE_URL が無ければ、エラー。
	if _, missingError := LoadExporterConfig(newReader(map[string]string{})); missingError == nil {
		testingT.Errorf("DATABASE_URL が無ければエラーのはず")
	}
}
