package cluster

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// isolationPolicyName は、プロジェクトの namespace に作る、隔離の NetworkPolicy の名前。
	isolationPolicyName = "launchs-isolation"

	// namespaceNameLabel は、k8s が全ての namespace に自動で付ける、namespace の名前のラベル。
	namespaceNameLabel = "kubernetes.io/metadata.name"

	// dnsNamespace と dnsPodLabelName、dnsPodLabelValue は、クラスターの DNS(CoreDNS)の Pod を選ぶための値。
	dnsNamespace     = "kube-system"
	dnsPodLabelName  = "k8s-app"
	dnsPodLabelValue = "kube-dns"
	dnsPortNumber    = 53
)

// privateNetworks は、インターネットへの出口の許可から除く、内部のネットワーク。
//
// ノードの IP(例 192.168.100.x)、Pod と Service の CIDR(例 10.240.0.0/16、10.112.0.0/12)、リンクローカル
// (クラウドのメタデータなど)、CGNAT(Tailscale など)を含む。これらへの通信は、止める。
var privateNetworks = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10"}

// buildIsolationPolicy は、プロジェクトの namespace を、他から隔離する NetworkPolicy を組み立てる。
//
// namespace の中の全ての Pod に、次を適用する(Cilium は、ポリシーが選んだ Pod だけを隔離するので、これが無いと、
// 全ての通信が許可される)。
//   - 入ってくる通信: 同じ namespace の Pod から。ingressNamespace があれば、その namespace(Traefik など)からも
//   - 出ていく通信: 同じ namespace の Pod、クラスターの DNS(UDP と TCP の 53)、インターネット(内部のネットワークを除く)
//
// 他のプロジェクト、システムの Pod、ノードの IP への通信は、止まる。
//
// 引数:
//   - namespaceName: 隔離する namespace(プロジェクトの ID)。
//   - ingressNamespace: 外からの通信を中継する namespace(Traefik など)。空なら、許可しない。
//
// 戻り値:
//   - 組み立てた NetworkPolicy。
func buildIsolationPolicy(namespaceName string, ingressNamespace string) *networkingv1.NetworkPolicy {
	// ----- 入ってくる通信: 同じ namespace(空の podSelector は、同じ namespace の全ての Pod) -----
	ingressPeers := []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}}
	// 外からの通信を中継する namespace があれば、そこからも許可する。
	if ingressNamespace != "" {
		ingressPeers = append(ingressPeers, networkingv1.NetworkPolicyPeer{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{namespaceNameLabel: ingressNamespace}},
		})
	}

	// ----- 出ていく通信 -----
	udp, tcp := corev1.ProtocolUDP, corev1.ProtocolTCP
	dnsPort := intstr.FromInt32(dnsPortNumber)
	egressRules := []networkingv1.NetworkPolicyEgressRule{
		// 同じ namespace の Pod。
		{To: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}}},
		// クラスターの DNS(名前の解決に必要。UDP と TCP の両方)。
		{
			To: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{namespaceNameLabel: dnsNamespace}},
				PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{dnsPodLabelName: dnsPodLabelValue}},
			}},
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: &udp, Port: &dnsPort}, {Protocol: &tcp, Port: &dnsPort}},
		},
		// インターネット(内部のネットワークを除く)。
		{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0", Except: append([]string(nil), privateNetworks...)}}}},
	}

	// 全ての Pod に、入ってくる通信と出ていく通信の両方の規則を適用する。
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: isolationPolicyName, Namespace: namespaceName, Labels: map[string]string{projectIDLabel: namespaceName}},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			Ingress:     []networkingv1.NetworkPolicyIngressRule{{From: ingressPeers}},
			Egress:      egressRules,
		},
	}
}

// ensureIsolationPolicy は、プロジェクトの namespace に、隔離の NetworkPolicy を作る。すでにあれば、内容を合わせる(冪等)。
//
// 設定(外からの通信を中継する namespace)を変えたあとや、手で書き換えられたあとも、次に呼ばれたときに、期待する内容に戻る。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: 隔離する namespace(プロジェクトの ID)。
//
// 戻り値:
//   - k8s への依頼に失敗したときのエラー。
func (operator *KubernetesDeploymentOperator) ensureIsolationPolicy(requestContext context.Context, namespaceName string) error {
	// 期待する内容を組み立てる。
	desiredPolicy := buildIsolationPolicy(namespaceName, operator.ingressNamespace)
	policies := operator.clientset.NetworkingV1().NetworkPolicies(namespaceName)

	// まず作ってみる。
	_, createError := policies.Create(requestContext, desiredPolicy, metav1.CreateOptions{})
	if createError == nil {
		return nil
	}
	// すでにあるとき以外の失敗は、原因を包んで返す。
	if !apierrors.IsAlreadyExists(createError) {
		return fmt.Errorf("NetworkPolicy %s を作れませんでした: %w", isolationPolicyName, createError)
	}

	// すでにあるときは、内容が期待と同じなら何もせず、違えば(手で変えられた、設定が変わった)期待する内容に更新する。
	existingPolicy, getError := policies.Get(requestContext, isolationPolicyName, metav1.GetOptions{})
	if getError != nil {
		return fmt.Errorf("NetworkPolicy %s を取得できませんでした: %w", isolationPolicyName, getError)
	}
	if reflect.DeepEqual(existingPolicy.Spec, desiredPolicy.Spec) {
		return nil
	}
	desiredPolicy.ResourceVersion = existingPolicy.ResourceVersion
	if _, updateError := policies.Update(requestContext, desiredPolicy, metav1.UpdateOptions{}); updateError != nil {
		return fmt.Errorf("NetworkPolicy %s を更新できませんでした: %w", isolationPolicyName, updateError)
	}
	return nil
}
