package cluster

import (
	"context"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	"launchs/worker/src/activity"
)

// serviceNamePattern は、操作してよい Service の名前の形("service-" + UUID v4)。
var serviceNamePattern = regexp.MustCompile(`^service-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// KubernetesServiceOperator は、client-go で、k8s の Service を操作する。
type KubernetesServiceOperator struct {
	clientset kubernetes.Interface
}

// NewKubernetesServiceOperator は、KubernetesServiceOperator を作る。
//
// 引数:
//   - clientset: k8s のクライアント。
//
// 戻り値:
//   - 作成した KubernetesServiceOperator。
func NewKubernetesServiceOperator(clientset kubernetes.Interface) *KubernetesServiceOperator {
	// クライアントを持たせて返す。
	return &KubernetesServiceOperator{clientset: clientset}
}

// ApplyService は、Service を作る。すでにあれば内容を更新する(冪等)。
//
// 作るのは、プロジェクトの namespace の中の、service の ID の形の名前だけ。それ以外は、k8s に触れる前に拒否する。
// 更新のときは、変えられない値(ClusterIP など)と、割り当て済みの NodePort を、今の Service から引き継ぐ。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - definition: 作る Service の内容。
//
// 戻り値:
//   - k8s が払い出した ClusterIP などの値。
//   - 名前の形が不正なとき(activity.ErrInvalidNamespaceName、activity.ErrInvalidServiceName)、または k8s への依頼に失敗したときのエラー。
func (operator *KubernetesServiceOperator) ApplyService(requestContext context.Context, definition activity.ServiceDefinition) (activity.ServiceState, error) {
	// プロジェクトのものではない namespace には、何も作らない。
	if validateError := validateProjectNamespace(definition.Namespace); validateError != nil {
		return activity.ServiceState{}, validateError
	}
	// service のものではない名前は、作らない。
	if !serviceNamePattern.MatchString(definition.Name) {
		return activity.ServiceState{}, fmt.Errorf("%w: %q", activity.ErrInvalidServiceName, definition.Name)
	}

	// 内容から、k8s の Service を組み立てる。
	desiredService := buildService(definition)
	services := operator.clientset.CoreV1().Services(definition.Namespace)

	// まず作ってみる。作れたら、払い出された値を返す。
	created, createError := services.Create(requestContext, desiredService, metav1.CreateOptions{})
	if createError == nil {
		return activity.ServiceState{ClusterIP: clusterIPOf(created)}, nil
	}
	// すでにあるとき以外の失敗は、原因を包んで返す。
	if !apierrors.IsAlreadyExists(createError) {
		return activity.ServiceState{}, fmt.Errorf("Service %s を作れませんでした: %w", definition.Name, createError)
	}

	// すでにあるときは、今の Service を読んで、変えられない値を引き継いで更新する。
	existing, getError := services.Get(requestContext, definition.Name, metav1.GetOptions{})
	if getError != nil {
		return activity.ServiceState{}, fmt.Errorf("Service %s を取得できませんでした: %w", definition.Name, getError)
	}
	carryOverImmutableFields(existing, desiredService)
	updated, updateError := services.Update(requestContext, desiredService, metav1.UpdateOptions{})
	if updateError != nil {
		return activity.ServiceState{}, fmt.Errorf("Service %s を更新できませんでした: %w", definition.Name, updateError)
	}
	return activity.ServiceState{ClusterIP: clusterIPOf(updated)}, nil
}

// DeleteService は、Service を削除する。無ければ何もしない(冪等)。
//
// 消すのは、プロジェクトの namespace の中の、service の ID の形の名前だけ。それ以外は、k8s に触れる前に拒否する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: Service の namespace(プロジェクトの ID)。
//   - serviceName: Service の名前(service の ID)。
//
// 戻り値:
//   - 名前の形が不正なとき、または k8s への依頼に失敗したときのエラー。
func (operator *KubernetesServiceOperator) DeleteService(requestContext context.Context, namespaceName string, serviceName string) error {
	// プロジェクトのものではない namespace からは、何も消さない。
	if validateError := validateProjectNamespace(namespaceName); validateError != nil {
		return validateError
	}
	// service のものではない名前は、消さない。
	if !serviceNamePattern.MatchString(serviceName) {
		return fmt.Errorf("%w: %q", activity.ErrInvalidServiceName, serviceName)
	}
	// 削除する。すでに無いときは、削除済みとして成功にする。
	deleteError := operator.clientset.CoreV1().Services(namespaceName).Delete(requestContext, serviceName, metav1.DeleteOptions{})
	if deleteError != nil && !apierrors.IsNotFound(deleteError) {
		return fmt.Errorf("Service %s を削除できませんでした: %w", serviceName, deleteError)
	}
	return nil
}

// buildService は、内容から、k8s の Service を組み立てる。
//
// Service は、deploy の ID のラベルを持つ Pod に、通信を振り分ける。Service のポートと、Pod のポートは、同じ番号にする。
//
// 引数:
//   - definition: 作る Service の内容。
//
// 戻り値:
//   - k8s の Service。
func buildService(definition activity.ServiceDefinition) *corev1.Service {
	// ポートを、k8s の形にする(protocol は、大文字の TCP・UDP にする)。
	ports := make([]corev1.ServicePort, 0, len(definition.Ports))
	for _, port := range definition.Ports {
		protocol := corev1.ProtocolTCP
		if port.Protocol == "udp" {
			protocol = corev1.ProtocolUDP
		}
		ports = append(ports, corev1.ServicePort{Name: port.Name, Port: port.Port, TargetPort: intstr.FromInt32(port.Port), Protocol: protocol})
	}
	// ラベルと、向き先の Pod の選び方(deploy の ID のラベル)を設定する。
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: definition.Name, Namespace: definition.Namespace,
			Labels: map[string]string{deployIDLabel: definition.DeployID, projectIDLabel: definition.Namespace},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceType(definition.Type),
			Selector: map[string]string{deployIDLabel: definition.DeployID},
			Ports:    ports,
		},
	}
}

// carryOverImmutableFields は、更新する Service に、今の Service の、変えられない値(ClusterIP など)と、
// 割り当て済みの NodePort を引き継ぐ。
//
// ClusterIP は、更新で変えられない。NodePort は、指定しないと、新しく割り当て直されて、変わってしまう。
//
// 引数:
//   - existing: 今の Service。
//   - desired: 更新する Service(この中身を書き換える)。
func carryOverImmutableFields(existing *corev1.Service, desired *corev1.Service) {
	// 更新に必要なバージョンと、変えられない値を引き継ぐ。
	desired.ResourceVersion = existing.ResourceVersion
	desired.Spec.ClusterIP = existing.Spec.ClusterIP
	desired.Spec.ClusterIPs = existing.Spec.ClusterIPs
	desired.Spec.IPFamilies = existing.Spec.IPFamilies
	desired.Spec.IPFamilyPolicy = existing.Spec.IPFamilyPolicy
	// 同じ名前・番号・protocol のポートには、割り当て済みの NodePort を引き継ぐ。
	for index := range desired.Spec.Ports {
		for _, existingPort := range existing.Spec.Ports {
			if existingPort.Name == desired.Spec.Ports[index].Name && existingPort.Port == desired.Spec.Ports[index].Port &&
				existingPort.Protocol == desired.Spec.Ports[index].Protocol {
				desired.Spec.Ports[index].NodePort = existingPort.NodePort
			}
		}
	}
}

// clusterIPOf は、Service の ClusterIP を返す。払い出されない(None、または空)ときは、空文字列を返す。
//
// 引数:
//   - service: k8s の Service。
//
// 戻り値:
//   - ClusterIP。無いときは空。
func clusterIPOf(service *corev1.Service) string {
	// None は、払い出されないことを表す値なので、空として扱う。
	if service.Spec.ClusterIP == corev1.ClusterIPNone {
		return ""
	}
	return service.Spec.ClusterIP
}
