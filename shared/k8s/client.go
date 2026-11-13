package k8s

import (
	"errors"
	"fmt"

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
	// 接続の方法ごとに、k8s への接続の設定を作る。
	var restConfig *rest.Config
	var configError error
	switch settings.Access {
	case AccessNone:
		// つながない設定なので、クライアントは作れない。
		return nil, ErrNotConnected
	case AccessKubeconfig:
		// kubeconfig のファイルから、接続の設定を読む。
		restConfig, configError = clientcmd.BuildConfigFromFlags("", settings.KubeconfigPath)
	case AccessInCluster:
		// Pod の中の ServiceAccount から、接続の設定を読む。
		restConfig, configError = rest.InClusterConfig()
	default:
		// 設定の検査(Validate)で防がれているはずだが、念のため。
		return nil, fmt.Errorf("未知の k8s の接続方法です: %q", settings.Access)
	}
	// 接続の設定を読めなかったときは、原因を包んで返す。
	if configError != nil {
		return nil, fmt.Errorf("k8s の接続の設定を読み込めませんでした: %w", configError)
	}

	// 接続の設定から、k8s のクライアントを作る。
	clientset, clientsetError := kubernetes.NewForConfig(restConfig)
	if clientsetError != nil {
		return nil, fmt.Errorf("k8s のクライアントを作れませんでした: %w", clientsetError)
	}
	return clientset, nil
}
