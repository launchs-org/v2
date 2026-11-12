// バックエンドの API サーバーのエントリポイント。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/config"
	"launchs/backend/src/server"
	sharedconfig "launchs/shared/config"
	"launchs/shared/logger"
)

// gracefulShutdownTimeout は、停止の合図を受けてから、処理中のリクエストが終わるのを待つ時間。
const gracefulShutdownTimeout = 10 * time.Second

// main は、サーバーを起動し、失敗したときは、原因を標準エラー出力に書いて、終了コード 1 で終わる。
func main() {
	if runError := run(); runError != nil {
		fmt.Fprintln(os.Stderr, runError)
		os.Exit(1)
	}
}

// run は、設定を読み、サーバーを起動して、停止の合図(SIGINT、SIGTERM)を受けるまで動かす。
//
// 停止の合図を受けたら、処理中のリクエストが終わるのを待ってから、正常に終わる。
//
// 戻り値:
//   - 設定の読み込みやサーバーの起動に失敗したときのエラー。正常に停止したときは nil。
func run() error {
	backendConfig, configError := config.LoadBackendConfig(sharedconfig.NewProcessEnvironmentReader())
	if configError != nil {
		return fmt.Errorf("設定を読み込めなかった: %w", configError)
	}

	appLogger := logger.New(os.Stdout, "backend", backendConfig.LogLevel)
	echoServer := server.New(appLogger)

	shutdownContext, stopListening := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopListening()

	startConfig := echo.StartConfig{
		Address:         fmt.Sprintf(":%d", backendConfig.HTTPPort),
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: gracefulShutdownTimeout,
	}

	appLogger.Info("サーバーを起動する", "port", backendConfig.HTTPPort)
	if startError := startConfig.Start(shutdownContext, echoServer); startError != nil {
		return fmt.Errorf("サーバーが異常終了した: %w", startError)
	}

	appLogger.Info("サーバーを停止した")
	return nil
}
