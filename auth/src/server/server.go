// Package server は、認証コンテナの Echo のサーバーを組み立てる。
package server

import (
	"log/slog"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"launchs/auth/src/clientip"
	"launchs/auth/src/controller"
	"launchs/auth/src/keys"
	"launchs/shared/httpmiddleware"
)

// MaxRequestBodyBytes は、リクエストのボディの大きさの上限(バイト)。
const MaxRequestBodyBytes = 16 * 1024

// noStoreMiddleware は、レスポンスに Cache-Control: no-store を付ける。
//
// 引数:
//   - next: 次のハンドラー。
//
// 戻り値:
//   - ヘッダを付けてから、次のハンドラーを呼ぶハンドラー。
func noStoreMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	// ハンドラーを返す。
	return func(echoContext *echo.Context) error {
		// 保存させないことを示して、次へ進む(エラーのレスポンスにも付く)。
		echoContext.Response().Header().Set("Cache-Control", "no-store")
		return next(echoContext)
	}
}

// New は、認証の Echo のサーバーを組み立てる。
//
// ミドルウェアは、外側から、リクエスト ID、アクセスログ、パニックからの復帰の順に登録する。エラーは、共通ルールの形式に変換する。
// 認証のエンドポイントは、`/auth` を付けたパスで受け付ける(Traefik と nginx は、パスを書き換えずに、そのまま渡す)。
//
// 引数:
//   - appLogger: アクセスログ、エラーの記録、Echo 自身のログに使うロガー。
//   - authUseCase: 認証のユースケース。
//   - adminUseCase: アカウント管理のユースケース。
//   - keyStore: 署名鍵(JWKS の配信に使う)。
//   - resolver: クライアントの IP を決めるもの。
//   - publicConfig: ブラウザに見せてよい設定(GET /auth/config)。
//
// 戻り値:
//   - 組み立てた Echo のサーバー。起動は呼び出し側が行う。
func New(appLogger *slog.Logger, authUseCase controller.AuthUseCase, adminUseCase controller.AdminUseCase, keyStore *keys.KeyStore, resolver *clientip.Resolver, publicConfig controller.PublicConfig) *echo.Echo {
	// エラーハンドラーと、ロガーを設定して、サーバーを作る。
	echoServer := echo.NewWithConfig(echo.Config{
		Logger:           appLogger,
		HTTPErrorHandler: httpmiddleware.NewErrorHandler(appLogger),
	})
	echoServer.Use(middleware.RequestID())
	echoServer.Use(httpmiddleware.NewAccessLogMiddleware(appLogger))
	echoServer.Use(middleware.Recover())
	// 巨大なボディでメモリを使わせない(認証のボディは、小さい JSON だけ)。
	echoServer.Use(middleware.BodyLimit(MaxRequestBodyBytes))

	// ルートを登録する。
	authController := controller.NewAuthController(authUseCase, keyStore, resolver, publicConfig)
	echoServer.GET("/healthz", controller.Health)
	authGroup := echoServer.Group("/auth")
	// トークンなどを含むレスポンスは、中継やブラウザに保存させない(JWKS だけ、ハンドラーで上書きして、キャッシュを許す)。
	authGroup.Use(noStoreMiddleware)
	authGroup.POST("/signup", authController.Signup)
	authGroup.POST("/login", authController.Login)
	authGroup.POST("/refresh", authController.Refresh)
	authGroup.GET("/me", authController.Me)
	authGroup.POST("/logout", authController.Logout)
	authGroup.GET("/login-history", authController.LoginHistory)
	authGroup.GET("/.well-known/jwks.json", authController.JSONWebKeys)
	authGroup.GET("/config", authController.Config)

	// アカウント管理(管理者だけ。各ハンドラーが、最初に、DB の最新の状態で、管理者であることを確かめる)。
	adminController := controller.NewAdminController(adminUseCase)
	adminGroup := authGroup.Group("/admin")
	adminGroup.GET("/users", adminController.ListUsers)
	adminGroup.GET("/users/:userId", adminController.GetUser)
	adminGroup.POST("/users/:userId/ban", adminController.BanUser())
	adminGroup.POST("/users/:userId/unban", adminController.UnbanUser())
	adminGroup.POST("/users/:userId/unlock", adminController.UnlockUser())
	adminGroup.GET("/login-events", adminController.ListLoginEvents)
	adminGroup.POST("/invitations", adminController.CreateInvitation)
	adminGroup.GET("/invitations", adminController.ListInvitations)
	adminGroup.DELETE("/invitations/:invitationId", adminController.RevokeInvitation)
	return echoServer
}
