package activity

import (
	"context"
	"log/slog"
)

// LoggingConfigMapOperator は、k8s に接続しないときの ConfigMapOperator(開発用)。何も作らず、ログに残すだけ。
type LoggingConfigMapOperator struct {
	// appLogger は、ログの出力先。
	appLogger *slog.Logger
}

// NewLoggingConfigMapOperator は、LoggingConfigMapOperator を作る。
//
// 引数:
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した LoggingConfigMapOperator。
func NewLoggingConfigMapOperator(appLogger *slog.Logger) *LoggingConfigMapOperator {
	// ログの出力先を持たせる。
	return &LoggingConfigMapOperator{appLogger: appLogger}
}

// ApplyConfigMap は、何も作らず、ログに残して成功する。
//
// 引数:
//   - namespaceName: namespace。
//   - name: ConfigMap の名前。
//   - data: キー → 内容。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingConfigMapOperator) ApplyConfigMap(_ context.Context, namespaceName string, name string, data map[string]string) error {
	// 作らなかったことを、警告で残す(内容は、秘密を含みうるので、件数だけ)。
	loggingOperator.appLogger.Warn("k8s に未接続のため、ConfigMap は作らない(仮の実装)", "namespace", namespaceName, "configmap", name, "files", len(data))
	return nil
}

// DeleteConfigMap は、何も削除せず、ログに残して成功する。
//
// 引数:
//   - namespaceName: namespace。
//   - name: ConfigMap の名前。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingConfigMapOperator) DeleteConfigMap(_ context.Context, namespaceName string, name string) error {
	// 削除しなかったことを、警告で残す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、ConfigMap は削除しない(仮の実装)", "namespace", namespaceName, "configmap", name)
	return nil
}
