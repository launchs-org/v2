package config

import (
	"log/slog"
	"strings"
	"testing"

	sharedconfig "launchs/shared/config"
	"launchs/shared/k8s"
)

// newTestReader は、固定の環境変数を持つ EnvironmentReader を作る(テスト用)。
//
// 引数:
//   - environment: 環境変数の名前と値の組。
//
// 戻り値:
//   - 指定した環境変数だけを読める EnvironmentReader。
func newTestReader(environment map[string]string) *sharedconfig.EnvironmentReader {
	// 環境変数の代わりに、渡された map から読む関数を使う。
	return sharedconfig.NewEnvironmentReader(func(name string) (string, bool) {
		value, isSet := environment[name]
		return value, isSet
	})
}

// TestLoadWorkerConfig は、ワーカーの設定を読めること、省略時は既定値になること、問題をまとめて報告することを確認する。
func TestLoadWorkerConfig(testingT *testing.T) {
	// 既定値、全指定、必須の欠落、未知のログレベル、複数の問題の場合。
	testCases := []struct {
		name                string
		environment         map[string]string
		expectedConfig      WorkerConfig
		expectedErrorPieces []string
	}{
		{
			name:           "DATABASE_URL だけなら、他は既定値になる",
			environment:    map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none"},
			expectedConfig: WorkerConfig{DatabaseURL: "postgres://db", TemporalAddress: "temporal:7233", TemporalNamespace: "default", LogLevel: slog.LevelInfo, Kubernetes: k8s.Settings{Access: k8s.AccessNone}},
		},
		{
			name:           "全て設定すると、その値になる",
			environment:    map[string]string{"DATABASE_URL": "postgres://db", "TEMPORAL_ADDRESS": "host:1", "TEMPORAL_NAMESPACE": "ns", "LOG_LEVEL": "debug", "KUBERNETES_ACCESS": "kubeconfig", "KUBECONFIG": "/kube/config", "RUNTIME_CLASS": "gvisor"},
			expectedConfig: WorkerConfig{DatabaseURL: "postgres://db", TemporalAddress: "host:1", TemporalNamespace: "ns", LogLevel: slog.LevelDebug, RuntimeClass: "gvisor", Kubernetes: k8s.Settings{Access: k8s.AccessKubeconfig, KubeconfigPath: "/kube/config"}},
		},
		{
			name:           "in-cluster も指定できる",
			environment:    map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "in-cluster"},
			expectedConfig: WorkerConfig{DatabaseURL: "postgres://db", TemporalAddress: "temporal:7233", TemporalNamespace: "default", LogLevel: slog.LevelInfo, Kubernetes: k8s.Settings{Access: k8s.AccessInCluster}},
		},
		{
			name:                "DATABASE_URL が無いとエラー",
			environment:         map[string]string{"KUBERNETES_ACCESS": "none"},
			expectedErrorPieces: []string{"DATABASE_URL"},
		},
		{
			name:                "KUBERNETES_ACCESS が無いとエラー(何も消さないまま動くのを防ぐ)",
			environment:         map[string]string{"DATABASE_URL": "postgres://db"},
			expectedErrorPieces: []string{"KUBERNETES_ACCESS"},
		},
		{
			name:                "KUBERNETES_ACCESS が未知の値だとエラー",
			environment:         map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "yes"},
			expectedErrorPieces: []string{"KUBERNETES_ACCESS"},
		},
		{
			name:                "kubeconfig なのに KUBECONFIG が無いとエラー",
			environment:         map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "kubeconfig"},
			expectedErrorPieces: []string{"KUBECONFIG"},
		},
		{
			name:                "ログレベルが未知だとエラー",
			environment:         map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none", "LOG_LEVEL": "verbose"},
			expectedErrorPieces: []string{"LOG_LEVEL"},
		},
		{
			name:                "問題が複数あると、全てまとめて報告する",
			environment:         map[string]string{"LOG_LEVEL": "verbose"},
			expectedErrorPieces: []string{"DATABASE_URL", "KUBERNETES_ACCESS", "LOG_LEVEL"},
		},
	}

	// 各ケースを、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			actualConfig, actualError := LoadWorkerConfig(newTestReader(testCase.environment))

			// エラーを期待しないケースは、設定の一致を確認する。
			if len(testCase.expectedErrorPieces) == 0 {
				if actualError != nil {
					subTest.Fatalf("エラーにならないはず: %v", actualError)
				}
				if actualConfig != testCase.expectedConfig {
					subTest.Errorf("設定が違う: 期待 %+v、実際 %+v", testCase.expectedConfig, actualConfig)
				}
				return
			}

			// エラーを期待するケースは、全ての問題が含まれていることを確認する。
			if actualError == nil {
				subTest.Fatalf("エラーになるはず")
			}
			for _, expectedPiece := range testCase.expectedErrorPieces {
				if !strings.Contains(actualError.Error(), expectedPiece) {
					subTest.Errorf("エラーに %s が含まれていない: %v", expectedPiece, actualError)
				}
			}
		})
	}
}

// TestLoadWorkerConfigIngressNamespace は、外からの通信を中継する namespace(INGRESS_NAMESPACE)が、
// 省略すると空(許可しない)で、指定するとその値になることを確認する。
func TestLoadWorkerConfigIngressNamespace(testingT *testing.T) {
	// 必須の環境変数に、確かめたいものを足して読む道具。
	load := func(extra map[string]string) (WorkerConfig, error) {
		environment := map[string]string{"DATABASE_URL": "postgres://x", "KUBERNETES_ACCESS": "none"}
		for name, value := range extra {
			environment[name] = value
		}
		return LoadWorkerConfig(sharedconfig.NewEnvironmentReader(func(name string) (string, bool) {
			value, exists := environment[name]
			return value, exists
		}))
	}
	testingT.Run("省略すると空", func(subTest *testing.T) {
		loaded, loadError := load(nil)
		if loadError != nil || loaded.IngressNamespace != "" {
			subTest.Errorf("空のはず: %v %q", loadError, loaded.IngressNamespace)
		}
	})
	testingT.Run("指定すると、その値", func(subTest *testing.T) {
		loaded, loadError := load(map[string]string{"INGRESS_NAMESPACE": "traefik"})
		if loadError != nil || loaded.IngressNamespace != "traefik" {
			subTest.Errorf("traefik のはず: %v %q", loadError, loaded.IngressNamespace)
		}
	})
}
