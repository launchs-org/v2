package server

import (
	"fmt"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"launchs/backend/src/middlewares"
	"launchs/shared/apitoken"
	"launchs/shared/template"
	"launchs/templates"
	"os"

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
	// API トークン(機械向けの JWT)。署名の秘密(環境変数 API_TOKEN_SECRET)が無いときは、機能を無効にする。
	apiTokenSigner := newAPITokenSigner()
	apiTokenRepository := repository.NewAPITokenRepository(dependencies.Database)
	// 接頭辞 lkt_ のトークンは API トークンとして、それ以外は、利用者のログインの JWT として認証する。
	authentication = middlewares.NewTokenDispatchMiddleware(authentication, apiTokenSigner, apiTokenRepository)
	// /api/v1 のグループを作り、認証のミドルウェアを付ける。
	apiGroup := echoServer.Group("/api/v1", authentication)
	// API トークンの発行・一覧・失効(ログインしたユーザーだけ。API トークンでは呼べない)。
	apiTokenController := controller.NewAPITokenController(service.NewAPITokenService(apiTokenRepository, apiTokenSigner))
	apiGroup.POST("/api-tokens", apiTokenController.Create)
	apiGroup.GET("/api-tokens", apiTokenController.List)
	apiGroup.DELETE("/api-tokens/:tokenId", apiTokenController.Revoke)
	apiGroup.GET("/api-tokens/:tokenId/reveal", apiTokenController.Reveal)

	// データベースを使うプロジェクトのリポジトリを作る。
	projectRepository := repository.NewProjectRepository(dependencies.Database)
	// リポジトリ、サービス、コントローラーの順につなぐ。
	projectController := controller.NewProjectController(service.NewProjectService(projectRepository, dependencies.ProjectDeletionStarter).WithTaskRecorder(repository.NewProjectTaskRepository(dependencies.Database)))
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
	deployService := service.NewDeployService(deployRepository, podRepository, dependencies.DeployWorkflowStarter)
	deployController := controller.NewDeployController(deployService, deployLogService, service.NewServiceService(serviceRepository, dependencies.ServiceWorkflowStarter))
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
	apiGroup.PUT("/deploy/:deployId/image", deployController.UpdateImage)
	// 自動デプロイの webhook(/api/v1 の外。トークンはヘッダーで渡し、webhook の中で確かめる)。
	echoServer.POST("/hooks/deploy", controller.NewWebhookController(deployService, apiTokenSigner, apiTokenRepository).DeployWebhook)
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

	serviceService := service.NewServiceService(serviceRepository, dependencies.ServiceWorkflowStarter)
	serviceController := controller.NewServiceController(serviceService)
	apiGroup.POST("/deploy/:deployId/service", serviceController.Create)
	apiGroup.GET("/deploy/:deployId/service", serviceController.Get)
	apiGroup.DELETE("/deploy/:deployId/service", serviceController.Delete)
	apiGroup.PATCH("/deploy/:deployId/service", serviceController.UpdatePorts)
	apiGroup.POST("/deploy/:deployId/service/apply", serviceController.ApplyPorts)
	apiGroup.POST("/deploy/:deployId/service/discard", serviceController.DiscardPorts)

	// ingress(プロジェクトの外向きの入り口)。
	ingressRepository := repository.NewIngressRepository(dependencies.Database)
	ingressService := service.NewIngressService(ingressRepository, dependencies.IngressWorkflowStarter, dependencies.IngressHostGenerator)
	ingressController := controller.NewIngressController(ingressService)
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

	// プロジェクトのタスク(Workflow の履歴。worker が記録する)。
	projectTaskController := controller.NewProjectTaskController(service.NewProjectTaskService(repository.NewProjectTaskRepository(dependencies.Database)).WithActivityObserver(dependencies.ActivityObserver))
	apiGroup.GET("/tasks", projectTaskController.ListAll)
	apiGroup.GET("/projects/:projectId/tasks", projectTaskController.List)
	apiGroup.GET("/projects/:projectId/tasks/:taskId", projectTaskController.Get)

	// volume(プロジェクトの永続ストレージ)。
	volumeMaxSizeMB := dependencies.VolumeMaxSizeMB
	if volumeMaxSizeMB == 0 {
		volumeMaxSizeMB = defaultVolumeMaxSizeMB
	}
	volumeRepository := repository.NewVolumeRepository(dependencies.Database)
	volumeService := service.NewVolumeService(volumeRepository, dependencies.VolumeWorkflowStarter, volumeMaxSizeMB)
	volumeController := controller.NewVolumeController(volumeService)

	// タグ(deploy、volume、service、ingress に付ける。タグ単位の一斉削除)と、メトリクス(deploy・プロジェクト・タグごとの CPU やメモリ)。
	tagController := controller.NewDeployTagController(
		service.NewDeployTagService(repository.NewDeployTagRepository(dependencies.Database), service.TagDeleters{Deploy: deployService, Service: serviceService, Volume: volumeService, Ingress: ingressService}),
		service.NewDeployMetricService(repository.NewDeployMetricRepository(dependencies.Database), deployRepository))
	apiGroup.PUT("/deploy/:deployId/tags", tagController.SetTags)
	apiGroup.PUT("/deploy/:deployId/service/tags", tagController.SetServiceTags)
	apiGroup.PUT("/projects/:projectId/volumes/:volumeId/tags", tagController.SetVolumeTags)
	apiGroup.PUT("/projects/:projectId/ingresses/:ingressId/tags", tagController.SetIngressTags)
	apiGroup.GET("/deploy/:deployId/metrics", tagController.DeployMetrics)
	apiGroup.GET("/projects/:projectId/tags", tagController.ListTags)
	apiGroup.DELETE("/projects/:projectId/tags/:tag/deploys", tagController.DeleteByTag)
	apiGroup.GET("/projects/:projectId/metrics", tagController.ProjectMetrics)
	apiGroup.GET("/projects/:projectId/tags/:tag/metrics", tagController.TagMetrics)
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

// newAPITokenSigner は、環境変数 API_TOKEN_SECRET から、API トークンの署名を作る。
//
// 未設定、または短すぎるときは nil(API トークンの機能は無効になる)。
//
// 戻り値:
//   - 署名・検証するもの。無効なら nil。
func newAPITokenSigner() *apitoken.Signer {
	// 秘密を読む(空なら、機能を使わない)。
	secret := os.Getenv("API_TOKEN_SECRET")
	if secret == "" {
		return nil
	}
	signer, signerError := apitoken.NewSigner([]byte(secret), nil)
	if signerError != nil {
		// 短い秘密のまま動かさない(気づけるよう、標準エラー出力に書く)。
		fmt.Fprintln(os.Stderr, "API_TOKEN_SECRET が使えないため、API トークンを無効にした:", signerError)
		return nil
	}
	return signer
}
