package activity

import "context"

// IngressRouteRule は、IngressRoute のルール1つ。path に一致した通信を、同じ namespace の Service のポートへ流す。
type IngressRouteRule struct {
	// Path は、前方一致させる path。
	Path string

	// ServiceName は、流す先の Service の名前(service の ID)。
	ServiceName string

	// Port は、流す先の Service のポート番号。
	Port int32
}

// IngressRouteDefinition は、IngressRoute に反映する内容。
type IngressRouteDefinition struct {
	// Namespace は、IngressRoute を置く namespace(プロジェクトの ID)。
	Namespace string

	// Name は、IngressRoute の名前(ingress の ID)。
	Name string

	// Host は、受け付けるホスト名。
	Host string

	// Rules は、path ごとのルール。空のときは、IngressRoute を置かない(あれば削除する)。
	Rules []IngressRouteRule
}

// IngressOperator は、k8s の Traefik の IngressRoute を操作するもの。
// 実際に k8s を操作する実装と、操作せずログに残すだけの実装(開発用)を、差し替えられるようにする。
type IngressOperator interface {
	// ApplyIngressRoute は、IngressRoute を、渡された内容にする(無ければ作り、あれば更新する。冪等)。
	// ルールが空なら、IngressRoute を削除する(無くてもエラーにしない)。
	ApplyIngressRoute(requestContext context.Context, definition IngressRouteDefinition) error

	// DeleteIngressRoute は、IngressRoute を削除する(無くてもエラーにしない。冪等)。
	DeleteIngressRoute(requestContext context.Context, namespaceName string, ingressName string) error
}
