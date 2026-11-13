// Package config は、ワーカーの設定を環境変数から読む。
package config

import (
	"errors"
	"fmt"
	"log/slog"

	sharedconfig "launchs/shared/config"
	"launchs/shared/logger"
)

const (
	// defaultTemporalAddress は、TEMPORAL_ADDRESS が無いときに使う Temporal サーバーの接続先。
	defaultTemporalAddress = "temporal:7233"

	// defaultTemporalNamespace は、TEMPORAL_NAMESPACE が無いときに使う Temporal の namespace。
	defaultTemporalNamespace = "default"

	// defaultLogLevelText は、LOG_LEVEL が無いときに使うログレベル。
	defaultLogLevelText = "info"
)

// WorkerConfig は、ワーカーの設定。
type WorkerConfig struct {
	// DatabaseURL は、app データベースの接続先(環境変数 DATABASE_URL)。パスワードを含むので、ログに出さない。
	DatabaseURL string

	// TemporalAddress は、Temporal サーバーの接続先(環境変数 TEMPORAL_ADDRESS)。
	TemporalAddress string

	// TemporalNamespace は、使う Temporal の namespace(環境変数 TEMPORAL_NAMESPACE)。
	TemporalNamespace string

	// LogLevel は、出力するログの最低のレベル(環境変数 LOG_LEVEL)。
	LogLevel slog.Level
}

// LoadWorkerConfig は、環境変数からワーカーの設定を読む。
//
// 読む環境変数:
//   - DATABASE_URL: app データベースの接続先(必須)。
//   - TEMPORAL_ADDRESS: Temporal サーバーの接続先。省略すると temporal:7233。
//   - TEMPORAL_NAMESPACE: Temporal の namespace。省略すると default。
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
func LoadWorkerConfig(reader *sharedconfig.EnvironmentReader) (WorkerConfig, error) {
	// 各環境変数を読む。
	databaseURL := reader.RequiredString("DATABASE_URL")
	temporalAddress := reader.OptionalString("TEMPORAL_ADDRESS", defaultTemporalAddress)
	temporalNamespace := reader.OptionalString("TEMPORAL_NAMESPACE", defaultTemporalNamespace)
	logLevelText := reader.OptionalString("LOG_LEVEL", defaultLogLevelText)

	// 読み込み中に見つかった問題を集める。
	loadError := reader.Err()

	// ログレベルの文字列を変換し、未知の値なら問題に加える。
	logLevel, levelError := logger.ParseLevel(logLevelText)
	if levelError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 LOG_LEVEL: %w", levelError))
	}

	// 問題があれば、ゼロ値の設定とエラーを返す。
	if loadError != nil {
		return WorkerConfig{}, loadError
	}
	// 問題が無ければ、設定を返す。
	return WorkerConfig{
		DatabaseURL:       databaseURL,
		TemporalAddress:   temporalAddress,
		TemporalNamespace: temporalNamespace,
		LogLevel:          logLevel,
	}, nil
}
