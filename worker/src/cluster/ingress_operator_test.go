package cluster

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"launchs/worker/src/activity"
)

// testIngressName は、テスト用の IngressRoute の名前(ingress の ID の形)。
const testIngressName = "ingress-0a1b2c3d-0000-4000-8000-000000000001"

// ingressRouteResource は、テストで使う IngressRoute のリソース。
var testIngressRouteResource = schema.GroupVersionResource{Group: "traefik.io", Version: "v1alpha1", Resource: "ingressroutes"}

// newTestIngressOperator は、偽の動的クライアントでつないだ KubernetesIngressOperator を作る。
//
// 戻り値:
//   - 作成した KubernetesIngressOperator と、偽のクライアント。
func newTestIngressOperator(testingT *testing.T) (*KubernetesIngressOperator, *dynamicfake.FakeDynamicClient) {
	// IngressRoute の一覧の型を、偽のクライアントに教えて作る。
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{testIngressRouteResource: "IngressRouteList"})
	operator, createError := NewKubernetesIngressOperator(fakeClient, IngressRouteSettings{APIVersion: "traefik.io/v1alpha1", EntryPoints: []string{"web", "websecure"}})
	if createError != nil {
		testingT.Fatalf("作れるはず: %v", createError)
	}
	return operator, fakeClient
}

// newTestIngressDefinition は、テスト用の IngressRoute の内容(/ → web の 8080、/api → api の 9090)を作る。
func newTestIngressDefinition() activity.IngressRouteDefinition {
	return activity.IngressRouteDefinition{
		Namespace: validNamespaceName, Name: testIngressName, Host: "brave-blue-fox.launchs.org",
		Rules: []activity.IngressRouteRule{
			{Path: "/", ServiceName: "service-web", Port: 8080},
			{Path: "/api", ServiceName: "service-api", Port: 9090},
		},
	}
}

// TestKubernetesIngressOperatorApply は、IngressRoute を、式・向き先・entryPoint を正しく設定して作り、
// すでにあれば更新し、ルールが空なら消し、不正な値を k8s に触れずに拒否することを確認する。
func TestKubernetesIngressOperatorApply(testingT *testing.T) {
	ctx := context.Background()

	testingT.Run("IngressRoute を作り、Host と PathPrefix の式、Service のポート、entryPoints が設定される", func(subTest *testing.T) {
		operator, fakeClient := newTestIngressOperator(subTest)
		if applyError := operator.ApplyIngressRoute(ctx, newTestIngressDefinition()); applyError != nil {
			subTest.Fatalf("作れるはず: %v", applyError)
		}
		route, getError := fakeClient.Resource(testIngressRouteResource).Namespace(validNamespaceName).Get(ctx, testIngressName, metav1.GetOptions{})
		if getError != nil {
			subTest.Fatalf("IngressRoute があるはず: %v", getError)
		}
		entryPoints, _, _ := unstructured.NestedStringSlice(route.Object, "spec", "entryPoints")
		routes, _, _ := unstructured.NestedSlice(route.Object, "spec", "routes")
		if route.GetKind() != "IngressRoute" || route.GetAPIVersion() != "traefik.io/v1alpha1" || len(entryPoints) != 2 || entryPoints[1] != "websecure" || len(routes) != 2 {
			subTest.Fatalf("内容が違う: %v", route.Object)
		}
		second := routes[1].(map[string]any)
		services := second["services"].([]any)[0].(map[string]any)
		if second["match"] != "Host(`brave-blue-fox.launchs.org`) && PathPrefix(`/api`)" || services["name"] != "service-api" || services["port"] != int64(9090) {
			subTest.Errorf("2つ目のルールが違う: %v", second)
		}
		if route.GetLabels()[projectIDLabel] != validNamespaceName {
			subTest.Errorf("プロジェクトのラベルが付くはず: %v", route.GetLabels())
		}
	})
	testingT.Run("すでにあるときは、更新する(ルールが変わる)", func(subTest *testing.T) {
		operator, fakeClient := newTestIngressOperator(subTest)
		_ = operator.ApplyIngressRoute(ctx, newTestIngressDefinition())
		changed := newTestIngressDefinition()
		changed.Rules = changed.Rules[:1]
		if applyError := operator.ApplyIngressRoute(ctx, changed); applyError != nil {
			subTest.Fatalf("更新できるはず: %v", applyError)
		}
		route, _ := fakeClient.Resource(testIngressRouteResource).Namespace(validNamespaceName).Get(ctx, testIngressName, metav1.GetOptions{})
		routes, _, _ := unstructured.NestedSlice(route.Object, "spec", "routes")
		if len(routes) != 1 {
			subTest.Errorf("ルールが1つになるはず: %d", len(routes))
		}
	})
	testingT.Run("ルールが空なら、IngressRoute を消す(無くてもエラーにしない)", func(subTest *testing.T) {
		operator, fakeClient := newTestIngressOperator(subTest)
		_ = operator.ApplyIngressRoute(ctx, newTestIngressDefinition())
		empty := newTestIngressDefinition()
		empty.Rules = nil
		if applyError := operator.ApplyIngressRoute(ctx, empty); applyError != nil {
			subTest.Fatalf("消せるはず: %v", applyError)
		}
		if _, getError := fakeClient.Resource(testIngressRouteResource).Namespace(validNamespaceName).Get(ctx, testIngressName, metav1.GetOptions{}); !apierrors.IsNotFound(getError) {
			subTest.Errorf("IngressRoute は消えているはず: %v", getError)
		}
		if again := operator.ApplyIngressRoute(ctx, empty); again != nil {
			subTest.Errorf("無くてもエラーにしないはず: %v", again)
		}
	})
	testingT.Run("不正な namespace・名前・ホスト名は、k8s に触れずに拒否する", func(subTest *testing.T) {
		for name, mutate := range map[string]func(*activity.IngressRouteDefinition){
			"namespace": func(definition *activity.IngressRouteDefinition) { definition.Namespace = "kube-system" },
			"名前":        func(definition *activity.IngressRouteDefinition) { definition.Name = "evil" },
			"ホスト名":      func(definition *activity.IngressRouteDefinition) { definition.Host = "a`) || Host(`b" },
		} {
			operator, fakeClient := newTestIngressOperator(subTest)
			definition := newTestIngressDefinition()
			mutate(&definition)
			if applyError := operator.ApplyIngressRoute(ctx, definition); applyError == nil {
				subTest.Errorf("%s: 拒否するはず", name)
			}
			if len(fakeClient.Actions()) != 0 {
				subTest.Errorf("%s: k8s に触れないはず: %v", name, fakeClient.Actions())
			}
		}
		operator, _ := newTestIngressOperator(subTest)
		definition := newTestIngressDefinition()
		definition.Name = "evil"
		if applyError := operator.ApplyIngressRoute(ctx, definition); !errors.Is(applyError, activity.ErrInvalidIngressName) {
			subTest.Errorf("名前の不正は ErrInvalidIngressName のはず: %v", applyError)
		}
	})
	testingT.Run("k8s のエラーは、包んで返す", func(subTest *testing.T) {
		operator, fakeClient := newTestIngressOperator(subTest)
		fakeClient.PrependReactor("create", "ingressroutes", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("api down")
		})
		if applyError := operator.ApplyIngressRoute(ctx, newTestIngressDefinition()); applyError == nil {
			subTest.Error("エラーになるはず")
		}
	})
}

// TestKubernetesIngressOperatorDeleteAndSettings は、IngressRoute の削除(冪等、不正な名前の拒否)と、不正な apiVersion の拒否を確認する。
func TestKubernetesIngressOperatorDeleteAndSettings(testingT *testing.T) {
	ctx := context.Background()
	operator, fakeClient := newTestIngressOperator(testingT)
	_ = operator.ApplyIngressRoute(ctx, newTestIngressDefinition())
	// 削除でき、もう一度削除しても成功する。
	if deleteError := operator.DeleteIngressRoute(ctx, validNamespaceName, testIngressName); deleteError != nil {
		testingT.Fatalf("削除できるはず: %v", deleteError)
	}
	if _, getError := fakeClient.Resource(testIngressRouteResource).Namespace(validNamespaceName).Get(ctx, testIngressName, metav1.GetOptions{}); !apierrors.IsNotFound(getError) {
		testingT.Errorf("消えているはず: %v", getError)
	}
	if again := operator.DeleteIngressRoute(ctx, validNamespaceName, testIngressName); again != nil {
		testingT.Errorf("無くても成功するはず: %v", again)
	}
	// 不正な namespace と名前は、拒否する。
	if operator.DeleteIngressRoute(ctx, "default", testIngressName) == nil || operator.DeleteIngressRoute(ctx, validNamespaceName, "x") == nil {
		testingT.Error("不正な値は拒否するはず")
	}
	// group/version の形でない apiVersion は、作れない。
	for _, invalid := range []string{"v1alpha1", "", "/v1", "traefik.io/"} {
		if _, createError := NewKubernetesIngressOperator(fakeClient, IngressRouteSettings{APIVersion: invalid, EntryPoints: []string{"web"}}); createError == nil {
			testingT.Errorf("%q は拒否するはず", invalid)
		}
	}
}
