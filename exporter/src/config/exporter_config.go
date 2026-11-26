// Package config は、exporter の設定を、環境変数から読み込む。
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	sharedconfig "launchs/shared/config"
	"launchs/shared/googledrive"
	"launchs/shared/logger"
	"launchs/shared/safehttp"
)

const (
	// defaultCheckInterval は、見回りの間隔の既定。
	defaultCheckInterval = 10 * time.Second

	// defaultDriveFlushRows は、Drive へ、自分の枠を待たずに書き出す、たまった行数の既定(ログの上限 10000 行の 6 割)。
	defaultDriveFlushRows = 6000
)

// ExporterConfig は、exporter の設定。
type ExporterConfig struct {
	// DatabaseURL は、app データベースの接続先(環境変数 DATABASE_URL)。パスワードを含むので、ログに出さない。
	DatabaseURL string

	// LogLevel は、出力するログの最低のレベル(環境変数 LOG_LEVEL)。
	LogLevel slog.Level

	// CredentialKey は、秘密を暗号化する鍵(環境変数 EXPORT_CREDENTIAL_KEY)。空なら、機能は無効。
	CredentialKey string

	// Google は、Google の OAuth クライアント(環境変数 GOOGLE_OAUTH_CLIENT_ID、GOOGLE_OAUTH_CLIENT_SECRET)。空なら、Drive は動かさない。
	Google googledrive.Config

	// CheckInterval は、見回りの間隔(環境変数 EXPORT_CHECK_INTERVAL。既定 10 秒)。
	CheckInterval time.Duration

	// DriveFlushRows は、Drive へ前倒しで書き出す、たまった行数(環境変数 EXPORT_DRIVE_FLUSH_ROWS。既定 6000)。
	DriveFlushRows int

	// SafeOptions は、webhook の URL と接続の検査のゆるめ方(環境変数 EXPORT_ALLOW_HTTP、EXPORT_ALLOW_PRIVATE_NETWORKS。開発用)。
	SafeOptions safehttp.Options
}

// LoadExporterConfig は、環境変数から、exporter の設定を読み込む。
//
// 設定に問題があっても止まらず、見つかった全ての問題をまとめて報告する。
//
// 引数:
//   - reader: 環境変数を読む EnvironmentReader。
//
// 戻り値:
//   - 読み込んだ設定。問題があったときは、ゼロ値。
//   - エラー。問題があったとき、全ての問題の説明を含む。
func LoadExporterConfig(reader *sharedconfig.EnvironmentReader) (ExporterConfig, error) {
	// 環境変数を読む。
	databaseURL := reader.RequiredString("DATABASE_URL")
	logLevelText := reader.OptionalString("LOG_LEVEL", "info")
	credentialKey := reader.OptionalString("EXPORT_CREDENTIAL_KEY", "")
	googleClientID := reader.OptionalString("GOOGLE_OAUTH_CLIENT_ID", "")
	googleClientSecret := reader.OptionalString("GOOGLE_OAUTH_CLIENT_SECRET", "")
	checkInterval := reader.OptionalDuration("EXPORT_CHECK_INTERVAL", defaultCheckInterval)
	driveFlushRows := reader.OptionalInt("EXPORT_DRIVE_FLUSH_ROWS", defaultDriveFlushRows)
	allowHTTP := reader.OptionalString("EXPORT_ALLOW_HTTP", "false")
	allowPrivate := reader.OptionalString("EXPORT_ALLOW_PRIVATE_NETWORKS", "false")
	loadError := reader.Err()
	// ログレベルを確かめる。
	logLevel, levelError := logger.ParseLevel(logLevelText)
	if levelError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 LOG_LEVEL: %w", levelError))
	}
	// 間隔と行数は、正の値でなければならない。
	if checkInterval <= 0 {
		loadError = errors.Join(loadError, errors.New("環境変数 EXPORT_CHECK_INTERVAL は、正の値でなければなりません"))
	}
	if driveFlushRows <= 0 {
		loadError = errors.Join(loadError, errors.New("環境変数 EXPORT_DRIVE_FLUSH_ROWS は、正の値でなければなりません"))
	}
	// 真偽の値は、true か false だけ。
	for name, value := range map[string]string{"EXPORT_ALLOW_HTTP": allowHTTP, "EXPORT_ALLOW_PRIVATE_NETWORKS": allowPrivate} {
		if value != "true" && value != "false" {
			loadError = errors.Join(loadError, fmt.Errorf("環境変数 %s は、true か false にしてください", name))
		}
	}
	if loadError != nil {
		return ExporterConfig{}, loadError
	}
	return ExporterConfig{
		DatabaseURL: databaseURL, LogLevel: logLevel, CredentialKey: credentialKey,
		Google:        googledrive.Config{ClientID: googleClientID, ClientSecret: googleClientSecret},
		CheckInterval: checkInterval, DriveFlushRows: driveFlushRows,
		SafeOptions: safehttp.Options{AllowHTTP: allowHTTP == "true", AllowPrivateNetworks: allowPrivate == "true"},
	}, nil
}
