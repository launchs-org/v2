package config

import (
	"strings"
	"testing"
	"time"

	sharedconfig "launchs/shared/config"
)

// newTestReader は、決めた環境変数だけを持つ EnvironmentReader を作る。
func newTestReader(values map[string]string) *sharedconfig.EnvironmentReader {
	// 表から引く関数を渡す。
	return sharedconfig.NewEnvironmentReader(func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	})
}

// TestLoadAccessStatsConfig は、既定値と、不正な値の拒否を確認する。
func TestLoadAccessStatsConfig(testingT *testing.T) {
	loaded, loadError := LoadAccessStatsConfig(newTestReader(map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none"}))
	if loadError != nil {
		testingT.Fatal(loadError)
	}
	if loaded.TraefikNamespace != "traefik" || loaded.TraefikSelector != "app.kubernetes.io/name=traefik" || !loaded.KeepClients || loaded.MinuteRetention != 24*time.Hour || loaded.HourRetention != 720*time.Hour {
		testingT.Errorf("既定値が違う: %+v", loaded)
	}
	off, _ := LoadAccessStatsConfig(newTestReader(map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none", "ACCESS_STATS_KEEP_CLIENTS": "false"}))
	if off.KeepClients {
		testingT.Error("クライアントを保存しない設定になるはず")
	}
	for name, values := range map[string]map[string]string{
		"DATABASE_URL":                  {"KUBERNETES_ACCESS": "none"},
		"ACCESS_STATS_KEEP_CLIENTS":     {"DATABASE_URL": "x", "KUBERNETES_ACCESS": "none", "ACCESS_STATS_KEEP_CLIENTS": "maybe"},
		"ACCESS_STATS_MINUTE_RETENTION": {"DATABASE_URL": "x", "KUBERNETES_ACCESS": "none", "ACCESS_STATS_MINUTE_RETENTION": "0s"},
	} {
		if _, failed := LoadAccessStatsConfig(newTestReader(values)); failed == nil || !strings.Contains(failed.Error(), name) {
			testingT.Errorf("%s: 拒否するはず: %v", name, failed)
		}
	}
}
