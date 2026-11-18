package activity

import "context"

// ServicePortDefinition は、k8s の Service に公開するポート1つ。
type ServicePortDefinition struct {
	// Name は、ポートの名前。
	Name string

	// Port は、ポート番号。Service のポートと、Pod のポートに、同じ番号を使う。
	Port int32

	// Protocol は、"tcp" または "udp"。
	Protocol string
}

// ServiceDefinition は、k8s に作る Service の内容。
//
// DB の service から組み立てる。k8s の型を、Activity に持ち込まないための入れ物。
type ServiceDefinition struct {
	// Namespace は、Service を作る namespace(プロジェクトの ID)。
	Namespace string

	// Name は、Service の名前(service の ID)。
	Name string

	// DeployID は、Service が向ける deploy の ID。この deploy の Pod に、通信を振り分ける。
	DeployID string

	// Type は、Service の種類("ClusterIP"、"NodePort"、"LoadBalancer")。
	Type string

	// Ports は、公開するポート。
	Ports []ServicePortDefinition
}

// ServiceState は、k8s が払い出した Service の値。
type ServiceState struct {
	// ClusterIP は、払い出された ClusterIP。払い出されないとき(None など)は空。
	ClusterIP string
}

// ServiceOperator は、k8s の Service を操作する。
//
// 実体は、client-go で操作する KubernetesServiceOperator(worker/src/cluster)。
// k8s につながない設定(KUBERNETES_ACCESS=none)のときは、LoggingServiceOperator を使う。
type ServiceOperator interface {
	// ApplyService は、Service を作る。すでにあれば内容を更新する(冪等)。払い出された値を返す。
	ApplyService(requestContext context.Context, definition ServiceDefinition) (ServiceState, error)

	// DeleteService は、Service を削除する。無ければ何もしない(冪等)。
	DeleteService(requestContext context.Context, namespaceName string, serviceName string) error
}
