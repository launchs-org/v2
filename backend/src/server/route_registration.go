package server

import (
	"github.com/labstack/echo/v5"

	"launchs/backend/src/controller"
	"launchs/backend/src/middlewares"
	"launchs/backend/src/service"
	"launchs/shared/repository"
)

// registerRoutes は、バックエンドの全てのルートを Echo のサーバーに登録する。
//
// 運用のためのヘルスチェック(GET /healthz)は、認証不要で、/api/v1 の外に置く。
// API のエンドポイントは、/api/v1 のグループにまとめる。グループには認証のミドルウェアを付け、
// リソースごとにここへ足していく。
//
// 引数:
//   - echoServer: ルートを登録する Echo のサーバー。
//   - dependencies: データベースなど、ルートが使うもの。
func registerRoutes(echoServer *echo.Echo, dependencies Dependencies) {
	healthController := controller.NewHealthController()
	echoServer.GET("/healthz", healthController.Check)

	apiGroup := echoServer.Group("/api/v1", middlewares.NewDevAuthenticationMiddleware(dependencies.DevUserID))

	projectRepository := repository.NewProjectRepository(dependencies.Database)
	projectController := controller.NewProjectController(service.NewProjectService(projectRepository))
	apiGroup.POST("/projects", projectController.Create)
	apiGroup.GET("/projects", projectController.List)
	apiGroup.GET("/projects/:projectId", projectController.Get)
	apiGroup.PATCH("/projects/:projectId", projectController.Rename)
}
