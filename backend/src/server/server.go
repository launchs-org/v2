// Package server は、バックエンドの Echo のサーバーを組み立てる。
//
// ミドルウェアとエラーハンドラー(middlewares パッケージ)を登録し、ルーティングを登録する。
package server

import (
	"log/slog"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"launchs/backend/src/middlewares"
)

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
//
// 戻り値:
//   - 組み立てた Echo のサーバー。起動は呼び出し側が行う。
func New(appLogger *slog.Logger) *echo.Echo {
	echoServer := echo.NewWithConfig(echo.Config{
		Logger:           appLogger,
		HTTPErrorHandler: middlewares.NewErrorHandler(appLogger),
	})

	echoServer.Use(middleware.RequestID())
	echoServer.Use(middlewares.NewAccessLogMiddleware(appLogger))
	echoServer.Use(middleware.Recover())

	registerRoutes(echoServer)
	return echoServer
}
