// Package config は、ワーカーの設定を環境変数から読む。
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	sharedconfig "launchs/shared/config"
	"launchs/shared/k8s"
	"launchs/shared/logger"
)

const (
	// defaultTemporalAddress は、TEMPORAL_ADDRESS が無いときに使う Temporal サーバーの接続先。
	defaultTemporalAddress = "temporal:7233"

	// defaultTemporalNamespace は、TEMPORAL_NAMESPACE が無いときに使う Temporal の namespace。
	defaultTemporalNamespace = "default"

	// defaultLogLevelText は、LOG_LEVEL が無いときに使うログレベル。
	defaultLogLevelText = "info"

	// defaultServiceType は、SERVICE_TYPE が無いときに使う、k8s の Service の種類。クラスター内だけに公開する。
	defaultServiceType = "ClusterIP"

	// defaultClusterDomain は、CLUSTER_DOMAIN が無いときに使う、クラスターのドメイン(k8s の既定)。
	defaultClusterDomain = "cluster.local"

	// maxClusterDomainLength は、クラスターのドメインの最大の長さ(DNS 名の長さの上限 253 から、
	// "<serviceId>.<projectId>.svc." の分を引いた余裕)。
	maxClusterDomainLength = 150

	// defaultTraefikAPIVersion は、Traefik の IngressRoute の apiVersion の既定値。
	defaultTraefikAPIVersion = "traefik.io/v1alpha1"

	// defaultTraefikEntryPoints は、IngressRoute を受け付ける entryPoint の名前の既定値。
	defaultTraefikEntryPoints = "web"
)

// validServiceTypes は、SERVICE_TYPE に指定できる、k8s の Service の種類。
var validServiceTypes = map[string]bool{"ClusterIP": true, "NodePort": true, "LoadBalancer": true}

// clusterDomainPattern は、クラスターのドメインとして正しい形(小文字の英数字とハイフンを、ドットで区切る)。
var clusterDomainPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

// WorkerConfig は、ワーカーの設定。
type WorkerConfig struct {
	// DatabaseURL は、app データベースの接続先(環境変数 DATABASE_URL)。パスワードを含むので、ログに出さない。
	DatabaseURL string

	// TemporalAddress は、Temporal サーバーの接続先(環境変数 TEMPORAL_ADDRESS)。
	TemporalAddress string

	// TemporalNamespace は、使う Temporal の namespace(環境変数 TEMPORAL_NAMESPACE)。
	TemporalNamespace string

	// LogLevel は、出力するログの最低のレベル(環境変数 LOG_LEVEL)。
	LogLevel slog.Level

	// RuntimeClass は、deploy の Pod に指定する RuntimeClass の名前(環境変数 RUNTIME_CLASS)。
	// 空なら指定せず、クラスタの既定のランタイムを使う。
	RuntimeClass string

	// IngressNamespace は、外からの通信を中継する namespace の名前(環境変数 INGRESS_NAMESPACE。Traefik など)。
	// プロジェクトの namespace の隔離の NetworkPolicy で、ここからの通信を許可する。空なら、許可しない。
	IngressNamespace string

	// BlockedNetworks は、プロジェクトの namespace から、インターネットへの出口の許可から除く、内部のネットワーク(CIDR)
	// (環境変数 EGRESS_BLOCKED_NETWORKS。カンマ区切り)。省略すると、既定(k8s.DefaultBlockedNetworks)。
	BlockedNetworks []string

	// ServiceType は、deploy の service として作る k8s の Service の種類(環境変数 SERVICE_TYPE)。
	// ClusterIP(既定)、NodePort、LoadBalancer のどれか。
	ServiceType string

	// ClusterDomain は、クラスターのドメイン(環境変数 CLUSTER_DOMAIN。既定 cluster.local)。
	// service の DNS 名("<serviceId>.<projectId>.svc.<ドメイン>")の最後の部分になる。
	ClusterDomain string

	// TraefikAPIVersion は、Traefik の IngressRoute の apiVersion(環境変数 TRAEFIK_API_VERSION)。
	TraefikAPIVersion string

	// TraefikEntryPoints は、IngressRoute を受け付ける entryPoint の名前(環境変数 TRAEFIK_ENTRY_POINTS。カンマ区切り)。
	TraefikEntryPoints []string

	// Kubernetes は、k8s への接続の設定(環境変数 KUBERNETES_ACCESS、KUBECONFIG)。読み方は shared/k8s にある。
	Kubernetes k8s.Settings
}

// LoadWorkerConfig は、環境変数からワーカーの設定を読む。
//
// 読む環境変数:
//   - DATABASE_URL: app データベースの接続先(必須)。
//   - TEMPORAL_ADDRESS: Temporal サーバーの接続先。省略すると temporal:7233。
//   - TEMPORAL_NAMESPACE: Temporal の namespace。省略すると default。
//   - RUNTIME_CLASS: deploy の Pod に指定する RuntimeClass。省略すると、指定しない(クラスタの既定)。
//   - SERVICE_TYPE: service として作る k8s の Service の種類(ClusterIP、NodePort、LoadBalancer)。省略すると ClusterIP。
//     LoadBalancer は、deploy ごとに外部 IP を使い、利用者のポート(UDP を含む)を、LAN に公開する。
//   - CLUSTER_DOMAIN: クラスターのドメイン。省略すると cluster.local(クラスターごとに違う。CoreDNS の設定で確かめる)。
//   - EGRESS_BLOCKED_NETWORKS: プロジェクトの namespace から、出ていく通信を許可しない内部のネットワーク(CIDR のカンマ区切り。IPv4)。
//     省略すると、既定(10.0.0.0/8、172.16.0.0/12、192.168.0.0/16、169.254.0.0/16、100.64.0.0/10)。何も除かない設定は、できない。
//   - INGRESS_NAMESPACE: 外からの通信を中継する namespace(Traefik など)。省略すると、その namespace からの通信は許可しない。
//   - KUBERNETES_ACCESS、KUBECONFIG: k8s への接続の設定(読み方と意味は shared/k8s の ReadSettings)。
//   - LOG_LEVEL: ログレベル(debug、info、warn、error)。省略すると info。
//
// 設定に問題があっても止まらず、見つかった全ての問題をまとめて報告する。
//
// 引数:
//   - reader: 環境変数を読む EnvironmentReader。
//
// 戻り値:
//   - 読み込んだ設定。問題があったときは、ゼロ値。
//   - 問題があったときのエラー。
func LoadWorkerConfig(reader *sharedconfig.EnvironmentReader) (WorkerConfig, error) {
	// 各環境変数を読む。
	databaseURL := reader.RequiredString("DATABASE_URL")
	temporalAddress := reader.OptionalString("TEMPORAL_ADDRESS", defaultTemporalAddress)
	temporalNamespace := reader.OptionalString("TEMPORAL_NAMESPACE", defaultTemporalNamespace)
	logLevelText := reader.OptionalString("LOG_LEVEL", defaultLogLevelText)
	runtimeClass := reader.OptionalString("RUNTIME_CLASS", "")
	ingressNamespace := reader.OptionalString("INGRESS_NAMESPACE", "")
	blockedNetworksText := reader.OptionalString("EGRESS_BLOCKED_NETWORKS", "")
	serviceType := reader.OptionalString("SERVICE_TYPE", defaultServiceType)
	clusterDomain := reader.OptionalString("CLUSTER_DOMAIN", defaultClusterDomain)
	traefikAPIVersion := reader.OptionalString("TRAEFIK_API_VERSION", defaultTraefikAPIVersion)
	traefikEntryPointsText := reader.OptionalString("TRAEFIK_ENTRY_POINTS", defaultTraefikEntryPoints)
	kubernetesSettings := k8s.ReadSettings(reader)

	// 読み込み中に見つかった問題を集める。
	loadError := reader.Err()

	// ログレベルの文字列を変換し、未知の値なら問題に加える。
	logLevel, levelError := logger.ParseLevel(logLevelText)
	if levelError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 LOG_LEVEL: %w", levelError))
	}

	// Service の種類を検査する。
	if !validServiceTypes[serviceType] {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 SERVICE_TYPE は ClusterIP、NodePort、LoadBalancer のどれかでなければなりません: %q", serviceType))
	}
	// クラスターのドメインを検査する(DNS 名として正しい形)。
	if !clusterDomainPattern.MatchString(clusterDomain) || len(clusterDomain) > maxClusterDomainLength {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 CLUSTER_DOMAIN は、小文字の英数字とハイフンを、ドットで区切った DNS 名でなければなりません: %q", clusterDomain))
	}

	// 除く内部のネットワークを検査し、問題があれば、環境変数の名前を付けて加える。
	blockedNetworks, blockedError := k8s.ParseBlockedNetworks(blockedNetworksText)
	if blockedError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 EGRESS_BLOCKED_NETWORKS: %w", blockedError))
	}

	// entryPoint の名前を、カンマで分けて取り出す(空の要素は捨てる)。
	traefikEntryPoints := splitCommaSeparated(traefikEntryPointsText)
	if len(traefikEntryPoints) == 0 {
		loadError = errors.Join(loadError, errors.New("環境変数 TRAEFIK_ENTRY_POINTS には、entryPoint の名前を1つ以上指定してください"))
	}
	// apiVersion は、group/version の形でなければならない。
	if !strings.Contains(traefikAPIVersion, "/") {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 TRAEFIK_API_VERSION は group/version の形でなければなりません: %q", traefikAPIVersion))
	}

	// k8s への接続の設定の値を検査し、問題があれば加える。
	if validateError := kubernetesSettings.Validate(); validateError != nil {
		loadError = errors.Join(loadError, validateError)
	}

	// 問題があれば、ゼロ値の設定とエラーを返す。
	if loadError != nil {
		return WorkerConfig{}, loadError
	}
	// 問題が無ければ、設定を返す。
	return WorkerConfig{
		DatabaseURL:        databaseURL,
		TemporalAddress:    temporalAddress,
		TemporalNamespace:  temporalNamespace,
		LogLevel:           logLevel,
		RuntimeClass:       runtimeClass,
		IngressNamespace:   ingressNamespace,
		BlockedNetworks:    blockedNetworks,
		ServiceType:        serviceType,
		ClusterDomain:      clusterDomain,
		TraefikAPIVersion:  traefikAPIVersion,
		TraefikEntryPoints: traefikEntryPoints,
		Kubernetes:         kubernetesSettings,
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
