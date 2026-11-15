package activity

import (
	"context"
	"log/slog"
)

// LoggingVolumeOperator は、k8s に接続しないときの VolumeOperator(開発用)。何も作らず、ログに残すだけ。
type LoggingVolumeOperator struct {
	// appLogger は、ログの出力先。
	appLogger *slog.Logger
}

// NewLoggingVolumeOperator は、LoggingVolumeOperator を作る。
//
// 引数:
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した LoggingVolumeOperator。
func NewLoggingVolumeOperator(appLogger *slog.Logger) *LoggingVolumeOperator {
	// ログの出力先を持たせる。
	return &LoggingVolumeOperator{appLogger: appLogger}
}

// EnsureVolumeClaim は、何も作らず、ログに残して成功する。
//
// 引数:
//   - definition: 作る PVC の内容。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingVolumeOperator) EnsureVolumeClaim(_ context.Context, definition VolumeClaimDefinition) error {
	// 作らなかったことを、警告で残す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、PVC は作らない(仮の実装)", "namespace", definition.Namespace, "pvc", definition.Name)
	return nil
}

// ExpandVolumeClaim は、何も拡張せず、ログに残して成功する。
//
// 引数:
//   - definition: 拡張する PVC の内容。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingVolumeOperator) ExpandVolumeClaim(_ context.Context, definition VolumeClaimDefinition, _ func()) error {
	// 拡張しなかったことを、警告で残す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、PVC は拡張しない(仮の実装)", "namespace", definition.Namespace, "pvc", definition.Name, "size_mb", definition.SizeMB)
	return nil
}

// DeleteVolumeClaim は、何も削除せず、ログに残して成功する。
//
// 引数:
//   - namespaceName: namespace の名前。
//   - claimName: PVC の名前。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingVolumeOperator) DeleteVolumeClaim(_ context.Context, namespaceName string, claimName string) error {
	// 削除しなかったことを、警告で残す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、PVC は削除しない(仮の実装)", "namespace", namespaceName, "pvc", claimName)
	return nil
}
