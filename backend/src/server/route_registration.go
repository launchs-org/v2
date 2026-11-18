package server

import (
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"launchs/backend/src/middlewares"
	"launchs/shared/template"
	"launchs/templates"

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
	deployLogService := service.NewDeployLogService(deployRepository, repository.NewDeployLogQueryRepository(dependencies.Database))
	serviceRepository := repository.NewServiceRepository(dependencies.Database)
	deployController := controller.NewDeployController(service.NewDeployService(deployRepository, podRepository, dependencies.DeployWorkflowStarter), deployLogService, service.NewServiceService(serviceRepository, dependencies.ServiceWorkflowStarter))
	apiGroup.POST("/deploy", deployController.Create)
	apiGroup.GET("/projects/:projectId/deploys", deployController.ListByProject)
	// テンプレート(同梱の YAML を、起動時に読み込んだもの)の一覧と取得。データベースには触らない。
	templateRegistry := dependencies.TemplateRegistry
	if templateRegistry == nil {
		loaded, loadError := template.Load(templates.Files)
		if loadError != nil {
			panic("同梱のテンプレートを読み込めません: " + loadError.Error())
		}
		templateRegistry = loaded
	}
	templateController := controller.NewTemplateController(service.NewTemplateService(templateRegistry))
	apiGroup.GET("/templates", templateController.List)
	apiGroup.GET("/templates/:templateId", templateController.Get)
	// テンプレートから部品を作る(受け付けは同期。作成は、Workflow が非同期で行い、進み具合を取れる)。
	templateApplyController := controller.NewTemplateApplyController(service.NewTemplateApplyService(
		templateRegistry, repository.NewTemplateApplyRepository(dependencies.Database), projectRepository,
		repository.NewQuotaRepository(dependencies.Database), dependencies.IngressHostGenerator, dependencies.TemplateApplyStarter))
	apiGroup.POST("/projects/:projectId/templates/:templateId/apply", templateApplyController.Apply)
	apiGroup.GET("/projects/:projectId/template-applies/:applyId", templateApplyController.Get)
	// プロジェクトの namespace の k8s の Event の一覧(watcher が、DB に保存したものを読む)。
	eventController := controller.NewProjectEventController(service.NewProjectEventService(repository.NewProjectEventRepository(dependencies.Database)))
	apiGroup.GET("/projects/:projectId/events", eventController.List)
	apiGroup.GET("/deploy/:deployId", deployController.Get)
	apiGroup.GET("/deploy/:deployId/logs", deployController.GetLogs)
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
	apiGroup.GET("/deploy/:deployId/files", deployController.GetFiles)
	apiGroup.PUT("/deploy/:deployId/files", deployController.UpdateFiles)
	apiGroup.POST("/deploy/:deployId/files/apply", deployController.ApplyFiles)
	apiGroup.POST("/deploy/:deployId/files/discard", deployController.DiscardFiles)
	apiGroup.GET("/deploy/:deployId/configmap-mounts", deployController.GetConfigMapMounts)
	apiGroup.PUT("/deploy/:deployId/configmap-mounts", deployController.UpdateConfigMapMounts)
	apiGroup.POST("/deploy/:deployId/configmap-mounts/apply", deployController.ApplyConfigMapMounts)
	apiGroup.POST("/deploy/:deployId/configmap-mounts/discard", deployController.DiscardConfigMapMounts)

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
	apiGroup.PATCH("/projects/:projectId/ingresses/:ingressId", ingressController.UpdateAccessStats)
	apiGroup.POST("/projects/:projectId/ingresses/:ingressId/apply", ingressController.ApplyRules)
	apiGroup.POST("/projects/:projectId/ingresses/:ingressId/discard", ingressController.DiscardRules)
	accessStatsController := controller.NewAccessStatsController(service.NewAccessStatsService(ingressRepository, repository.NewAccessStatsRepository(dependencies.Database)))
	apiGroup.GET("/projects/:projectId/ingresses/:ingressId/access-stats", accessStatsController.Get)
	accessLogController := controller.NewAccessLogController(service.NewAccessLogService(ingressRepository, repository.NewAccessStatsRepository(dependencies.Database)))
	apiGroup.GET("/projects/:projectId/ingresses/:ingressId/access-logs", accessLogController.List)

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
	// 管理者だけの API(access token の role が admin のときだけ通る)。ユーザーごとの上限(クォータ)の確認と設定。
	adminGroup := apiGroup.Group("/admin", middlewares.RequireAdmin)
	adminGroup.GET("/users/:userId/quota", quotaController.GetForUser)
	adminGroup.PUT("/users/:userId/quota", quotaController.PutForUser)
	apiGroup.POST("/projects/:projectId/volumes", volumeController.Create)
	apiGroup.GET("/projects/:projectId/volumes", volumeController.List)
	apiGroup.GET("/projects/:projectId/volumes/:volumeId", volumeController.Get)
	apiGroup.PATCH("/projects/:projectId/volumes/:volumeId", volumeController.Rename)
	apiGroup.DELETE("/projects/:projectId/volumes/:volumeId", volumeController.Delete)
	apiGroup.PATCH("/projects/:projectId/volumes/:volumeId/size", volumeController.UpdateSize)
	apiGroup.POST("/projects/:projectId/volumes/:volumeId/apply", volumeController.ApplySize)
	apiGroup.POST("/projects/:projectId/volumes/:volumeId/discard", volumeController.DiscardSize)

	// ConfigMap(プロジェクトで共有する設定ファイル。docs/configmap.md)。本文は、base64 や JSON のエスケープで大きくなるので、この API だけ上限を広げる。
	configMapController := controller.NewConfigMapController(service.NewConfigMapService(repository.NewConfigMapRepository(dependencies.Database)))
	configMapBodyLimit := middleware.BodyLimit(configMapRequestBodyLimitBytes)
	apiGroup.POST("/projects/:projectId/configmaps", configMapController.Create, configMapBodyLimit)
	apiGroup.GET("/projects/:projectId/configmaps", configMapController.List)
	apiGroup.GET("/configmaps/:configMapId", configMapController.Get)
	apiGroup.PUT("/configmaps/:configMapId", configMapController.Replace, configMapBodyLimit)
	apiGroup.PATCH("/configmaps/:configMapId", configMapController.Rename)
	apiGroup.DELETE("/configmaps/:configMapId", configMapController.Delete)
	apiGroup.GET("/configmaps/:configMapId/revisions", configMapController.ListRevisions)
	apiGroup.GET("/configmaps/:configMapId/usage", configMapController.Usage)
}

// configMapRequestBodyLimitBytes は、ConfigMap の作成と置き換えのリクエストの本文の上限(バイト)。
// 保存する大きさの上限は 1MiB だが、text の改行や引用符は JSON で 2 文字になるため、余裕を持たせる。
const configMapRequestBodyLimitBytes = 3 * 1024 * 1024

// defaultVolumeMaxSizeMB は、volume の大きさの上限の既定値(MB)。
const defaultVolumeMaxSizeMB = 5120
