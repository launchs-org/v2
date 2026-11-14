package cluster

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// getIsolationPolicy は、namespace の隔離の NetworkPolicy を取得する。
//
// 引数:
//   - clientset: fake の k8s のクライアント。
//   - namespaceName: namespace の名前。
//
// 戻り値:
//   - 取得した NetworkPolicy。
//   - 取得できなかったときのエラー(無いとき)。
func getIsolationPolicy(clientset *fake.Clientset, namespaceName string) (*networkingv1.NetworkPolicy, error) {
	// 名前で取得する。
	return clientset.NetworkingV1().NetworkPolicies(namespaceName).Get(context.Background(), isolationPolicyName, metav1.GetOptions{})
}

// TestBuildIsolationPolicy は、隔離の NetworkPolicy が、入る通信・出ていく通信の規則を、期待どおりに持つことを確認する。
func TestBuildIsolationPolicy(testingT *testing.T) {
	testingT.Run("全ての Pod に、入る通信と出ていく通信の両方を適用する", func(subTest *testing.T) {
		policy := buildIsolationPolicy(validNamespaceName, "")
		if policy.Name != isolationPolicyName || policy.Namespace != validNamespaceName || len(policy.Spec.PodSelector.MatchLabels) != 0 || len(policy.Spec.PodSelector.MatchExpressions) != 0 {
			subTest.Errorf("名前と、全ての Pod を選ぶ空のセレクターのはず: %+v", policy)
		}
		if len(policy.Spec.PolicyTypes) != 2 || policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress || policy.Spec.PolicyTypes[1] != networkingv1.PolicyTypeEgress {
			subTest.Errorf("Ingress と Egress の両方のはず: %v", policy.Spec.PolicyTypes)
		}
		if policy.Labels[projectIDLabel] != validNamespaceName {
			subTest.Errorf("プロジェクトの ID のラベルが付くはず: %v", policy.Labels)
		}
	})

	testingT.Run("入る通信は、同じ namespace だけ(外の中継の namespace が無ければ)", func(subTest *testing.T) {
		policy := buildIsolationPolicy(validNamespaceName, "")
		if len(policy.Spec.Ingress) != 1 || len(policy.Spec.Ingress[0].From) != 1 {
			subTest.Fatalf("規則は1つで、相手も1つのはず: %+v", policy.Spec.Ingress)
		}
		peer := policy.Spec.Ingress[0].From[0]
		if peer.PodSelector == nil || peer.NamespaceSelector != nil || peer.IPBlock != nil {
			subTest.Errorf("同じ namespace の Pod だけのはず(namespaceSelector なし): %+v", peer)
		}
	})

	testingT.Run("外の中継の namespace(Traefik など)を指定すると、そこからの通信も許可する", func(subTest *testing.T) {
		policy := buildIsolationPolicy(validNamespaceName, "traefik")
		peers := policy.Spec.Ingress[0].From
		if len(peers) != 2 || peers[1].NamespaceSelector == nil || peers[1].NamespaceSelector.MatchLabels[namespaceNameLabel] != "traefik" || peers[1].PodSelector != nil {
			subTest.Errorf("traefik の namespace 全体を、2つ目の相手に足すはず: %+v", peers)
		}
	})

	testingT.Run("出ていく通信は、同じ namespace・DNS(UDP と TCP の 53)・インターネットだけ", func(subTest *testing.T) {
		policy := buildIsolationPolicy(validNamespaceName, "")
		if len(policy.Spec.Egress) != 3 {
			subTest.Fatalf("規則は3つのはず: %d", len(policy.Spec.Egress))
		}
		// 1. 同じ namespace。
		sameNamespace := policy.Spec.Egress[0]
		if len(sameNamespace.To) != 1 || sameNamespace.To[0].PodSelector == nil || sameNamespace.To[0].NamespaceSelector != nil || len(sameNamespace.Ports) != 0 {
			subTest.Errorf("同じ namespace の Pod の、全てのポートのはず: %+v", sameNamespace)
		}
		// 2. DNS。
		dns := policy.Spec.Egress[1]
		if len(dns.To) != 1 || dns.To[0].NamespaceSelector.MatchLabels[namespaceNameLabel] != "kube-system" || dns.To[0].PodSelector.MatchLabels["k8s-app"] != "kube-dns" {
			subTest.Errorf("kube-system の kube-dns のはず: %+v", dns.To)
		}
		if len(dns.Ports) != 2 || *dns.Ports[0].Protocol != corev1.ProtocolUDP || *dns.Ports[1].Protocol != corev1.ProtocolTCP || dns.Ports[0].Port.IntValue() != 53 || dns.Ports[1].Port.IntValue() != 53 {
			subTest.Errorf("UDP と TCP の 53 のはず: %+v", dns.Ports)
		}
		// 3. インターネット。
		internet := policy.Spec.Egress[2]
		if len(internet.To) != 1 || internet.To[0].IPBlock == nil || internet.To[0].IPBlock.CIDR != "0.0.0.0/0" || len(internet.Ports) != 0 {
			subTest.Fatalf("0.0.0.0/0 のはず: %+v", internet)
		}
		excluded := map[string]bool{}
		for _, cidr := range internet.To[0].IPBlock.Except {
			excluded[cidr] = true
		}
		for _, want := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10"} {
			if !excluded[want] {
				subTest.Errorf("%s を除くはず: %v", want, internet.To[0].IPBlock.Except)
			}
		}
	})

	testingT.Run("呼ぶたびに別のオブジェクトで、除外の一覧を共有しない(書き換えの影響が出ない)", func(subTest *testing.T) {
		first := buildIsolationPolicy(validNamespaceName, "")
		first.Spec.Egress[2].To[0].IPBlock.Except[0] = "changed"
		if second := buildIsolationPolicy(validNamespaceName, ""); second.Spec.Egress[2].To[0].IPBlock.Except[0] != "10.0.0.0/8" {
			subTest.Errorf("共有してはいけない: %v", second.Spec.Egress[2].To[0].IPBlock.Except)
		}
	})
}

// TestEnsureNamespaceAppliesIsolationPolicy は、namespace を用意するとき、隔離の NetworkPolicy も必ず作られ、
// すでにある場合や、手で変えられた場合も、期待する内容に揃うことを確認する。
func TestEnsureNamespaceAppliesIsolationPolicy(testingT *testing.T) {
	ctx := context.Background()

	testingT.Run("新しい namespace には、namespace と NetworkPolicy の両方が作られる", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		if ensureError := NewKubernetesDeploymentOperator(fakeClientset, "", "").EnsureNamespace(ctx, validNamespaceName); ensureError != nil {
			subTest.Fatalf("成功するはず: %v", ensureError)
		}
		if _, getError := getIsolationPolicy(fakeClientset, validNamespaceName); getError != nil {
			subTest.Errorf("NetworkPolicy が作られているはず: %v", getError)
		}
	})
	testingT.Run("繰り返し呼んでも、成功し、NetworkPolicy は1つのまま", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		operator := NewKubernetesDeploymentOperator(fakeClientset, "", "")
		for attempt := 0; attempt < 3; attempt++ {
			if ensureError := operator.EnsureNamespace(ctx, validNamespaceName); ensureError != nil {
				subTest.Fatalf("%d回目も成功するはず: %v", attempt+1, ensureError)
			}
		}
		policies, _ := fakeClientset.NetworkingV1().NetworkPolicies(validNamespaceName).List(ctx, metav1.ListOptions{})
		if len(policies.Items) != 1 {
			subTest.Errorf("1つのままのはず: %d", len(policies.Items))
		}
	})
	testingT.Run("NetworkPolicy が無かった既存の namespace にも、次の呼び出しで、NetworkPolicy が作られる", func(subTest *testing.T) {
		existing := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: validNamespaceName}}
		fakeClientset := fake.NewSimpleClientset(existing)
		if ensureError := NewKubernetesDeploymentOperator(fakeClientset, "", "").EnsureNamespace(ctx, validNamespaceName); ensureError != nil {
			subTest.Fatalf("成功するはず: %v", ensureError)
		}
		if _, getError := getIsolationPolicy(fakeClientset, validNamespaceName); getError != nil {
			subTest.Errorf("NetworkPolicy が作られているはず: %v", getError)
		}
	})
	testingT.Run("手で書き換えられた NetworkPolicy は、期待する内容に戻る", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		operator := NewKubernetesDeploymentOperator(fakeClientset, "", "")
		_ = operator.EnsureNamespace(ctx, validNamespaceName)
		// 全て許可する規則に書き換える(隔離が外れた状態)。
		tampered, _ := getIsolationPolicy(fakeClientset, validNamespaceName)
		tampered.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{}}
		_, _ = fakeClientset.NetworkingV1().NetworkPolicies(validNamespaceName).Update(ctx, tampered, metav1.UpdateOptions{})

		if ensureError := operator.EnsureNamespace(ctx, validNamespaceName); ensureError != nil {
			subTest.Fatalf("成功するはず: %v", ensureError)
		}
		restored, _ := getIsolationPolicy(fakeClientset, validNamespaceName)
		if len(restored.Spec.Egress) != 3 {
			subTest.Errorf("3つの規則に戻るはず: %+v", restored.Spec.Egress)
		}
	})
	testingT.Run("外の中継の namespace の設定を変えると、次の呼び出しで、NetworkPolicy に反映される", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		_ = NewKubernetesDeploymentOperator(fakeClientset, "", "").EnsureNamespace(ctx, validNamespaceName)
		before, _ := getIsolationPolicy(fakeClientset, validNamespaceName)
		if len(before.Spec.Ingress[0].From) != 1 {
			subTest.Fatalf("最初は、同じ namespace だけのはず")
		}

		if ensureError := NewKubernetesDeploymentOperator(fakeClientset, "", "traefik").EnsureNamespace(ctx, validNamespaceName); ensureError != nil {
			subTest.Fatalf("成功するはず: %v", ensureError)
		}
		after, _ := getIsolationPolicy(fakeClientset, validNamespaceName)
		if len(after.Spec.Ingress[0].From) != 2 {
			subTest.Errorf("traefik が足されるはず: %+v", after.Spec.Ingress[0].From)
		}
	})
	testingT.Run("NetworkPolicy を作れなければ、エラーを返す(リトライされる)", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		fakeClientset.PrependReactor("create", "networkpolicies", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("api down")
		})
		if ensureError := NewKubernetesDeploymentOperator(fakeClientset, "", "").EnsureNamespace(ctx, validNamespaceName); ensureError == nil {
			subTest.Errorf("エラーのはず")
		}
	})
	testingT.Run("プロジェクトの形でない namespace には、NetworkPolicy も作らず、k8s に触れない", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		if ensureError := NewKubernetesDeploymentOperator(fakeClientset, "", "").EnsureNamespace(ctx, "kube-system"); ensureError == nil || len(fakeClientset.Actions()) != 0 {
			subTest.Errorf("拒否され、k8s は呼ばれないはず: %v %v", ensureError, fakeClientset.Actions())
		}
	})
}
