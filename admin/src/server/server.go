// Package server は、管理コンテナの Echo のサーバーを組み立てる。
package server

import (
	"log/slog"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"gorm.io/gorm"

	"launchs/admin/src/controller"
	adminmiddleware "launchs/admin/src/middleware"
	"launchs/admin/src/repository"
	"launchs/admin/src/service"
	"launchs/shared/httpmiddleware"
)

// Dependencies は、サーバーが外から受け取るもの。
type Dependencies struct {
	// Database は、app データベースの接続。
	Database *gorm.DB

	// AuthDatabase は、auth データベースの接続。
	AuthDatabase *gorm.DB

	// Settings は、画面に見せる設定(名前 → 値)。
	Settings map[string]string

	// SettingNames は、見せる設定の名前。
	SettingNames []string

	// Verifier は、管理者の access token を検証するもの。
	Verifier adminmiddleware.AccessTokenVerifier
}

// New は、管理コンテナの Echo のサーバーを組み立てる(起動は呼び出し側)。
//
// 引数:
//   - appLogger: ログの出力先。
//   - dependencies: 依存するものの一式。
//
// 戻り値:
//   - 組み立てた Echo のサーバー。
func New(appLogger *slog.Logger, dependencies Dependencies) *echo.Echo {
	// ロガーと、エラーを共通の形式にするハンドラーを設定する。
	echoServer := echo.NewWithConfig(echo.Config{Logger: appLogger, HTTPErrorHandler: httpmiddleware.NewErrorHandler(appLogger)})
	// リクエスト ID、アクセスログ、パニックからの復帰の順に、外側から登録する。
	echoServer.Use(middleware.RequestID())
	echoServer.Use(httpmiddleware.NewAccessLogMiddleware(appLogger))
	echoServer.Use(middleware.Recover())

	// 死活監視は、認証なし。
	echoServer.GET("/healthz", controller.Health)
	// 管理者だけの API。
	adminGroup := echoServer.Group("/admin-api", adminmiddleware.NewAdminAuthMiddleware(dependencies.Verifier))
	overviewController := controller.NewOverviewController(service.NewOverviewService(repository.NewOverviewRepository(dependencies.Database)))
	adminGroup.GET("/overview", overviewController.Get)
	securityService := service.NewSecurityService(repository.NewSecurityRepository(dependencies.AuthDatabase))
	queryController := controller.NewAdminQueryController(service.NewAdminQueryService(repository.NewAdminQueryRepository(dependencies.Database),
		repository.NewQuotaDefaultRepository(dependencies.Database), dependencies.Settings, dependencies.SettingNames), securityService)
	securityController := controller.NewSecurityController(securityService)
	adminGroup.GET("/security/suspicious-ips", securityController.ListSuspiciousIPs)
	adminGroup.GET("/security/blocked-ips", securityController.ListBlocked)
	adminGroup.POST("/security/blocked-ips", securityController.CreateBlock)
	adminGroup.DELETE("/security/blocked-ips/:blockId", securityController.DeleteBlock)
	adminGroup.GET("/security/audit-logs", securityController.ListAudit)
	adminGroup.GET("/deploys", queryController.ListDeploys)
	adminGroup.GET("/access-logs", queryController.ListAccessLogs)
	adminGroup.GET("/quota-defaults", queryController.GetQuotaDefaults)
	adminGroup.PUT("/quota-defaults", queryController.PutQuotaDefaults)
	adminGroup.GET("/settings", queryController.GetSettings)
	return echoServer
}
