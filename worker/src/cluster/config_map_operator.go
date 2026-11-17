package cluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// KubernetesConfigMapOperator は、client-go で ConfigMap を操作する ConfigMapOperator の実装。
//
// ConfigMap は、deploy のファイルの置き場で、名前は deploy の ID と同じ。プロジェクトの namespace の中に作る。
type KubernetesConfigMapOperator struct {
	// clientset は、k8s のクライアント。
	clientset kubernetes.Interface
}

// NewKubernetesConfigMapOperator は、KubernetesConfigMapOperator を作る。
//
// 引数:
//   - clientset: k8s のクライアント。
//
// 戻り値:
//   - 作成した KubernetesConfigMapOperator。
func NewKubernetesConfigMapOperator(clientset kubernetes.Interface) *KubernetesConfigMapOperator {
	// クライアントを持たせて返す。
	return &KubernetesConfigMapOperator{clientset: clientset}
}

// ApplyConfigMap は、ConfigMap を作る。すでにあれば、内容を置き換える(冪等)。
//
// 名前は、deploy の ID の形だけを受け付ける(他のものを、誤って、作ったり変えたりしないため)。
// 作る ConfigMap には、deploy と、プロジェクトのラベルを付ける。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: ConfigMap を作る namespace(プロジェクトの ID)。
//   - name: ConfigMap の名前(deploy の ID)。
//   - data: キー → 内容。
//
// 戻り値:
//   - 名前の形が不正なとき、または k8s への依頼に失敗したときのエラー。
func (operator *KubernetesConfigMapOperator) ApplyConfigMap(requestContext context.Context, namespaceName string, name string, data map[string]string) error {
	// 名前が、deploy の ID の形でなければ、拒否する。
	if !deploymentNamePattern.MatchString(name) {
		return fmt.Errorf("ConfigMap の名前が、deploy の ID の形ではありません: %q", name)
	}
	// 作る ConfigMap を組み立てる。
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespaceName, Labels: map[string]string{deployIDLabel: name, projectIDLabel: namespaceName}},
		Data:       data,
	}
	// まず作る。すでにあれば、内容を置き換える。
	_, createError := operator.clientset.CoreV1().ConfigMaps(namespaceName).Create(requestContext, desired, metav1.CreateOptions{})
	if createError == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(createError) {
		return fmt.Errorf("ConfigMap を作れませんでした: %w", createError)
	}
	// 置き換える(消えた(削除された)キーも、確実に無くなるよう、Update で全体を置き換える)。
	_, updateError := operator.clientset.CoreV1().ConfigMaps(namespaceName).Update(requestContext, desired, metav1.UpdateOptions{})
	if updateError != nil {
		return fmt.Errorf("ConfigMap を更新できませんでした: %w", updateError)
	}
	return nil
}

// DeleteConfigMap は、ConfigMap を削除する。無ければ、何もしない(冪等)。
//
// 名前は、deploy の ID の形だけを受け付ける。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: ConfigMap がある namespace(プロジェクトの ID)。
//   - name: ConfigMap の名前(deploy の ID)。
//
// 戻り値:
//   - 名前の形が不正なとき、または k8s への依頼に失敗したときのエラー。
func (operator *KubernetesConfigMapOperator) DeleteConfigMap(requestContext context.Context, namespaceName string, name string) error {
	// 名前が、deploy の ID の形でなければ、拒否する。
	if !deploymentNamePattern.MatchString(name) {
		return fmt.Errorf("ConfigMap の名前が、deploy の ID の形ではありません: %q", name)
	}
	// 削除する。無い(NotFound)ときは、成功として扱う。
	deleteError := operator.clientset.CoreV1().ConfigMaps(namespaceName).Delete(requestContext, name, metav1.DeleteOptions{})
	if deleteError != nil && !apierrors.IsNotFound(deleteError) {
		return fmt.Errorf("ConfigMap を削除できませんでした: %w", deleteError)
	}
	return nil
}
