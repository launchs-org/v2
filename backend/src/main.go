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
	"launchs/shared/database"
	"launchs/shared/logger"
)

// gracefulShutdownTimeout は、停止の合図を受けてから、処理中のリクエストが終わるのを待つ時間。
const gracefulShutdownTimeout = 10 * time.Second

// main は、サーバーを起動し、失敗したときは、原因を標準エラー出力に書いて、終了コード 1 で終わる。
func main() {
	// サーバーを動かし、失敗して戻ってきたときだけ、エラー終了の処理に進む。
	if runError := run(); runError != nil {
		// 原因を標準エラー出力に書き、利用者が失敗に気づけるようにする。
		fmt.Fprintln(os.Stderr, runError)
		// 終了コード 1 で終わり、起動の失敗を呼び出し元(コンテナなど)に伝える。
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
	// 環境変数から設定を読む。読み方は shared の部品に任せ、名前と既定値はこのバックエンドの config が決める。
	backendConfig, configError := config.LoadBackendConfig(sharedconfig.NewProcessEnvironmentReader())
	// 設定に問題があるときは、サーバーを起動せず、全ての問題を含むエラーを返す。
	if configError != nil {
		// 呼び出し側で原因を表示できるよう、元のエラーを %w で包む。
		return fmt.Errorf("設定を読み込めませんでした: %w", configError)
	}

	// 設定したログレベルで、標準出力に JSON を出すロガーを作る。
	appLogger := logger.New(os.Stdout, "backend", backendConfig.LogLevel)

	// データベースに接続する。接続できない状態でサーバーを起動しても、全ての API が失敗するだけなので、先に確認する。
	appDatabase, databaseError := database.Open(backendConfig.DatabaseURL)
	// 接続に失敗したときは、そのエラーをそのまま返して終わる。
	if databaseError != nil {
		// 呼び出し側が原因を表示できるよう、エラーを返す。
		return databaseError
	}
	// run が終わるときに、データベースの接続を閉じる。閉じるときのエラーは、できることが無いので捨てる。
	defer func() { _ = database.Close(appDatabase) }()

	// ロガーとデータベースと開発用ユーザー ID を渡して、ルートとミドルウェアを組み立てたサーバーを作る。
	echoServer := server.New(appLogger, server.Dependencies{Database: appDatabase, DevUserID: backendConfig.DevUserID})

	// SIGINT と SIGTERM を受けると終わる context を作る。これを使って、停止の合図で、サーバーを止める。
	shutdownContext, stopListening := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// run が終わるときに、シグナルの監視を止めて、資源を解放する。
	defer stopListening()

	// 待ち受けるアドレスと、起動時の表示、停止時に待つ時間を決める。バナーとポートの表示は、ログに任せるので消す。
	startConfig := echo.StartConfig{
		Address:         fmt.Sprintf(":%d", backendConfig.HTTPPort),
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: gracefulShutdownTimeout,
	}

	// 起動することを、使うポート番号と一緒に記録する。
	appLogger.Info("サーバーを起動する", "port", backendConfig.HTTPPort)
	// 停止の合図があるまで、サーバーを動かす。正常に停止したときは、エラーにならない。
	if startError := startConfig.Start(shutdownContext, echoServer); startError != nil {
		// 異常終了の原因を、%w で包んで返す。
		return fmt.Errorf("サーバーが異常終了した: %w", startError)
	}

	// 正常に停止したことを記録する。
	appLogger.Info("サーバーを停止した")
	// 正常に停止したので、エラーは無い。
	return nil
}
