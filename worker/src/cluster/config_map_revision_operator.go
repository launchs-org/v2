package cluster

import (
	"context"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// configMapRevisionLabel は、版の ConfigMap に付けるラベル。使われていない版を探して消すときの目印にする。
	configMapRevisionLabel = "launchs.io/configmap-revision"
)

// configMapRevisionNamePattern は、版の ConfigMap の名前の形("configmap-<uuid>-r<版>")。この形のものだけを、作る・消す。
var configMapRevisionNamePattern = regexp.MustCompile(`^configmap-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}-r[0-9]+$`)

// KubernetesConfigMapRevisionOperator は、client-go で版の ConfigMap を操作する ConfigMapRevisionOperator の実装。
//
// 版の ConfigMap は、immutable で作る(一度作ったら、中身を変えない)。更新しないので、RBAC の update は要らない。
type KubernetesConfigMapRevisionOperator struct {
	// clientset は、k8s のクライアント。
	clientset kubernetes.Interface
}

// NewKubernetesConfigMapRevisionOperator は、KubernetesConfigMapRevisionOperator を作る。
//
// 引数:
//   - clientset: k8s のクライアント。
//
// 戻り値:
//   - 作成した KubernetesConfigMapRevisionOperator。
func NewKubernetesConfigMapRevisionOperator(clientset kubernetes.Interface) *KubernetesConfigMapRevisionOperator {
	// クライアントを持たせて返す。
	return &KubernetesConfigMapRevisionOperator{clientset: clientset}
}

// ApplyRevisionConfigMap は、版の ConfigMap を、immutable で作る。すでにあれば、何もしない(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: ConfigMap を作る namespace(プロジェクトの ID)。
//   - name: ConfigMap の名前("<configMapId>-r<revision>"の形だけを受け付ける)。
//   - data: テキストのファイル(キー → 内容)。
//   - binaryData: バイナリのファイル(キー → バイト列)。
//
// 戻り値:
//   - 名前の形が不正なとき、または k8s への依頼に失敗したときのエラー。
func (operator *KubernetesConfigMapRevisionOperator) ApplyRevisionConfigMap(requestContext context.Context, namespaceName string, name string, data map[string]string, binaryData map[string][]byte) error {
	// 名前が、版の ConfigMap の形でなければ、拒否する(他のものを、誤って作らないため)。
	if !configMapRevisionNamePattern.MatchString(name) {
		return fmt.Errorf("ConfigMap の名前が、版の形ではありません: %q", name)
	}
	// 作る ConfigMap を組み立てる(immutable)。
	immutable := true
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespaceName, Labels: map[string]string{configMapRevisionLabel: "true", projectIDLabel: namespaceName}},
		Data:       data,
		BinaryData: binaryData,
		Immutable:  &immutable,
	}
	// 作る。すでにあれば、同じ版なので、そのまま使う。
	_, createError := operator.clientset.CoreV1().ConfigMaps(namespaceName).Create(requestContext, desired, metav1.CreateOptions{})
	if createError != nil && !apierrors.IsAlreadyExists(createError) {
		return fmt.Errorf("ConfigMap を作れませんでした: %w", createError)
	}
	return nil
}

// ListRevisionConfigMaps は、namespace にある、版の ConfigMap の名前を全て返す(ラベルで探す)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: namespace(プロジェクトの ID)。
//
// 戻り値:
//   - 版の ConfigMap の名前。k8s への依頼に失敗したときはエラー。
func (operator *KubernetesConfigMapRevisionOperator) ListRevisionConfigMaps(requestContext context.Context, namespaceName string) ([]string, error) {
	// ラベルで絞って一覧を取る(namespace が無いときは、空)。
	list, listError := operator.clientset.CoreV1().ConfigMaps(namespaceName).List(requestContext, metav1.ListOptions{LabelSelector: configMapRevisionLabel + "=true"})
	if apierrors.IsNotFound(listError) {
		return []string{}, nil
	}
	if listError != nil {
		return nil, fmt.Errorf("ConfigMap の一覧を取得できませんでした: %w", listError)
	}
	// 名前だけを取り出す。
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.Name)
	}
	return names, nil
}

// DeleteRevisionConfigMap は、版の ConfigMap を削除する。無ければ、何もしない(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: namespace(プロジェクトの ID)。
//   - name: ConfigMap の名前(版の形だけを受け付ける)。
//
// 戻り値:
//   - 名前の形が不正なとき、または k8s への依頼に失敗したときのエラー。
func (operator *KubernetesConfigMapRevisionOperator) DeleteRevisionConfigMap(requestContext context.Context, namespaceName string, name string) error {
	// 名前が、版の ConfigMap の形でなければ、拒否する(他のものを、誤って消さないため)。
	if !configMapRevisionNamePattern.MatchString(name) {
		return fmt.Errorf("ConfigMap の名前が、版の形ではありません: %q", name)
	}
	// 削除する。無い(NotFound)ときは、成功として扱う。
	deleteError := operator.clientset.CoreV1().ConfigMaps(namespaceName).Delete(requestContext, name, metav1.DeleteOptions{})
	if deleteError != nil && !apierrors.IsNotFound(deleteError) {
		return fmt.Errorf("ConfigMap を削除できませんでした: %w", deleteError)
	}
	return nil
}
