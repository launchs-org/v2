package activity

import (
	"context"
	"log/slog"
)

// ConfigMapRevisionOperator は、共有の ConfigMap の「版」の k8s の ConfigMap(名前は "<configMapId>-r<revision>"。immutable)を操作する。
//
// deploy のファイルの置き場(ConfigMapOperator。名前は deploy の ID)とは別のもの。
// 実体は、client-go で操作する KubernetesConfigMapRevisionOperator(worker/src/cluster)。
// k8s につながない設定(KUBERNETES_ACCESS=none)のときは、LoggingConfigMapRevisionOperator を使う。
type ConfigMapRevisionOperator interface {
	// ApplyRevisionConfigMap は、版の ConfigMap を作る。すでにあれば何もしない(版は更新されないので、冪等)。
	// data はテキストのファイル、binaryData はバイナリのファイル(キー → バイト列)。
	ApplyRevisionConfigMap(requestContext context.Context, namespaceName string, name string, data map[string]string, binaryData map[string][]byte) error

	// ListRevisionConfigMaps は、namespace にある、版の ConfigMap の名前を全て返す。
	ListRevisionConfigMaps(requestContext context.Context, namespaceName string) ([]string, error)

	// DeleteRevisionConfigMap は、版の ConfigMap を削除する。無ければ何もしない(冪等)。
	DeleteRevisionConfigMap(requestContext context.Context, namespaceName string, name string) error
}

// LoggingConfigMapRevisionOperator は、k8s に接続しないときの ConfigMapRevisionOperator(開発用)。何も作らず、ログに残すだけ。
type LoggingConfigMapRevisionOperator struct {
	appLogger *slog.Logger
}

// NewLoggingConfigMapRevisionOperator は、LoggingConfigMapRevisionOperator を作る。
//
// 引数:
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した LoggingConfigMapRevisionOperator。
func NewLoggingConfigMapRevisionOperator(appLogger *slog.Logger) *LoggingConfigMapRevisionOperator {
	// ロガーを持たせて返す。
	return &LoggingConfigMapRevisionOperator{appLogger: appLogger}
}

// ApplyRevisionConfigMap は、何も作らず、ログに残して成功する。
//
// 引数:
//   - namespaceName: ConfigMap を作る namespace。
//   - name: ConfigMap の名前。
//   - data: テキストのファイル。
//   - binaryData: バイナリのファイル。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingConfigMapRevisionOperator) ApplyRevisionConfigMap(_ context.Context, namespaceName string, name string, data map[string]string, binaryData map[string][]byte) error {
	// 実際には作らないことを、警告で残す(ファイルの内容は、出さない)。
	loggingOperator.appLogger.Warn("k8s に未接続のため、版の ConfigMap は作らない(仮の実装)", "namespace", namespaceName, "configmap", name, "text_files", len(data), "binary_files", len(binaryData))
	return nil
}

// ListRevisionConfigMaps は、何もしないで、空を返す。
//
// 引数:
//   - namespaceName: namespace。
//
// 戻り値:
//   - 空の一覧。
func (loggingOperator *LoggingConfigMapRevisionOperator) ListRevisionConfigMaps(_ context.Context, namespaceName string) ([]string, error) {
	// 何も作っていないので、空。
	loggingOperator.appLogger.Warn("k8s に未接続のため、版の ConfigMap の一覧は空(仮の実装)", "namespace", namespaceName)
	return []string{}, nil
}

// DeleteRevisionConfigMap は、何も削除せず、ログに残して成功する。
//
// 引数:
//   - namespaceName: namespace。
//   - name: ConfigMap の名前。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingConfigMapRevisionOperator) DeleteRevisionConfigMap(_ context.Context, namespaceName string, name string) error {
	// 実際には削除しないことを、警告で残す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、版の ConfigMap は削除しない(仮の実装)", "namespace", namespaceName, "configmap", name)
	return nil
}
