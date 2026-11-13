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
	// ヘルスチェックのコントローラーを作る。
	healthController := controller.NewHealthController()
	// 認証不要のヘルスチェックを、/api/v1 の外に登録する。
	echoServer.GET("/healthz", healthController.Check)

	// /api/v1 のグループを作り、開発用の仮の認証を付ける。
	apiGroup := echoServer.Group("/api/v1", middlewares.NewDevAuthenticationMiddleware(dependencies.DevUserID))

	// データベースを使うプロジェクトのリポジトリを作る。
	projectRepository := repository.NewProjectRepository(dependencies.Database)
	// リポジトリ、サービス、コントローラーの順につなぐ。
	projectController := controller.NewProjectController(service.NewProjectService(projectRepository, dependencies.ProjectDeletionStarter))
	// プロジェクトの作成、一覧、取得、名前の変更のルートを登録する。
	apiGroup.POST("/projects", projectController.Create)
	apiGroup.GET("/projects", projectController.List)
	apiGroup.GET("/projects/:projectId", projectController.Get)
	apiGroup.PATCH("/projects/:projectId", projectController.Rename)
	apiGroup.DELETE("/projects/:projectId", projectController.Delete)

	deployRepository := repository.NewDeployRepository(dependencies.Database)
	deployController := controller.NewDeployController(service.NewDeployService(deployRepository, dependencies.DeployWorkflowStarter))
	apiGroup.POST("/deploy", deployController.Create)
	apiGroup.GET("/deploy/:deployId", deployController.Get)
	apiGroup.GET("/deploy/:deployId/config", deployController.GetConfig)
	apiGroup.PUT("/deploy/:deployId/config", deployController.UpdateConfig)
	apiGroup.POST("/deploy/:deployId/config/discard", deployController.DiscardConfig)
}
