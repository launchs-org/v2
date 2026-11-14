package config

import (
	"log/slog"
	"testing"

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
