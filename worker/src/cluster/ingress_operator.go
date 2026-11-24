package cluster

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"launchs/worker/src/activity"
)

const (
	// ingressRouteKind と ingressRouteResource は、Traefik の IngressRoute の種類と、API のリソース名。
	ingressRouteKind     = "IngressRoute"
	ingressRouteResource = "ingressroutes"

	// middlewareKind と middlewareResource は、Traefik の Middleware の種類と、API のリソース名。
	middlewareKind     = "Middleware"
	middlewareResource = "middlewares"

	// statsMiddlewareSuffix は、アクセスの集計の Middleware の名前の末尾(名前は、ingress の ID + この文字列)。
	statsMiddlewareSuffix = "-stats"

	// statsIntervalSeconds は、アクセスの集計を出力する間隔(秒)。
	statsIntervalSeconds = 10
)

// ingressNamePattern は、IngressRoute の名前として受け付ける形(ingress の ID: "ingress-" + UUID v4)。
var ingressNamePattern = regexp.MustCompile(`^ingress-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// hostPattern は、IngressRoute の Host(...) に埋め込むホスト名として受け付ける形(小文字の英数字、ハイフン、ドット)。
var hostPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

// IngressRouteSettings は、KubernetesIngressOperator の設定。
type IngressRouteSettings struct {
	// APIVersion は、Traefik の IngressRoute の apiVersion(環境変数 TRAEFIK_API_VERSION。例: traefik.io/v1alpha1)。
	APIVersion string

	// EntryPoints は、IngressRoute を受け付ける Traefik の entryPoint の名前(環境変数 TRAEFIK_ENTRY_POINTS。例: web)。
	EntryPoints []string

	// StatsPlugin は、アクセスの集計の Traefik プラグインの名前(環境変数 INGRESS_STATS_PLUGIN。例: launchs-stats)。
	// 空なら、この機能は使わない(Middleware を作らない。Traefik にプラグインが無いのに Middleware を参照すると、ルートが壊れるため)。
	StatsPlugin string
}

// KubernetesIngressOperator は、動的クライアントで、Traefik の IngressRoute を操作する IngressOperator の実装。
type KubernetesIngressOperator struct {
	// resource は、IngressRoute の API のリソース(group、version、resource)。
	resource schema.GroupVersionResource

	// apiVersion は、IngressRoute に書く apiVersion(group/version)。
	apiVersion string

	// entryPoints は、IngressRoute の entryPoints。
	entryPoints []string

	// middlewareResource は、Middleware の API のリソース(IngressRoute と同じ group と version)。
	middlewareResource schema.GroupVersionResource

	// statsPlugin は、アクセスの集計のプラグインの名前。空なら、使わない。
	statsPlugin string

	// client は、型を持たない k8s のクライアント。
	client dynamic.Interface
}

// NewKubernetesIngressOperator は、KubernetesIngressOperator を作る。
//
// 引数:
//   - client: 型を持たない k8s のクライアント。
//   - settings: Traefik の apiVersion と entryPoints。apiVersion は、"group/version" の形でなければならない。
//
// 戻り値:
//   - 作成した KubernetesIngressOperator。apiVersion の形が不正ならエラー。
func NewKubernetesIngressOperator(client dynamic.Interface, settings IngressRouteSettings) (*KubernetesIngressOperator, error) {
	// apiVersion を、group と version に分ける。
	parsed, parseError := schema.ParseGroupVersion(settings.APIVersion)
	if parseError != nil || parsed.Group == "" || parsed.Version == "" {
		return nil, fmt.Errorf("TRAEFIK_API_VERSION は group/version の形でなければなりません: %q", settings.APIVersion)
	}
	// 値を持たせて返す。
	return &KubernetesIngressOperator{
		resource:    parsed.WithResource(ingressRouteResource),
		apiVersion:  settings.APIVersion,
		entryPoints: settings.EntryPoints,
		client:      client,

		middlewareResource: parsed.WithResource(middlewareResource),
		statsPlugin:        strings.TrimSpace(settings.StatsPlugin),
	}, nil
}

// ApplyIngressRoute は、IngressRoute を、渡された内容にする(無ければ作り、あれば更新する。冪等)。
//
// ルールが空のときは、IngressRoute を削除する(ルールの無い IngressRoute を置かない)。
// 受け付けるのは、プロジェクトの ID の形の namespace と、ingress の ID の形の名前、安全な形のホスト名と path だけ。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - definition: 反映する内容。
//
// 戻り値:
//   - 名前の形が不正なとき(ErrInvalid...)、k8s への依頼に失敗したときのエラー。
func (operator *KubernetesIngressOperator) ApplyIngressRoute(requestContext context.Context, definition activity.IngressRouteDefinition) error {
	// 触ってよい namespace と名前かを、k8s に触れる前に確かめる。
	if validateError := validateProjectNamespace(definition.Namespace); validateError != nil {
		return validateError
	}
	if !ingressNamePattern.MatchString(definition.Name) {
		return fmt.Errorf("%w: %q", activity.ErrInvalidIngressName, definition.Name)
	}
	// ルールが空なら、IngressRoute を置かない。
	if len(definition.Rules) == 0 {
		return operator.DeleteIngressRoute(requestContext, definition.Namespace, definition.Name)
	}
	// 式に埋め込む値の形を確かめる。
	if !hostPattern.MatchString(definition.Host) {
		return fmt.Errorf("ホスト名の形が正しくありません: %q", definition.Host)
	}
	// カスタムドメインも、ルールの式に埋め込むので、同じ形かを確かめる。
	for _, extraHost := range definition.ExtraHosts {
		if !hostPattern.MatchString(extraHost) {
			return fmt.Errorf("ホスト名の形が正しくありません: %q", extraHost)
		}
	}
	// アクセスの集計を付けるかを決める(プラグインが無効な環境では、付けない)。
	withStats := definition.AccessStats && operator.statsPlugin != ""
	// 付けるなら、IngressRoute より先に、Middleware を用意する(参照先が無い時間を作らない)。
	if withStats {
		if upsertError := operator.upsert(requestContext, operator.middlewareResource, definition.Namespace, operator.buildStatsMiddleware(definition)); upsertError != nil {
			return fmt.Errorf("Middleware %s を反映できませんでした: %w", statsMiddlewareName(definition.Name), upsertError)
		}
	}
	// IngressRoute を反映する。
	if upsertError := operator.upsert(requestContext, operator.resource, definition.Namespace, operator.buildIngressRoute(definition, withStats)); upsertError != nil {
		return fmt.Errorf("IngressRoute %s を反映できませんでした: %w", definition.Name, upsertError)
	}
	// 付けないなら、IngressRoute が参照しなくなってから、Middleware を消す(機能が無効な環境では、触らない)。
	if !withStats && operator.statsPlugin != "" {
		return operator.deleteStatsMiddleware(requestContext, definition.Namespace, definition.Name)
	}
	return nil
}

// upsert は、オブジェクトを、無ければ作り、あれば resourceVersion を引き継いで更新する(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - resource: 対象の API のリソース。
//   - namespaceName: namespace の名前。
//   - desired: 反映するオブジェクト。
//
// 戻り値:
//   - k8s への依頼に失敗したときのエラー。
func (operator *KubernetesIngressOperator) upsert(requestContext context.Context, resource schema.GroupVersionResource, namespaceName string, desired *unstructured.Unstructured) error {
	// まず作ってみる。
	client := operator.client.Resource(resource).Namespace(namespaceName)
	_, createError := client.Create(requestContext, desired, metav1.CreateOptions{})
	if createError == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(createError) {
		return createError
	}
	// すでにあるときは、resourceVersion を引き継いで、更新する。
	existing, getError := client.Get(requestContext, desired.GetName(), metav1.GetOptions{})
	if getError != nil {
		return getError
	}
	desired.SetResourceVersion(existing.GetResourceVersion())
	_, updateError := client.Update(requestContext, desired, metav1.UpdateOptions{})
	return updateError
}

// statsMiddlewareName は、ingress の、アクセスの集計の Middleware の名前を返す。
func statsMiddlewareName(ingressName string) string {
	// ingress の ID に、末尾を足す。
	return ingressName + statsMiddlewareSuffix
}

// deleteStatsMiddleware は、アクセスの集計の Middleware を削除する(無くてもエラーにしない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: namespace の名前。
//   - ingressName: ingress の ID。
//
// 戻り値:
//   - k8s への依頼に失敗したときのエラー。
func (operator *KubernetesIngressOperator) deleteStatsMiddleware(requestContext context.Context, namespaceName string, ingressName string) error {
	// 削除する。無ければ、すでに目的どおり。
	deleteError := operator.client.Resource(operator.middlewareResource).Namespace(namespaceName).Delete(requestContext, statsMiddlewareName(ingressName), metav1.DeleteOptions{})
	if deleteError != nil && !apierrors.IsNotFound(deleteError) {
		return fmt.Errorf("Middleware %s を削除できませんでした: %w", statsMiddlewareName(ingressName), deleteError)
	}
	return nil
}

// buildStatsMiddleware は、アクセスの集計の Middleware(プラグイン launchs-stats の設定)を組み立てる。
//
// 引数:
//   - definition: 反映する内容(ingress の ID を、集計の宛先として設定する)。
//
// 戻り値:
//   - 組み立てた Middleware。
func (operator *KubernetesIngressOperator) buildStatsMiddleware(definition activity.IngressRouteDefinition) *unstructured.Unstructured {
	// プラグインの設定を持つ Middleware を返す。
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": operator.apiVersion,
		"kind":       middlewareKind,
		"metadata": map[string]any{
			"name":      statsMiddlewareName(definition.Name),
			"namespace": definition.Namespace,
			"labels":    map[string]any{projectIDLabel: definition.Namespace},
		},
		"spec": map[string]any{
			"plugin": map[string]any{
				operator.statsPlugin: map[string]any{
					"ingressId":       definition.Name,
					"intervalSeconds": int64(statsIntervalSeconds),
				},
			},
		},
	}}
}

// DeleteIngressRoute は、IngressRoute を削除する(無くてもエラーにしない。冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: namespace の名前(プロジェクトの ID の形)。
//   - ingressName: IngressRoute の名前(ingress の ID の形)。
//
// 戻り値:
//   - 名前の形が不正なとき、k8s への依頼に失敗したときのエラー。
func (operator *KubernetesIngressOperator) DeleteIngressRoute(requestContext context.Context, namespaceName string, ingressName string) error {
	// 触ってよい namespace と名前かを、k8s に触れる前に確かめる。
	if validateError := validateProjectNamespace(namespaceName); validateError != nil {
		return validateError
	}
	if !ingressNamePattern.MatchString(ingressName) {
		return fmt.Errorf("%w: %q", activity.ErrInvalidIngressName, ingressName)
	}
	// 削除する。無ければ、すでに目的どおりなので、成功にする。
	deleteError := operator.client.Resource(operator.resource).Namespace(namespaceName).Delete(requestContext, ingressName, metav1.DeleteOptions{})
	if deleteError != nil && !apierrors.IsNotFound(deleteError) {
		return fmt.Errorf("IngressRoute %s を削除できませんでした: %w", ingressName, deleteError)
	}
	// 集計の Middleware も消す(機能が無効な環境では、触らない)。
	if operator.statsPlugin != "" {
		return operator.deleteStatsMiddleware(requestContext, namespaceName, ingressName)
	}
	return nil
}

// hostMatchExpression は、受け付けるホスト名の、Traefik のルールの式を作る。
// 払い出したホスト名だけなら Host(`a`)、カスタムドメインもあれば (Host(`a`) || Host(`b`)) にする。
//
// 引数:
//   - definition: 反映する内容(Host と ExtraHosts を使う)。
//
// 戻り値:
//   - ルールの式の、ホストの部分。
func hostMatchExpression(definition activity.IngressRouteDefinition) string {
	// 払い出したホスト名だけのときは、これまでと同じ式にする。
	if len(definition.ExtraHosts) == 0 {
		return "Host(`" + definition.Host + "`)"
	}
	// 払い出したホスト名と、カスタムドメインを、「または」でつなぐ。
	expressions := []string{"Host(`" + definition.Host + "`)"}
	for _, extraHost := range definition.ExtraHosts {
		expressions = append(expressions, "Host(`"+extraHost+"`)")
	}
	return "(" + strings.Join(expressions, " || ") + ")"
}

// buildIngressRoute は、定義から、IngressRoute のオブジェクトを組み立てる。
//
// ルールごとに、Host(...) && PathPrefix(...) の式で、Service のポートへ流す。
// Traefik は、式が長いルールを先に評価するので、長い path が、短い path(例: "/")より優先される。
//
// 引数:
//   - definition: 反映する内容。
//   - withStats: アクセスの集計の Middleware を、各ルートに付けるか。
//
// 戻り値:
//   - 組み立てた IngressRoute。
func (operator *KubernetesIngressOperator) buildIngressRoute(definition activity.IngressRouteDefinition, withStats bool) *unstructured.Unstructured {
	// ルールごとの routes を作る。
	routes := make([]any, 0, len(definition.Rules))
	for _, rule := range definition.Rules {
		route := map[string]any{
			"kind":  "Rule",
			"match": hostMatchExpression(definition) + " && PathPrefix(`" + rule.Path + "`)",
			"services": []any{
				map[string]any{"name": rule.ServiceName, "port": int64(rule.Port)},
			},
		}
		// 集計を付けるなら、Middleware を参照する。
		if withStats {
			route["middlewares"] = []any{map[string]any{"name": statsMiddlewareName(definition.Name)}}
		}
		routes = append(routes, route)
	}
	// entryPoints を、型を持たない形にする。
	entryPoints := make([]any, 0, len(operator.entryPoints))
	for _, entryPoint := range operator.entryPoints {
		entryPoints = append(entryPoints, strings.TrimSpace(entryPoint))
	}
	// オブジェクトを返す。
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": operator.apiVersion,
		"kind":       ingressRouteKind,
		"metadata": map[string]any{
			"name":      definition.Name,
			"namespace": definition.Namespace,
			"labels":    map[string]any{projectIDLabel: definition.Namespace},
		},
		"spec": map[string]any{
			"entryPoints": entryPoints,
			"routes":      routes,
		},
	}}
}
