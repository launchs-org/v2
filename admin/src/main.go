// 管理コンテナ(admin)のエントリポイント。管理者だけが使う API(/admin-api)を提供する。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"

	"launchs/admin/src/config"
	"launchs/admin/src/server"
	sharedconfig "launchs/shared/config"
	"launchs/shared/database"
	"launchs/shared/jwtverify"
	"launchs/shared/logger"
)

// gracefulShutdownTimeout は、停止の合図を受けてから、処理中のリクエストを待つ時間。
const gracefulShutdownTimeout = 10 * time.Second

// jwksInitialFetchTimeout は、起動時に JWKS を取得するときに待つ時間の上限。
const jwksInitialFetchTimeout = 5 * time.Second

// main は、サーバーを動かし、失敗したときは、原因を標準エラー出力に書いて、終了コード 1 で終わる。
func main() {
	// 失敗したときだけ、エラー終了の処理に進む。
	if runError := run(); runError != nil {
		fmt.Fprintln(os.Stderr, runError)
		os.Exit(1)
	}
}

// run は、設定を読み、サーバーを起動して、停止の合図を受けるまで動かす。
//
// 戻り値:
//   - 設定や起動の失敗のエラー。正常に停止したときは nil。
func run() error {
	// 環境変数から設定を読む。
	adminConfig, configError := config.LoadAdminConfig(sharedconfig.NewProcessEnvironmentReader())
	if configError != nil {
		return fmt.Errorf("設定を読み込めませんでした: %w", configError)
	}
	appLogger := logger.New(os.Stdout, "admin", adminConfig.LogLevel)

	// データベースに接続する。
	appDatabase, databaseError := database.Open(adminConfig.DatabaseURL)
	if databaseError != nil {
		return databaseError
	}
	defer func() { _ = database.Close(appDatabase) }()
	// auth データベースに接続する(IP のブロック、監査ログ、ログイン履歴の集計)。
	authDatabase, authDatabaseError := database.Open(adminConfig.AuthDatabaseURL)
	if authDatabaseError != nil {
		return authDatabaseError
	}
	defer func() { _ = database.Close(authDatabase) }()

	// 公開鍵(JWKS)を、起動時に 1 度取得してみる(失敗しても、リクエストのときに取得し直す)。
	verifier := jwtverify.New(jwtverify.Options{URL: adminConfig.AuthJWKSURL, Logger: appLogger})
	fetchContext, cancel := context.WithTimeout(context.Background(), jwksInitialFetchTimeout)
	defer cancel()
	if fetchError := verifier.Refresh(fetchContext); fetchError != nil {
		appLogger.Warn("起動時に JWKS を取得できなかった(リクエストのときに、取得し直す)", "error", fetchError)
	}

	// サーバーを組み立てる。
	echoServer := server.New(appLogger, server.Dependencies{Database: appDatabase, AuthDatabase: authDatabase, Verifier: verifier, Settings: adminConfig.Settings, SettingNames: config.SettingNames})

	// 停止の合図で終わる context を作る。
	shutdownContext, stopListening := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopListening()
	startConfig := echo.StartConfig{Address: fmt.Sprintf(":%d", adminConfig.HTTPPort), HideBanner: true, HidePort: true, GracefulTimeout: gracefulShutdownTimeout}
	appLogger.Info("サーバーを起動する", "port", adminConfig.HTTPPort)
	if startError := startConfig.Start(shutdownContext, echoServer); startError != nil {
		return fmt.Errorf("サーバーが異常終了した: %w", startError)
	}
	appLogger.Info("サーバーを停止した")
	return nil
}
