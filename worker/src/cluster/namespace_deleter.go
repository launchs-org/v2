// Package cluster は、ワーカーが k8s のクラスタを操作する部品を持つ。
package cluster

import (
	"context"
	"fmt"
	"regexp"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"launchs/shared/model"
	"launchs/worker/src/activity"
)

// projectNamespacePattern は、削除してよい namespace の名前の形("project-" + UUID v4)。
//
// バグや設定の誤りがあっても、kube-system などの、プロジェクトのものではない
// namespace を、誤って削除しないための安全装置。
var projectNamespacePattern = regexp.MustCompile(`^` + regexp.QuoteMeta(model.ProjectIDPrefix) + `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// KubernetesNamespaceDeleter は、client-go で、プロジェクトの namespace を削除する。
//
// namespace を削除すると、配下の deploy、service、ingress、PVC(データを含む)も、まとめて消える。
type KubernetesNamespaceDeleter struct {
	clientset kubernetes.Interface
}

// NewKubernetesNamespaceDeleter は、KubernetesNamespaceDeleter を作る。
//
// 引数:
//   - clientset: k8s のクライアント。
//
// 戻り値:
//   - 作成した KubernetesNamespaceDeleter。
func NewKubernetesNamespaceDeleter(clientset kubernetes.Interface) *KubernetesNamespaceDeleter {
	return &KubernetesNamespaceDeleter{clientset: clientset}
}

// DeleteNamespace は、namespace の削除を k8s に頼む。
//
// k8s の削除は非同期で、頼んだあと namespace は Terminating の状態になり、配下のリソースが
// 消えてから、namespace 自体が消える。消えたかどうかは、NamespaceExists で確かめる。
// すでに無い、またはすでに削除中のときは、成功として扱う(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: 削除する namespace の名前(プロジェクトの ID の形のものだけ許す)。
//
// 戻り値:
//   - 名前がプロジェクトの ID の形でないとき、または k8s への依頼に失敗したときのエラー。
func (deleter *KubernetesNamespaceDeleter) DeleteNamespace(requestContext context.Context, namespaceName string) error {
	// プロジェクトのものではない名前は、誤削除を防ぐため、k8s に触れる前に拒否する。
	if validateError := validateProjectNamespace(namespaceName); validateError != nil {
		return validateError
	}

	// 削除を頼む。
	deleteError := deleter.clientset.CoreV1().Namespaces().Delete(requestContext, namespaceName, metav1.DeleteOptions{})
	// すでに無いなら、目的は達成されているので成功とする。
	if apierrors.IsNotFound(deleteError) {
		return nil
	}
	// すでに削除中(Conflict)でも、消えるのを待てばよいので成功とする。
	if apierrors.IsConflict(deleteError) {
		return nil
	}
	// それ以外の失敗は、原因を包んで返す(Temporal がリトライする)。
	if deleteError != nil {
		return fmt.Errorf("namespace %s の削除を k8s に頼めませんでした: %w", namespaceName, deleteError)
	}
	return nil
}

// NamespaceExists は、namespace が(削除中のものを含めて)まだ残っているかを返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: 確かめる namespace の名前(プロジェクトの ID の形のものだけ許す)。
//
// 戻り値:
//   - 残っていれば true、無ければ false。
//   - 名前がプロジェクトの ID の形でないとき、または k8s への問い合わせに失敗したときのエラー。
func (deleter *KubernetesNamespaceDeleter) NamespaceExists(requestContext context.Context, namespaceName string) (bool, error) {
	// 削除のときと同じ名前の検査を行う。
	if validateError := validateProjectNamespace(namespaceName); validateError != nil {
		return false, validateError
	}

	// namespace を取得してみる。
	_, getError := deleter.clientset.CoreV1().Namespaces().Get(requestContext, namespaceName, metav1.GetOptions{})
	// 見つからなければ、消えている。
	if apierrors.IsNotFound(getError) {
		return false, nil
	}
	// それ以外の失敗は、原因を包んで返す。
	if getError != nil {
		return false, fmt.Errorf("namespace %s を k8s に問い合わせられませんでした: %w", namespaceName, getError)
	}
	// 取得できたので、まだ残っている。
	return true, nil
}

// validateProjectNamespace は、namespace の名前が、プロジェクトの ID の形かを確かめる。
//
// 引数:
//   - namespaceName: 確かめる名前。
//
// 戻り値:
//   - 形が違うときのエラー。
func validateProjectNamespace(namespaceName string) error {
	// プロジェクトの ID の形に一致しなければ、エラーにする。
	if !projectNamespacePattern.MatchString(namespaceName) {
		return fmt.Errorf("%w(操作を拒否しました): %q", activity.ErrInvalidNamespaceName, namespaceName)
	}
	return nil
}
