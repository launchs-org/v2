package config

import (
	"log/slog"
	"testing"
	"time"

	sharedconfig "launchs/shared/config"
	"launchs/shared/k8s"
)

// TestLoadWatcherConfig は、環境変数から、必須の値・既定値・不正な値の扱いを確認する。
func TestLoadWatcherConfig(testingT *testing.T) {
	// 入力ごとの期待。
	testCases := []struct {
		name          string
		environment   map[string]string
		expectedError bool
		expectedLevel slog.Level
		expectedMode  k8s.Access
	}{
		{name: "必須だけで、既定値になる", environment: map[string]string{"DATABASE_URL": "postgres://x", "KUBERNETES_ACCESS": "none"}, expectedLevel: slog.LevelInfo, expectedMode: k8s.AccessNone},
		{name: "ログレベルと kubeconfig を指定できる", environment: map[string]string{"DATABASE_URL": "postgres://x", "KUBERNETES_ACCESS": "kubeconfig", "KUBECONFIG": "/kube/config", "LOG_LEVEL": "debug"}, expectedLevel: slog.LevelDebug, expectedMode: k8s.AccessKubeconfig},
		{name: "DATABASE_URL が無ければエラー", environment: map[string]string{"KUBERNETES_ACCESS": "none"}, expectedError: true},
		{name: "KUBERNETES_ACCESS が無ければエラー", environment: map[string]string{"DATABASE_URL": "postgres://x"}, expectedError: true},
		{name: "kubeconfig なのにパスが無ければエラー", environment: map[string]string{"DATABASE_URL": "postgres://x", "KUBERNETES_ACCESS": "kubeconfig"}, expectedError: true},
		{name: "未知のログレベルはエラー", environment: map[string]string{"DATABASE_URL": "postgres://x", "KUBERNETES_ACCESS": "none", "LOG_LEVEL": "loud"}, expectedError: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			reader := sharedconfig.NewEnvironmentReader(func(name string) (string, bool) {
				value, exists := testCase.environment[name]
				return value, exists
			})
			loaded, loadError := LoadWatcherConfig(reader)
			if (loadError != nil) != testCase.expectedError {
				subTest.Fatalf("エラーの有無が違う: %v", loadError)
			}
			if !testCase.expectedError && (loaded.LogLevel != testCase.expectedLevel || loaded.Kubernetes.Access != testCase.expectedMode) {
				subTest.Errorf("設定が違う: %+v", loaded)
			}
		})
	}
}

// TestLoadWatcherConfigCrashLoopAndTemporal は、Temporal の接続先と、クラッシュで止める上限の、
// 既定値・指定・不正な値の扱いを確認する。
func TestLoadWatcherConfigCrashLoopAndTemporal(testingT *testing.T) {
	// 必須の環境変数に、確かめたいものを足して読む道具。
	load := func(extra map[string]string) (WatcherConfig, error) {
		environment := map[string]string{"DATABASE_URL": "postgres://x", "KUBERNETES_ACCESS": "none"}
		for name, value := range extra {
			environment[name] = value
		}
		return LoadWatcherConfig(sharedconfig.NewEnvironmentReader(func(name string) (string, bool) {
			value, exists := environment[name]
			return value, exists
		}))
	}

	testingT.Run("既定値は、30分・20回・10分で、Temporal は temporal:7233 の default", func(subTest *testing.T) {
		loaded, loadError := load(nil)
		if loadError != nil || loaded.CrashLoop.UnhealthyFor != 30*time.Minute || loaded.CrashLoop.MaxRestarts != 20 ||
			loaded.CrashLoop.RestartMinUnhealthy != 10*time.Minute || loaded.TemporalAddress != "temporal:7233" || loaded.TemporalNamespace != "default" {
			subTest.Errorf("既定値が違う: %v %+v", loadError, loaded)
		}
	})
	testingT.Run("環境変数で、上限と Temporal の接続先を変えられる", func(subTest *testing.T) {
		loaded, loadError := load(map[string]string{
			"CRASH_LOOP_UNHEALTHY_FOR": "2m", "CRASH_LOOP_MAX_RESTARTS": "3", "CRASH_LOOP_RESTART_MIN_UNHEALTHY": "30s",
			"TEMPORAL_ADDRESS": "localhost:7233", "TEMPORAL_NAMESPACE": "prod",
		})
		if loadError != nil || loaded.CrashLoop.UnhealthyFor != 2*time.Minute || loaded.CrashLoop.MaxRestarts != 3 ||
			loaded.CrashLoop.RestartMinUnhealthy != 30*time.Second || loaded.TemporalAddress != "localhost:7233" || loaded.TemporalNamespace != "prod" {
			subTest.Errorf("指定した値になるはず: %v %+v", loadError, loaded)
		}
	})
	// 0 や負の値は、全ての deploy を止めてしまうので、拒否する。
	for _, invalid := range []map[string]string{
		{"CRASH_LOOP_UNHEALTHY_FOR": "0s"}, {"CRASH_LOOP_UNHEALTHY_FOR": "-5m"}, {"CRASH_LOOP_MAX_RESTARTS": "0"},
		{"CRASH_LOOP_MAX_RESTARTS": "-1"}, {"CRASH_LOOP_RESTART_MIN_UNHEALTHY": "0s"}, {"CRASH_LOOP_UNHEALTHY_FOR": "abc"},
	} {
		testingT.Run("不正な値は拒否する", func(subTest *testing.T) {
			if _, loadError := load(invalid); loadError == nil {
				subTest.Errorf("拒否されるはず: %v", invalid)
			}
		})
	}
}
