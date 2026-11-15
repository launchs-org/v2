package server

import (
	"github.com/labstack/echo/v5"
	"launchs/backend/src/middlewares"

	"launchs/backend/src/controller"
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

	// 認証のミドルウェアが渡されていないときは、誰も通さない(開かれたままにしない)。
	authentication := dependencies.Authentication
	if authentication == nil {
		authentication = middlewares.NewDevAuthenticationMiddleware("")
	}
	// /api/v1 のグループを作り、認証のミドルウェアを付ける。
	apiGroup := echoServer.Group("/api/v1", authentication)

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
	podRepository := repository.NewDeployObservationRepository(dependencies.Database)
	deployController := controller.NewDeployController(service.NewDeployService(deployRepository, podRepository, dependencies.DeployWorkflowStarter))
	apiGroup.POST("/deploy", deployController.Create)
	apiGroup.GET("/deploy/:deployId", deployController.Get)
	apiGroup.DELETE("/deploy/:deployId", deployController.Delete)
	apiGroup.POST("/deploy/:deployId/scale", deployController.Scale)
	apiGroup.GET("/deploy/:deployId/config", deployController.GetConfig)
	apiGroup.PUT("/deploy/:deployId/config", deployController.UpdateConfig)
	apiGroup.POST("/deploy/:deployId/config/apply", deployController.ApplyConfig)
	apiGroup.POST("/deploy/:deployId/config/discard", deployController.DiscardConfig)
	apiGroup.GET("/deploy/:deployId/mounts", deployController.GetMounts)
	apiGroup.PUT("/deploy/:deployId/mounts", deployController.UpdateMounts)
	apiGroup.POST("/deploy/:deployId/mounts/apply", deployController.ApplyMounts)
	apiGroup.POST("/deploy/:deployId/mounts/discard", deployController.DiscardMounts)

	serviceRepository := repository.NewServiceRepository(dependencies.Database)
	serviceController := controller.NewServiceController(service.NewServiceService(serviceRepository, dependencies.ServiceWorkflowStarter))
	apiGroup.POST("/deploy/:deployId/service", serviceController.Create)
	apiGroup.GET("/deploy/:deployId/service", serviceController.Get)
	apiGroup.DELETE("/deploy/:deployId/service", serviceController.Delete)
	apiGroup.PATCH("/deploy/:deployId/service", serviceController.UpdatePorts)
	apiGroup.POST("/deploy/:deployId/service/apply", serviceController.ApplyPorts)
	apiGroup.POST("/deploy/:deployId/service/discard", serviceController.DiscardPorts)

	// ingress(プロジェクトの外向きの入り口)。
	ingressRepository := repository.NewIngressRepository(dependencies.Database)
	ingressController := controller.NewIngressController(service.NewIngressService(ingressRepository, dependencies.IngressWorkflowStarter, dependencies.IngressHostGenerator))
	apiGroup.POST("/projects/:projectId/ingresses", ingressController.Create)
	apiGroup.GET("/projects/:projectId/ingresses", ingressController.List)
	apiGroup.GET("/projects/:projectId/ingresses/:ingressId", ingressController.Get)
	apiGroup.DELETE("/projects/:projectId/ingresses/:ingressId", ingressController.Delete)
	apiGroup.PUT("/projects/:projectId/ingresses/:ingressId", ingressController.UpdateRules)
	apiGroup.POST("/projects/:projectId/ingresses/:ingressId/apply", ingressController.ApplyRules)
	apiGroup.POST("/projects/:projectId/ingresses/:ingressId/discard", ingressController.DiscardRules)

	// volume(プロジェクトの永続ストレージ)。
	volumeMaxSizeMB := dependencies.VolumeMaxSizeMB
	if volumeMaxSizeMB == 0 {
		volumeMaxSizeMB = defaultVolumeMaxSizeMB
	}
	volumeRepository := repository.NewVolumeRepository(dependencies.Database)
	volumeController := controller.NewVolumeController(service.NewVolumeService(volumeRepository, dependencies.VolumeWorkflowStarter, volumeMaxSizeMB))
	// 自分の上限(クォータ)と使用量。
	quotaController := controller.NewQuotaController(service.NewQuotaService(repository.NewQuotaRepository(dependencies.Database), volumeMaxSizeMB))
	apiGroup.GET("/quota", quotaController.Get)
	apiGroup.POST("/projects/:projectId/volumes", volumeController.Create)
	apiGroup.GET("/projects/:projectId/volumes", volumeController.List)
	apiGroup.GET("/projects/:projectId/volumes/:volumeId", volumeController.Get)
	apiGroup.PATCH("/projects/:projectId/volumes/:volumeId", volumeController.Rename)
	apiGroup.DELETE("/projects/:projectId/volumes/:volumeId", volumeController.Delete)
	apiGroup.PATCH("/projects/:projectId/volumes/:volumeId/size", volumeController.UpdateSize)
	apiGroup.POST("/projects/:projectId/volumes/:volumeId/apply", volumeController.ApplySize)
	apiGroup.POST("/projects/:projectId/volumes/:volumeId/discard", volumeController.DiscardSize)
}

// defaultVolumeMaxSizeMB は、volume の大きさの上限の既定値(MB)。
const defaultVolumeMaxSizeMB = 5120
