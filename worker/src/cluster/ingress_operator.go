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
}

// KubernetesIngressOperator は、動的クライアントで、Traefik の IngressRoute を操作する IngressOperator の実装。
type KubernetesIngressOperator struct {
	// resource は、IngressRoute の API のリソース(group、version、resource)。
	resource schema.GroupVersionResource

	// apiVersion は、IngressRoute に書く apiVersion(group/version)。
	apiVersion string

	// entryPoints は、IngressRoute の entryPoints。
	entryPoints []string

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
	// IngressRoute を組み立てる。
	desired := operator.buildIngressRoute(definition)
	routes := operator.client.Resource(operator.resource).Namespace(definition.Namespace)
	// まず作ってみる。無ければ作れる。
	_, createError := routes.Create(requestContext, desired, metav1.CreateOptions{})
	if createError == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(createError) {
		return fmt.Errorf("IngressRoute %s を作れませんでした: %w", definition.Name, createError)
	}
	// すでにあるときは、resourceVersion を引き継いで、更新する。
	existing, getError := routes.Get(requestContext, definition.Name, metav1.GetOptions{})
	if getError != nil {
		return fmt.Errorf("IngressRoute %s を取得できませんでした: %w", definition.Name, getError)
	}
	desired.SetResourceVersion(existing.GetResourceVersion())
	if _, updateError := routes.Update(requestContext, desired, metav1.UpdateOptions{}); updateError != nil {
		return fmt.Errorf("IngressRoute %s を更新できませんでした: %w", definition.Name, updateError)
	}
	return nil
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
	return nil
}

// buildIngressRoute は、定義から、IngressRoute のオブジェクトを組み立てる。
//
// ルールごとに、Host(...) && PathPrefix(...) の式で、Service のポートへ流す。
// Traefik は、式が長いルールを先に評価するので、長い path が、短い path(例: "/")より優先される。
//
// 引数:
//   - definition: 反映する内容。
//
// 戻り値:
//   - 組み立てた IngressRoute。
func (operator *KubernetesIngressOperator) buildIngressRoute(definition activity.IngressRouteDefinition) *unstructured.Unstructured {
	// ルールごとの routes を作る。
	routes := make([]any, 0, len(definition.Rules))
	for _, rule := range definition.Rules {
		routes = append(routes, map[string]any{
			"kind":  "Rule",
			"match": "Host(`" + definition.Host + "`) && PathPrefix(`" + rule.Path + "`)",
			"services": []any{
				map[string]any{"name": rule.ServiceName, "port": int64(rule.Port)},
			},
		})
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
