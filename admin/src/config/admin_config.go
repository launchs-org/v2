// Package config は、管理コンテナ(admin)の設定を、環境変数から読む。
package config

import (
	"fmt"
	"log/slog"

	sharedconfig "launchs/shared/config"
)

// AdminConfig は、管理コンテナの設定。
type AdminConfig struct {
	// HTTPPort は、待ち受けるポート番号(環境変数 HTTP_PORT。既定 8080)。
	HTTPPort int

	// LogLevel は、ログの最低のレベル(環境変数 LOG_LEVEL。既定 info)。
	LogLevel slog.Level

	// DatabaseURL は、app データベースの接続先(環境変数 DATABASE_URL。必須)。
	DatabaseURL string

	// AuthJWKSURL は、auth の公開鍵(JWKS)の URL(環境変数 AUTH_JWKS_URL。必須。管理者の access token を検証する)。
	AuthJWKSURL string
}

// LoadAdminConfig は、環境変数から設定を読む。問題は、全てまとめて報告する。
//
// 引数:
//   - reader: 環境変数を読むもの。
//
// 戻り値:
//   - 読み込んだ設定と、問題があったときのエラー。
func LoadAdminConfig(reader *sharedconfig.EnvironmentReader) (AdminConfig, error) {
	// 必須と省略できる項目を読む。
	databaseURL := reader.RequiredString("DATABASE_URL")
	authJWKSURL := reader.RequiredString("AUTH_JWKS_URL")
	httpPort := reader.OptionalInt("HTTP_PORT", 8080)
	logLevelText := reader.OptionalString("LOG_LEVEL", "info")
	// ここまでの読み込みの問題があれば、まとめて返す。
	if readError := reader.Err(); readError != nil {
		return AdminConfig{}, readError
	}
	// ログレベルの文字を、レベルに直す。
	var logLevel slog.Level
	if levelError := logLevel.UnmarshalText([]byte(logLevelText)); levelError != nil {
		return AdminConfig{}, fmt.Errorf("LOG_LEVEL が不正です(debug、info、warn、error): %q", logLevelText)
	}
	// ポート番号の範囲を確かめる。
	if httpPort < 1 || httpPort > 65535 {
		return AdminConfig{}, fmt.Errorf("HTTP_PORT は 1〜65535 で指定してください: %d", httpPort)
	}
	return AdminConfig{HTTPPort: httpPort, LogLevel: logLevel, DatabaseURL: databaseURL, AuthJWKSURL: authJWKSURL}, nil
}
