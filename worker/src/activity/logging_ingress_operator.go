package activity

import (
	"context"
	"log/slog"
)

// LoggingIngressOperator は、k8s に接続しないときの IngressOperator(開発用)。何も作らず、ログに残すだけ。
type LoggingIngressOperator struct {
	// appLogger は、ログの出力先。
	appLogger *slog.Logger
}

// NewLoggingIngressOperator は、LoggingIngressOperator を作る。
//
// 引数:
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した LoggingIngressOperator。
func NewLoggingIngressOperator(appLogger *slog.Logger) *LoggingIngressOperator {
	// ログの出力先を持たせる。
	return &LoggingIngressOperator{appLogger: appLogger}
}

// ApplyIngressRoute は、何も作らず、ログに残して成功する。
//
// 引数:
//   - definition: 反映する内容。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingIngressOperator) ApplyIngressRoute(_ context.Context, definition IngressRouteDefinition) error {
	// 作らなかったことを、警告で残す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、IngressRoute は作らない(仮の実装)", "namespace", definition.Namespace, "ingress", definition.Name, "rules", len(definition.Rules))
	return nil
}

// DeleteIngressRoute は、何も削除せず、ログに残して成功する。
//
// 引数:
//   - namespaceName: namespace の名前。
//   - ingressName: IngressRoute の名前。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingIngressOperator) DeleteIngressRoute(_ context.Context, namespaceName string, ingressName string) error {
	// 削除しなかったことを、警告で残す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、IngressRoute は削除しない(仮の実装)", "namespace", namespaceName, "ingress", ingressName)
	return nil
}
