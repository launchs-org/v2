package k8s

import (
	"errors"
	"fmt"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ErrNotConnected は、接続の方法が none のため、k8s のクライアントを作れないことを表すエラー。
//
// 呼び出し側は、このエラーのときに、k8s を操作せずにログだけを出す実装に切り替える。
var ErrNotConnected = errors.New("k8s につながない設定(KUBERNETES_ACCESS=none)です")

// NewClientset は、設定に合わせて、k8s のクライアントを作る。
//
// 接続の方法ごとの動作:
//   - none: クライアントを作らず、ErrNotConnected を返す。
//   - kubeconfig: kubeconfig のファイルから接続の設定を読む(開発用)。
//   - in-cluster: Pod の ServiceAccount から接続の設定を読む(本番)。
//
// クライアントを作る時点では、クラスタへの通信はしない(最初の操作で初めてつながる)。
//
// 引数:
//   - settings: k8s への接続の設定(検査済みのもの)。
//
// 戻り値:
//   - k8s のクライアント。
//   - none のときは ErrNotConnected。それ以外は、接続の設定を読めなかったときのエラー。
func NewClientset(settings Settings) (kubernetes.Interface, error) {
	// 接続の設定を作る。
	restConfig, configError := NewRestConfig(settings)
	if configError != nil {
		return nil, configError
	}
	// クライアントを作る。
	clientset, clientsetError := kubernetes.NewForConfig(restConfig)
	if clientsetError != nil {
		return nil, fmt.Errorf("k8s のクライアントを作れませんでした: %w", clientsetError)
	}
	return clientset, nil
}

// NewRestConfig は、設定から、k8s への接続の設定を作る。
//
// 引数:
//   - settings: 接続の設定。
//
// 戻り値:
//   - 接続の設定。KUBERNETES_ACCESS=none のときは ErrNotConnected。
func NewRestConfig(settings Settings) (*rest.Config, error) {
	// 接続の方法ごとに、設定を作る。
	var restConfig *rest.Config
	var configError error
	switch settings.Access {
	case AccessNone:
		return nil, ErrNotConnected
	case AccessKubeconfig:
		restConfig, configError = clientcmd.BuildConfigFromFlags("", settings.KubeconfigPath)
	case AccessInCluster:
		restConfig, configError = rest.InClusterConfig()
	default:
		return nil, fmt.Errorf("未知の k8s の接続方法です: %q", settings.Access)
	}
	if configError != nil {
		return nil, fmt.Errorf("k8s の接続の設定を読み込めませんでした: %w", configError)
	}
	return restConfig, nil
}

// NewDynamicClient は、CRD(Traefik の IngressRoute など)を操作する、型を持たないクライアントを作る。
//
// 引数:
//   - settings: 接続の設定。
//
// 戻り値:
//   - クライアント。KUBERNETES_ACCESS=none のときは ErrNotConnected。
func NewDynamicClient(settings Settings) (dynamic.Interface, error) {
	// 接続の設定を作る。
	restConfig, configError := NewRestConfig(settings)
	if configError != nil {
		return nil, configError
	}
	// クライアントを作る。
	dynamicClient, clientError := dynamic.NewForConfig(restConfig)
	if clientError != nil {
		return nil, fmt.Errorf("k8s の動的クライアントを作れませんでした: %w", clientError)
	}
	return dynamicClient, nil
}
