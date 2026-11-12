package server

import (
	"github.com/labstack/echo/v5"

	"launchs/backend/src/controller"
)

// registerRoutes は、バックエンドの全てのルートを Echo のサーバーに登録する。
//
// 現在は、運用のためのヘルスチェック(GET /healthz。認証不要で、/api/v1 の外)だけを登録している。
// API のエンドポイントは、/api/v1 のグループにまとめて、リソースごとにここへ足していく。
//
// 引数:
//   - echoServer: ルートを登録する Echo のサーバー。
func registerRoutes(echoServer *echo.Echo) {
	healthController := controller.NewHealthController()
	echoServer.GET("/healthz", healthController.Check)
}
