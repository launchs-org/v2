package cluster

import (
	"fmt"
	"log/slog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"launchs/worker/src/activity"
	"launchs/worker/src/config"
)

// NewNamespaceDeleter は、設定に合わせた、namespace を削除するものを作る。
//
// 接続の方法ごとの動作:
//   - none: k8s につながず、何も削除しない(ログに警告を出すだけ。開発用)。
//   - kubeconfig: kubeconfig のファイルで接続する(開発用)。
//   - in-cluster: Pod の ServiceAccount で接続する(本番)。
//
// 引数:
//   - workerConfig: ワーカーの設定。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - namespace を削除するもの。
//   - 接続の設定を読み込めなかったときのエラー。
func NewNamespaceDeleter(workerConfig config.WorkerConfig, appLogger *slog.Logger) (activity.NamespaceDeleter, error) {
	// 接続の方法ごとに、k8s への接続の設定を作る。
	var restConfig *rest.Config
	var configError error
	switch workerConfig.KubernetesAccess {
	case config.KubernetesAccessNone:
		// つながないので、何も削除しない仮の実装を返す。起動のログでも、そうなっていることが分かるようにする。
		appLogger.Warn("KUBERNETES_ACCESS=none のため、k8s の namespace は削除されません(開発用)")
		return activity.NewLoggingNamespaceDeleter(appLogger), nil
	case config.KubernetesAccessKubeconfig:
		// kubeconfig のファイルから、接続の設定を読む。
		restConfig, configError = clientcmd.BuildConfigFromFlags("", workerConfig.KubeconfigPath)
	case config.KubernetesAccessInCluster:
		// Pod の中の ServiceAccount から、接続の設定を読む。
		restConfig, configError = rest.InClusterConfig()
	default:
		// 設定の読み込みで検査済みだが、念のため。
		return nil, fmt.Errorf("未知の k8s の接続方法です: %q", workerConfig.KubernetesAccess)
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
	return NewKubernetesNamespaceDeleter(clientset), nil
}
