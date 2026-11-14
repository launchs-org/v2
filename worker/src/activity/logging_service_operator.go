package activity

import (
	"context"
	"log/slog"
)

// LoggingServiceOperator は、k8s を操作せず、ログに出すだけの ServiceOperator。
//
// KUBERNETES_ACCESS=none のときの、開発用の仮の実装。実際には Service を作らないので、
// service は、ClusterIP が無いまま active になる。本番では使わない。
type LoggingServiceOperator struct {
	appLogger *slog.Logger
}

// NewLoggingServiceOperator は、LoggingServiceOperator を作る。
//
// 引数:
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した LoggingServiceOperator。
func NewLoggingServiceOperator(appLogger *slog.Logger) *LoggingServiceOperator {
	// ロガーを持たせて返す。
	return &LoggingServiceOperator{appLogger: appLogger}
}

// ApplyService は、何も作らず、警告のログを出すだけ。ClusterIP は払い出されない(空)。
//
// 引数:
//   - requestContext: 使わない。
//   - definition: 作る予定の Service の内容。
//
// 戻り値:
//   - 空の ServiceState と nil。
func (loggingOperator *LoggingServiceOperator) ApplyService(_ context.Context, definition ServiceDefinition) (ServiceState, error) {
	// 実際には作っていないことが分かるよう、警告で出す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、Service は作らない(仮の実装)", "namespace", definition.Namespace, "service", definition.Name)
	return ServiceState{}, nil
}

// DeleteService は、何も削除せず、警告のログを出すだけ。
//
// 引数:
//   - requestContext: 使わない。
//   - namespaceName: 削除する予定の Service の namespace。
//   - serviceName: 削除する予定の Service の名前。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingServiceOperator) DeleteService(_ context.Context, namespaceName string, serviceName string) error {
	// 実際には削除していないことが分かるよう、警告で出す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、Service は削除しない(仮の実装)", "namespace", namespaceName, "service", serviceName)
	return nil
}
