package cluster

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"launchs/worker/src/activity"
)

// testServiceName は、テスト用の Service の名前(service の ID の形)。
const testServiceName = "service-0a1b2c3d-0000-4000-8000-000000000001"

// newTestServiceDefinition は、テスト用の Service の内容を作る。
//
// 戻り値:
//   - ClusterIP で、web(8080、tcp)と game(27015、udp)の2つのポートを持つ内容。
func newTestServiceDefinition() activity.ServiceDefinition {
	return activity.ServiceDefinition{
		Namespace: validNamespaceName, Name: testServiceName, DeployID: testDeploymentName, Type: "ClusterIP",
		Ports: []activity.ServicePortDefinition{{Name: "web", Port: 8080, Protocol: "tcp"}, {Name: "game", Port: 27015, Protocol: "udp"}},
	}
}

// TestKubernetesServiceOperatorApplyService は、Service を、種類・ポート・向き先を正しく設定して作り、
// すでにあれば、変えられない値を引き継いで更新し、不正な名前を k8s に触れずに拒否することを確認する。
func TestKubernetesServiceOperatorApplyService(testingT *testing.T) {
	ctx := context.Background()

	testingT.Run("ClusterIP の Service を作り、ポート・protocol・向き先・ラベルが設定される", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		if _, applyError := NewKubernetesServiceOperator(fakeClientset).ApplyService(ctx, newTestServiceDefinition()); applyError != nil {
			subTest.Fatalf("作れるはず: %v", applyError)
		}
		service, getError := fakeClientset.CoreV1().Services(validNamespaceName).Get(ctx, testServiceName, metav1.GetOptions{})
		if getError != nil {
			subTest.Fatalf("作られているはず: %v", getError)
		}
		if service.Spec.Type != corev1.ServiceTypeClusterIP {
			subTest.Errorf("ClusterIP のはず: %v", service.Spec.Type)
		}
		if service.Spec.Selector[deployIDLabel] != testDeploymentName || len(service.Spec.Selector) != 1 {
			subTest.Errorf("deploy の ID のラベルだけで、Pod を選ぶはず: %v", service.Spec.Selector)
		}
		if service.Labels[deployIDLabel] != testDeploymentName || service.Labels[projectIDLabel] != validNamespaceName {
			subTest.Errorf("ラベルが付くはず: %v", service.Labels)
		}
		if len(service.Spec.Ports) != 2 {
			subTest.Fatalf("2つのポートのはず: %+v", service.Spec.Ports)
		}
		web, game := service.Spec.Ports[0], service.Spec.Ports[1]
		if web.Name != "web" || web.Port != 8080 || web.TargetPort.IntValue() != 8080 || web.Protocol != corev1.ProtocolTCP {
			subTest.Errorf("web は 8080 の TCP で、向き先も同じ番号のはず: %+v", web)
		}
		if game.Name != "game" || game.Port != 27015 || game.TargetPort.IntValue() != 27015 || game.Protocol != corev1.ProtocolUDP {
			subTest.Errorf("game は 27015 の UDP のはず: %+v", game)
		}
	})
	for _, serviceType := range []string{"NodePort", "LoadBalancer"} {
		testingT.Run("種類 "+serviceType+" を指定できる", func(subTest *testing.T) {
			fakeClientset := fake.NewSimpleClientset()
			definition := newTestServiceDefinition()
			definition.Type = serviceType
			if _, applyError := NewKubernetesServiceOperator(fakeClientset).ApplyService(ctx, definition); applyError != nil {
				subTest.Fatalf("作れるはず: %v", applyError)
			}
			service, _ := fakeClientset.CoreV1().Services(validNamespaceName).Get(ctx, testServiceName, metav1.GetOptions{})
			if string(service.Spec.Type) != serviceType {
				subTest.Errorf("%s のはず: %v", serviceType, service.Spec.Type)
			}
		})
	}
	testingT.Run("払い出された ClusterIP を返す。None のときは空", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		fakeClientset.PrependReactor("create", "services", func(action clienttesting.Action) (bool, runtime.Object, error) {
			created := action.(clienttesting.CreateAction).GetObject().(*corev1.Service).DeepCopy()
			created.Spec.ClusterIP = "10.112.0.5"
			return true, created, nil
		})
		state, applyError := NewKubernetesServiceOperator(fakeClientset).ApplyService(ctx, newTestServiceDefinition())
		if applyError != nil || state.ClusterIP != "10.112.0.5" {
			subTest.Errorf("10.112.0.5 のはず: %v %+v", applyError, state)
		}
		noneClientset := fake.NewSimpleClientset()
		noneClientset.PrependReactor("create", "services", func(action clienttesting.Action) (bool, runtime.Object, error) {
			created := action.(clienttesting.CreateAction).GetObject().(*corev1.Service).DeepCopy()
			created.Spec.ClusterIP = corev1.ClusterIPNone
			return true, created, nil
		})
		if noneState, _ := NewKubernetesServiceOperator(noneClientset).ApplyService(ctx, newTestServiceDefinition()); noneState.ClusterIP != "" {
			subTest.Errorf("None は空のはず: %q", noneState.ClusterIP)
		}
	})
	testingT.Run("すでにあれば更新し、ClusterIP と割り当て済みの NodePort を引き継ぐ", func(subTest *testing.T) {
		existing := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: testServiceName, Namespace: validNamespaceName, ResourceVersion: "7"},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort, ClusterIP: "10.112.0.9", ClusterIPs: []string{"10.112.0.9"},
				Ports: []corev1.ServicePort{{Name: "web", Port: 8080, Protocol: corev1.ProtocolTCP, NodePort: 31234}},
			},
		}
		fakeClientset := fake.NewSimpleClientset(existing)
		definition := newTestServiceDefinition()
		definition.Type = "NodePort"
		state, applyError := NewKubernetesServiceOperator(fakeClientset).ApplyService(ctx, definition)
		if applyError != nil || state.ClusterIP != "10.112.0.9" {
			subTest.Fatalf("更新でき、ClusterIP は変わらないはず: %v %+v", applyError, state)
		}
		updated, _ := fakeClientset.CoreV1().Services(validNamespaceName).Get(ctx, testServiceName, metav1.GetOptions{})
		if len(updated.Spec.Ports) != 2 || updated.Spec.Ports[0].NodePort != 31234 || updated.Spec.Ports[1].NodePort != 0 {
			subTest.Errorf("同じポートの NodePort は引き継ぎ、新しいポートは割り当てに任せるはず: %+v", updated.Spec.Ports)
		}
		if updated.Spec.ClusterIP != "10.112.0.9" {
			subTest.Errorf("ClusterIP を引き継ぐはず: %q", updated.Spec.ClusterIP)
		}
	})
	testingT.Run("k8s への依頼の失敗は、エラーとして返す", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		fakeClientset.PrependReactor("create", "services", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("api down")
		})
		if _, applyError := NewKubernetesServiceOperator(fakeClientset).ApplyService(ctx, newTestServiceDefinition()); applyError == nil {
			subTest.Errorf("エラーのはず")
		}
	})
	for _, invalid := range []struct {
		name      string
		namespace string
		service   string
		expected  error
	}{
		{"プロジェクトの形でない namespace(kube-system)は拒否する", "kube-system", testServiceName, activity.ErrInvalidNamespaceName},
		{"service の形でない名前(kubernetes)は拒否する", validNamespaceName, "kubernetes", activity.ErrInvalidServiceName},
		{"空の名前は拒否する", validNamespaceName, "", activity.ErrInvalidServiceName},
	} {
		testingT.Run(invalid.name, func(subTest *testing.T) {
			fakeClientset := fake.NewSimpleClientset()
			definition := newTestServiceDefinition()
			definition.Namespace, definition.Name = invalid.namespace, invalid.service
			_, applyError := NewKubernetesServiceOperator(fakeClientset).ApplyService(ctx, definition)
			if !errors.Is(applyError, invalid.expected) || len(fakeClientset.Actions()) != 0 {
				subTest.Errorf("拒否され、k8s は呼ばれないはず: %v %v", applyError, fakeClientset.Actions())
			}
		})
	}
}

// TestKubernetesServiceOperatorDeleteService は、Service を削除でき、無くても成功し、
// プロジェクトの namespace と service の名前の形でないものは、k8s に触れずに拒否することを確認する。
func TestKubernetesServiceOperatorDeleteService(testingT *testing.T) {
	ctx := context.Background()
	testingT.Run("Service を削除できる", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		operator := NewKubernetesServiceOperator(fakeClientset)
		_, _ = operator.ApplyService(ctx, newTestServiceDefinition())
		if deleteError := operator.DeleteService(ctx, validNamespaceName, testServiceName); deleteError != nil {
			subTest.Fatalf("削除できるはず: %v", deleteError)
		}
		if _, getError := fakeClientset.CoreV1().Services(validNamespaceName).Get(ctx, testServiceName, metav1.GetOptions{}); !apierrors.IsNotFound(getError) {
			subTest.Errorf("消えているはず: %v", getError)
		}
	})
	testingT.Run("すでに無くても成功する(冪等)", func(subTest *testing.T) {
		if deleteError := NewKubernetesServiceOperator(fake.NewSimpleClientset()).DeleteService(ctx, validNamespaceName, testServiceName); deleteError != nil {
			subTest.Errorf("成功するはず: %v", deleteError)
		}
	})
	testingT.Run("k8s への依頼の失敗は、エラーとして返す", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		fakeClientset.PrependReactor("delete", "services", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("api down")
		})
		if deleteError := NewKubernetesServiceOperator(fakeClientset).DeleteService(ctx, validNamespaceName, testServiceName); deleteError == nil {
			subTest.Errorf("エラーのはず")
		}
	})
	for _, invalid := range []struct {
		name      string
		namespace string
		service   string
		expected  error
	}{
		{"プロジェクトの形でない namespace は拒否する", "kube-system", testServiceName, activity.ErrInvalidNamespaceName},
		{"service の形でない名前(kube-dns)は拒否する", validNamespaceName, "kube-dns", activity.ErrInvalidServiceName},
	} {
		testingT.Run(invalid.name, func(subTest *testing.T) {
			fakeClientset := fake.NewSimpleClientset()
			deleteError := NewKubernetesServiceOperator(fakeClientset).DeleteService(ctx, invalid.namespace, invalid.service)
			if !errors.Is(deleteError, invalid.expected) || len(fakeClientset.Actions()) != 0 {
				subTest.Errorf("拒否され、k8s は呼ばれないはず: %v %v", deleteError, fakeClientset.Actions())
			}
		})
	}
}
