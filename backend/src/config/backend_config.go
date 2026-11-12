// Package config は、バックエンドの設定を環境変数から読む。
//
// 環境変数を読む部品は shared/config にあり、このパッケージは、
// バックエンドが必要とする環境変数の名前と、既定値を決める。
package config

import (
	"errors"
	"fmt"
	"log/slog"

	sharedconfig "launchs/shared/config"
	"launchs/shared/logger"
)

const (
	// defaultHTTPPort は、HTTP_PORT が設定されていないときに使うポート番号。
	defaultHTTPPort = 8080

	// defaultLogLevelText は、LOG_LEVEL が設定されていないときに使うログレベル。
	defaultLogLevelText = "info"

	// minimumPort と maximumPort は、ポート番号として使える範囲。
	minimumPort = 1
	maximumPort = 65535
)

// BackendConfig は、バックエンドの設定。
type BackendConfig struct {
	// HTTPPort は、API サーバーが待ち受けるポート番号(環境変数 HTTP_PORT)。
	HTTPPort int

	// LogLevel は、出力するログの最低のレベル(環境変数 LOG_LEVEL)。
	LogLevel slog.Level
}

// LoadBackendConfig は、環境変数からバックエンドの設定を読む。
//
// 読む環境変数:
//   - HTTP_PORT: 待ち受けるポート番号(1〜65535)。省略すると 8080。
//   - LOG_LEVEL: ログレベル(debug、info、warn、error)。省略すると info。
//
// 設定に問題があっても止まらず、見つかった全ての問題をまとめて報告する。
//
// 引数:
//   - reader: 環境変数を読む EnvironmentReader。
//
// 戻り値:
//   - 読み込んだ設定。問題があったときは、ゼロ値。
//   - エラー。問題があったとき、全ての問題の説明を含む。
func LoadBackendConfig(reader *sharedconfig.EnvironmentReader) (BackendConfig, error) {
	httpPort := reader.OptionalInt("HTTP_PORT", defaultHTTPPort)
	logLevelText := reader.OptionalString("LOG_LEVEL", defaultLogLevelText)

	loadError := reader.Err()

	if httpPort < minimumPort || httpPort > maximumPort {
		portError := fmt.Errorf("環境変数 HTTP_PORT は %d〜%d でなければならない: %d", minimumPort, maximumPort, httpPort)
		loadError = errors.Join(loadError, portError)
	}

	logLevel, levelError := logger.ParseLevel(logLevelText)
	if levelError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 LOG_LEVEL: %w", levelError))
	}

	if loadError != nil {
		return BackendConfig{}, loadError
	}
	return BackendConfig{HTTPPort: httpPort, LogLevel: logLevel}, nil
}
