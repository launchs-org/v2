package config

import (
	"log/slog"
	"reflect"
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
			expectedConfig: WorkerConfig{DatabaseURL: "postgres://db", TemporalAddress: "temporal:7233", TemporalNamespace: "default", LogLevel: slog.LevelInfo, BlockedNetworks: k8s.DefaultBlockedNetworks(), ServiceType: "ClusterIP", ClusterDomain: "cluster.local", VolumeAccessMode: "ReadWriteOnce", TraefikAPIVersion: "traefik.io/v1alpha1", TraefikEntryPoints: []string{"web"}, Kubernetes: k8s.Settings{Access: k8s.AccessNone}},
		},
		{
			name:           "全て設定すると、その値になる",
			environment:    map[string]string{"DATABASE_URL": "postgres://db", "TEMPORAL_ADDRESS": "host:1", "TEMPORAL_NAMESPACE": "ns", "LOG_LEVEL": "debug", "KUBERNETES_ACCESS": "kubeconfig", "KUBECONFIG": "/kube/config", "RUNTIME_CLASS": "gvisor"},
			expectedConfig: WorkerConfig{DatabaseURL: "postgres://db", TemporalAddress: "host:1", TemporalNamespace: "ns", LogLevel: slog.LevelDebug, RuntimeClass: "gvisor", BlockedNetworks: k8s.DefaultBlockedNetworks(), ServiceType: "ClusterIP", ClusterDomain: "cluster.local", VolumeAccessMode: "ReadWriteOnce", TraefikAPIVersion: "traefik.io/v1alpha1", TraefikEntryPoints: []string{"web"}, Kubernetes: k8s.Settings{Access: k8s.AccessKubeconfig, KubeconfigPath: "/kube/config"}},
		},
		{
			name:           "in-cluster も指定できる",
			environment:    map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "in-cluster"},
			expectedConfig: WorkerConfig{DatabaseURL: "postgres://db", TemporalAddress: "temporal:7233", TemporalNamespace: "default", LogLevel: slog.LevelInfo, BlockedNetworks: k8s.DefaultBlockedNetworks(), ServiceType: "ClusterIP", ClusterDomain: "cluster.local", VolumeAccessMode: "ReadWriteOnce", TraefikAPIVersion: "traefik.io/v1alpha1", TraefikEntryPoints: []string{"web"}, Kubernetes: k8s.Settings{Access: k8s.AccessInCluster}},
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
				if !reflect.DeepEqual(actualConfig, testCase.expectedConfig) {
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

// TestLoadWorkerConfigBlockedNetworks は、インターネットへの出口から除く内部のネットワーク(EGRESS_BLOCKED_NETWORKS)が、
// 省略すると既定で、指定するとその値になり、不正な値は、環境変数の名前を付けて拒否されることを確認する。
func TestLoadWorkerConfigBlockedNetworks(testingT *testing.T) {
	// 必須の環境変数に、確かめたいものを足して読む道具。
	load := func(blocked string) (WorkerConfig, error) {
		environment := map[string]string{"DATABASE_URL": "postgres://x", "KUBERNETES_ACCESS": "none"}
		if blocked != "" {
			environment["EGRESS_BLOCKED_NETWORKS"] = blocked
		}
		return LoadWorkerConfig(newTestReader(environment))
	}
	testingT.Run("省略すると、既定の5つ", func(subTest *testing.T) {
		loaded, loadError := load("")
		if loadError != nil || !reflect.DeepEqual(loaded.BlockedNetworks, k8s.DefaultBlockedNetworks()) {
			subTest.Errorf("既定のはず: %v %v", loadError, loaded.BlockedNetworks)
		}
	})
	testingT.Run("指定すると、その値(既定には足されず、置き換わる)", func(subTest *testing.T) {
		loaded, loadError := load("192.168.0.0/16, 203.0.113.0/24")
		if loadError != nil || !reflect.DeepEqual(loaded.BlockedNetworks, []string{"192.168.0.0/16", "203.0.113.0/24"}) {
			subTest.Errorf("指定した2つのはず: %v %v", loadError, loaded.BlockedNetworks)
		}
	})
	for _, invalid := range []string{"not-a-cidr", "10.0.0.0/8,2001:db8::/32", "0.0.0.0/0"} {
		testingT.Run("不正な値 "+invalid+" は、環境変数の名前を付けて拒否する", func(subTest *testing.T) {
			if _, loadError := load(invalid); loadError == nil || !strings.Contains(loadError.Error(), "EGRESS_BLOCKED_NETWORKS") {
				subTest.Errorf("名前を含むエラーのはず: %v", loadError)
			}
		})
	}
}

// TestLoadWorkerConfigServiceTypeAndClusterDomain は、Service の種類(SERVICE_TYPE)とクラスターのドメイン(CLUSTER_DOMAIN)の、
// 既定値・指定・不正な値の扱いを確認する。
func TestLoadWorkerConfigServiceTypeAndClusterDomain(testingT *testing.T) {
	// 必須の環境変数に、確かめたいものを足して読む道具。
	load := func(extra map[string]string) (WorkerConfig, error) {
		environment := map[string]string{"DATABASE_URL": "postgres://x", "KUBERNETES_ACCESS": "none"}
		for name, value := range extra {
			environment[name] = value
		}
		return LoadWorkerConfig(newTestReader(environment))
	}
	testingT.Run("省略すると、ClusterIP と cluster.local", func(subTest *testing.T) {
		loaded, loadError := load(nil)
		if loadError != nil || loaded.ServiceType != "ClusterIP" || loaded.ClusterDomain != "cluster.local" {
			subTest.Errorf("既定のはず: %v %q %q", loadError, loaded.ServiceType, loaded.ClusterDomain)
		}
	})
	for _, serviceType := range []string{"ClusterIP", "NodePort", "LoadBalancer"} {
		testingT.Run("SERVICE_TYPE に "+serviceType+" を指定できる", func(subTest *testing.T) {
			loaded, loadError := load(map[string]string{"SERVICE_TYPE": serviceType})
			if loadError != nil || loaded.ServiceType != serviceType {
				subTest.Errorf("%s のはず: %v %q", serviceType, loadError, loaded.ServiceType)
			}
		})
	}
	testingT.Run("CLUSTER_DOMAIN を指定できる", func(subTest *testing.T) {
		loaded, loadError := load(map[string]string{"CLUSTER_DOMAIN": "test-cluster.local"})
		if loadError != nil || loaded.ClusterDomain != "test-cluster.local" {
			subTest.Errorf("指定した値のはず: %v %q", loadError, loaded.ClusterDomain)
		}
	})
	for _, invalid := range []map[string]string{
		{"SERVICE_TYPE": "ExternalName"}, {"SERVICE_TYPE": "clusterip"}, {"SERVICE_TYPE": "Headless"},
		{"CLUSTER_DOMAIN": "Bad_Domain"}, {"CLUSTER_DOMAIN": ".leading.dot"}, {"CLUSTER_DOMAIN": "trailing.dot."}, {"CLUSTER_DOMAIN": "has space.local"},
		{"CLUSTER_DOMAIN": "-bad.local"}, {"CLUSTER_DOMAIN": strings.Repeat("a", 151)},
	} {
		testingT.Run("不正な値は、環境変数の名前を付けて拒否する", func(subTest *testing.T) {
			_, loadError := load(invalid)
			if loadError == nil || (!strings.Contains(loadError.Error(), "SERVICE_TYPE") && !strings.Contains(loadError.Error(), "CLUSTER_DOMAIN")) {
				subTest.Errorf("名前を含むエラーのはず: %v %v", invalid, loadError)
			}
		})
	}
}

// TestLoadWorkerConfigTraefik は、Traefik の apiVersion と entryPoint を、環境変数で変えられることと、不正な値を拒否することを確認する。
//
// 引数:
//   - testingT: テストの状態。
func TestLoadWorkerConfigTraefik(testingT *testing.T) {
	// 値を指定すると、既定値を置き換える(カンマ区切りで、空白と空の要素は捨てる)。
	loaded, loadError := LoadWorkerConfig(newTestReader(map[string]string{
		"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none", "TRAEFIK_API_VERSION": "traefik.containo.us/v1alpha1", "TRAEFIK_ENTRY_POINTS": " web, websecure ,",
	}))
	if loadError != nil || loaded.TraefikAPIVersion != "traefik.containo.us/v1alpha1" || !reflect.DeepEqual(loaded.TraefikEntryPoints, []string{"web", "websecure"}) {
		testingT.Fatalf("環境変数の値で置き換わるはず: %+v %v", loaded, loadError)
	}
	// group/version の形でない apiVersion と、空の entryPoint は、エラーにする。
	if _, invalidError := LoadWorkerConfig(newTestReader(map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none", "TRAEFIK_API_VERSION": "v1alpha1"})); invalidError == nil || !strings.Contains(invalidError.Error(), "TRAEFIK_API_VERSION") {
		testingT.Errorf("apiVersion の形が不正ならエラーになるはず: %v", invalidError)
	}
	if _, emptyError := LoadWorkerConfig(newTestReader(map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none", "TRAEFIK_ENTRY_POINTS": " , "})); emptyError == nil || !strings.Contains(emptyError.Error(), "TRAEFIK_ENTRY_POINTS") {
		testingT.Errorf("entryPoint が空ならエラーになるはず: %v", emptyError)
	}
}

// TestLoadWorkerConfigVolume は、PVC の StorageClass とアクセスモードを環境変数で変えられることと、不正な値を拒否することを確認する。
//
// 引数:
//   - testingT: テストの状態。
func TestLoadWorkerConfigVolume(testingT *testing.T) {
	// 値を指定すると、既定値を置き換える。
	loaded, loadError := LoadWorkerConfig(newTestReader(map[string]string{
		"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none", "VOLUME_STORAGE_CLASS": "longhorn", "VOLUME_ACCESS_MODE": "ReadWriteMany",
	}))
	if loadError != nil || loaded.VolumeStorageClass != "longhorn" || loaded.VolumeAccessMode != "ReadWriteMany" {
		testingT.Fatalf("環境変数の値で置き換わるはず: %+v %v", loaded, loadError)
	}
	// 未指定なら、StorageClass は空(クラスターの既定)、アクセスモードは ReadWriteOnce。
	defaults, defaultError := LoadWorkerConfig(newTestReader(map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none"}))
	if defaultError != nil || defaults.VolumeStorageClass != "" || defaults.VolumeAccessMode != "ReadWriteOnce" {
		testingT.Errorf("既定のはず: %+v %v", defaults, defaultError)
	}
	// 知らないアクセスモードは、エラーにする。
	if _, invalidError := LoadWorkerConfig(newTestReader(map[string]string{"DATABASE_URL": "postgres://db", "KUBERNETES_ACCESS": "none", "VOLUME_ACCESS_MODE": "ReadOnlyMany"})); invalidError == nil || !strings.Contains(invalidError.Error(), "VOLUME_ACCESS_MODE") {
		testingT.Errorf("エラーになるはず: %v", invalidError)
	}
}
