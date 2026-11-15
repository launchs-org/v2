package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	sharedconfig "launchs/shared/config"
)

// newTestReader は、指定した map だけを、環境変数として返す EnvironmentReader を作る。
//
// 引数:
//   - environment: 環境変数の名前と値。
//
// 戻り値:
//   - EnvironmentReader。
func newTestReader(environment map[string]string) *sharedconfig.EnvironmentReader {
	// 指定した map だけを環境変数として返す。
	return sharedconfig.NewEnvironmentReader(func(name string) (string, bool) {
		value, isSet := environment[name]
		return value, isSet
	})
}

// TestLoadLogcollectorConfig は、既定値(設計の値)、値の置き換え、不正な値(0 以下、行の上限が速さの上限より大きい)の
// まとめての拒否を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestLoadLogcollectorConfig(testingT *testing.T) {
	// 最小の環境変数(k8s に接続しない)だけだと、設計の既定値になる。
	loaded, loadError := LoadLogcollectorConfig(newTestReader(map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none"}))
	if loadError != nil || loaded.LogLevel != slog.LevelInfo || loaded.Retention != 7*24*time.Hour || loaded.MaxBytesPerPod != 50*1024*1024 || loaded.MaxLineBytes != 16*1024 ||
		loaded.MaxStreams != 500 || loaded.IngestBytesPerSec != 1024*1024 || loaded.ReconcileInterval != 2*time.Second || loaded.FlushInterval != time.Second {
		testingT.Fatalf("既定値のはず: %+v %v", loaded, loadError)
	}
	// 値を指定すると、置き換わる。
	custom, customError := LoadLogcollectorConfig(newTestReader(map[string]string{
		"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none", "LOG_RETENTION": "24h", "LOG_MAX_BYTES_PER_POD": "1000", "LOG_MAX_LINE_BYTES": "100", "LOG_MAX_STREAMS": "5",
		"LOG_INGEST_BYTES_PER_SEC": "200", "LOG_RECONCILE_INTERVAL": "500ms", "LOG_FLUSH_INTERVAL": "250ms", "LOG_LEVEL": "debug",
	}))
	if customError != nil || custom.Retention != 24*time.Hour || custom.MaxBytesPerPod != 1000 || custom.MaxLineBytes != 100 || custom.MaxStreams != 5 ||
		custom.IngestBytesPerSec != 200 || custom.ReconcileInterval != 500*time.Millisecond || custom.FlushInterval != 250*time.Millisecond || custom.LogLevel != slog.LevelDebug {
		testingT.Fatalf("置き換わるはず: %+v %v", custom, customError)
	}
	// 必須が無い、不正な値は、まとめてエラーにする。
	_, invalidError := LoadLogcollectorConfig(newTestReader(map[string]string{
		"KUBERNETES_ACCESS": "none", "LOG_RETENTION": "0s", "LOG_MAX_BYTES_PER_POD": "-1", "LOG_MAX_STREAMS": "0", "LOG_FLUSH_INTERVAL": "-1s", "LOG_LEVEL": "loud",
	}))
	if invalidError == nil {
		testingT.Fatal("エラーのはず")
	}
	for _, expected := range []string{"DATABASE_URL", "LOG_RETENTION", "LOG_MAX_BYTES_PER_POD", "LOG_MAX_STREAMS", "LOG_FLUSH_INTERVAL", "LOG_LEVEL"} {
		if !strings.Contains(invalidError.Error(), expected) {
			testingT.Errorf("%s の問題を、報告するはず: %v", expected, invalidError)
		}
	}
	// 1行の上限が、書き込みの速さの上限より大きいと、拒否する。
	_, tooLongError := LoadLogcollectorConfig(newTestReader(map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none", "LOG_MAX_LINE_BYTES": "2000", "LOG_INGEST_BYTES_PER_SEC": "1000"}))
	if tooLongError == nil || !strings.Contains(tooLongError.Error(), "LOG_MAX_LINE_BYTES") {
		testingT.Fatalf("行の上限が大きすぎるときは、エラーのはず: %v", tooLongError)
	}
}
