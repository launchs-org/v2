package activity

import (
	"context"
	"log/slog"
)

// LoggingDeploymentOperator は、k8s を操作せず、ログに出すだけの DeploymentOperator。
//
// KUBERNETES_ACCESS=none のときの、開発用の仮の実装。起動を確かめずに「Ready」と答えるため、
// deploy は、実際には何も動いていなくても active になる。本番では使わない。
type LoggingDeploymentOperator struct {
	appLogger *slog.Logger
}

// NewLoggingDeploymentOperator は、LoggingDeploymentOperator を作る。
//
// 引数:
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した LoggingDeploymentOperator。
func NewLoggingDeploymentOperator(appLogger *slog.Logger) *LoggingDeploymentOperator {
	// ロガーを持たせて返す。
	return &LoggingDeploymentOperator{appLogger: appLogger}
}

// EnsureNamespace は、何も作らず、警告のログを出すだけ。
//
// 引数:
//   - requestContext: 使わない。
//   - namespaceName: 作る予定の namespace の名前。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingDeploymentOperator) EnsureNamespace(_ context.Context, namespaceName string) error {
	// 実際には作っていないことが分かるよう、警告で出す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、namespace は作らない(仮の実装)", "namespace", namespaceName)
	return nil
}

// ApplyDeployment は、何も作らず、警告のログを出すだけ。
//
// 引数:
//   - requestContext: 使わない。
//   - definition: 作る予定の Deployment の内容。
//
// 戻り値:
//   - 常に、世代 0 と nil。
func (loggingOperator *LoggingDeploymentOperator) ApplyDeployment(_ context.Context, definition DeploymentDefinition) (int64, error) {
	// 実際には作っていないことが分かるよう、警告で出す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、Deployment は作らない(仮の実装)", "namespace", definition.Namespace, "deployment", definition.Name)
	return 0, nil
}

// DeploymentReady は、仮の実装なので、常に Ready を返す。
//
// 引数:
//   - requestContext: 使わない。
//   - namespaceName: 使わない。
//   - deploymentName: 使わない。
//
// 戻り値:
//   - 常に true と nil。
func (loggingOperator *LoggingDeploymentOperator) DeploymentReady(_ context.Context, _ string, _ string) (bool, error) {
	// 何も作っていないので待つ必要は無く、常に Ready として扱う。
	return true, nil
}

// DeleteDeployment は、何も削除せず、警告のログを出すだけ。
//
// 引数:
//   - requestContext: 使わない。
//   - namespaceName: 削除する予定の Deployment の namespace。
//   - deploymentName: 削除する予定の Deployment の名前。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingDeploymentOperator) DeleteDeployment(_ context.Context, namespaceName string, deploymentName string) error {
	// 実際には削除していないことが分かるよう、警告で出す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、Deployment は削除しない(仮の実装)", "namespace", namespaceName, "deployment", deploymentName)
	return nil
}
