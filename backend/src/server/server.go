// Package server は、バックエンドの Echo のサーバーを組み立てる。
//
// ミドルウェアとエラーハンドラー(middlewares パッケージ)を登録し、ルーティングを登録する。
package server

import (
	"log/slog"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"gorm.io/gorm"

	"launchs/backend/src/middlewares"
)

// Dependencies は、サーバーが外から受け取る、依存するものの一式。
type Dependencies struct {
	// Database は、app データベースの接続。
	Database *gorm.DB

	// DevUserID は、開発用の仮の認証で、全てのリクエストのユーザーとして扱う ID。空なら API は全て 401。
	DevUserID string
}

// New は、バックエンドの Echo のサーバーを組み立てる。
//
// ミドルウェアは、外側から次の順に登録する。
//  1. リクエスト ID(ヘッダ X-Request-Id が無ければ作り、レスポンスにも付ける)
//  2. アクセスログ(リクエスト ID を出すため、1 のあと)
//  3. パニックからの復帰(ハンドラーがパニックしても、500 を返してサーバーを止めない)
//
// エラーは、共通ルールの形式のレスポンスに変換する。
//
// 引数:
//   - appLogger: アクセスログ、エラーの記録、Echo 自身のログに使うロガー。
//   - dependencies: データベースなど、ルートが使うもの。
//
// 戻り値:
//   - 組み立てた Echo のサーバー。起動は呼び出し側が行う。
func New(appLogger *slog.Logger, dependencies Dependencies) *echo.Echo {
	echoServer := echo.NewWithConfig(echo.Config{
		Logger:           appLogger,
		HTTPErrorHandler: middlewares.NewErrorHandler(appLogger),
	})

	echoServer.Use(middleware.RequestID())
	echoServer.Use(middlewares.NewAccessLogMiddleware(appLogger))
	echoServer.Use(middleware.Recover())

	registerRoutes(echoServer, dependencies)
	return echoServer
}
