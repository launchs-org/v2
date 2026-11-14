// Package config は、watcher の設定を環境変数から読む。
package config

import (
	"errors"
	"fmt"
	"log/slog"

	sharedconfig "launchs/shared/config"
	"launchs/shared/k8s"
	"launchs/shared/logger"
)

// defaultLogLevelText は、LOG_LEVEL が無いときに使うログレベル。
const defaultLogLevelText = "info"

// WatcherConfig は、watcher の設定。
type WatcherConfig struct {
	// DatabaseURL は、app データベースの接続先(環境変数 DATABASE_URL)。パスワードを含むので、ログに出さない。
	DatabaseURL string

	// LogLevel は、出力するログの最低のレベル(環境変数 LOG_LEVEL)。
	LogLevel slog.Level

	// Kubernetes は、k8s への接続の設定(環境変数 KUBERNETES_ACCESS、KUBECONFIG)。読み方は shared/k8s にある。
	Kubernetes k8s.Settings
}

// LoadWatcherConfig は、環境変数から watcher の設定を読む。
//
// 読む環境変数:
//   - DATABASE_URL: app データベースの接続先(必須)。
//   - KUBERNETES_ACCESS、KUBECONFIG: k8s への接続の設定(読み方と意味は shared/k8s の ReadSettings)。
//   - LOG_LEVEL: ログレベル(debug、info、warn、error)。省略すると info。
//
// 設定に問題があっても止まらず、見つかった全ての問題をまとめて報告する。
//
// 引数:
//   - reader: 環境変数を読む EnvironmentReader。
//
// 戻り値:
//   - 読み込んだ設定。問題があったときは、ゼロ値。
//   - 問題があったときのエラー。
func LoadWatcherConfig(reader *sharedconfig.EnvironmentReader) (WatcherConfig, error) {
	// 各環境変数を読む。
	databaseURL := reader.RequiredString("DATABASE_URL")
	logLevelText := reader.OptionalString("LOG_LEVEL", defaultLogLevelText)
	kubernetesSettings := k8s.ReadSettings(reader)

	// 読み込み中に見つかった問題を集める。
	loadError := reader.Err()

	// ログレベルの文字列を変換し、未知の値なら問題に加える。
	logLevel, levelError := logger.ParseLevel(logLevelText)
	if levelError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 LOG_LEVEL: %w", levelError))
	}
	// k8s への接続の設定の値を検査する。
	if validateError := kubernetesSettings.Validate(); validateError != nil {
		loadError = errors.Join(loadError, validateError)
	}
	// 問題があったときは、まとめて返す。
	if loadError != nil {
		return WatcherConfig{}, loadError
	}
	return WatcherConfig{DatabaseURL: databaseURL, LogLevel: logLevel, Kubernetes: kubernetesSettings}, nil
}
