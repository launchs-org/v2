// auth は、認証のコンテナ。ユーザーの登録・ログイン・トークンの発行と、公開鍵(JWKS)の配信を行う。
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"

	"launchs/auth/src/clientip"
	"launchs/auth/src/config"
	"launchs/auth/src/controller"
	"launchs/auth/src/keys"
	"launchs/auth/src/password"
	"launchs/auth/src/repository"
	"launchs/auth/src/server"
	"launchs/auth/src/service"
	"launchs/auth/src/token"
	"launchs/auth/src/turnstile"
	sharedconfig "launchs/shared/config"
	"launchs/shared/database"
	"launchs/shared/logger"
)

// gracefulShutdownTimeout は、停止の合図を受けてから、処理中のリクエストを待つ最長の時間。
const gracefulShutdownTimeout = 10 * time.Second

// cleanupInterval は、期限切れの行を消す間隔。
const cleanupInterval = time.Hour

// hashSlotWait は、パスワードの計算の空きを待つ時間の上限(超えたら、429 で待たせる)。
const hashSlotWait = 2 * time.Second

// main は、run を呼んで、失敗したときは、原因を表示して、異常終了する。
func main() {
	// `admin` で始まる引数のときは、サーバーではなく、管理者を作るコマンドとして動く。
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		if commandError := runAdminCommand(os.Args[2:]); commandError != nil {
			fmt.Fprintln(os.Stderr, commandError)
			os.Exit(1)
		}
		return
	}
	// 失敗したら、標準エラーに出して、終了コード 1 で終わる。
	if runError := run(); runError != nil {
		fmt.Fprintln(os.Stderr, runError)
		os.Exit(1)
	}
}

// run は、設定を読み、部品を組み立てて、サーバーを起動する。停止の合図(SIGINT、SIGTERM)で、安全に止まる。
//
// 戻り値:
//   - 起動できなかったとき、または異常終了したときのエラー。
func run() error {
	// 設定とロガーを用意する。
	authConfig, configError := config.LoadAuthConfig(sharedconfig.NewProcessEnvironmentReader())
	if configError != nil {
		return fmt.Errorf("設定を読み込めませんでした: %w", configError)
	}
	appLogger := logger.New(os.Stdout, "auth", authConfig.LogLevel)

	// データベースに接続する。
	appDatabase, databaseError := database.Open(authConfig.DatabaseURL)
	if databaseError != nil {
		return databaseError
	}
	defer func() { _ = database.Close(appDatabase) }()

	// 署名鍵を読み込む(開発用の設定なら、無いときは作る)。
	keyStore, keyError := keys.Load(authConfig.KeysDirectory, authConfig.ActiveKeyID, authConfig.KeysAutoGenerate, time.Now())
	if keyError != nil {
		return fmt.Errorf("署名鍵を読み込めませんでした: %w", keyError)
	}
	appLogger.Info("署名鍵を読み込んだ", "active_kid", keyStore.ActiveKeyID(), "keys", len(keyStore.JSONWebKeys()))

	// クライアントの IP を決めるものを作る。
	resolver, resolverError := clientip.NewResolver(authConfig.TrustedProxyCIDRs)
	if resolverError != nil {
		return resolverError
	}

	// 部品を組み立てる。
	userRepository := repository.NewUserRepository(appDatabase)
	refreshTokenRepository := repository.NewRefreshTokenRepository(appDatabase)
	loginFailureRepository := repository.NewLoginFailureRepository(appDatabase)
	rateLimitRepository := repository.NewRateLimitRepository(appDatabase)
	loginEventRepository := repository.NewLoginEventRepository(appDatabase)
	// 人間であることの確認(Cloudflare Turnstile)は、サイトキーとシークレットキーが、設定されているときだけ、有効にする。
	var captchaVerifier service.CaptchaVerifier
	if authConfig.TurnstileSecretKey != "" {
		captchaVerifier = turnstile.NewVerifier(turnstile.Options{SecretKey: authConfig.TurnstileSecretKey, VerifyURL: authConfig.TurnstileVerifyURL, Hostnames: authConfig.TurnstileHostnames})
		appLogger.Info("人間であることの確認(Cloudflare Turnstile)を有効にした", "hostnames", authConfig.TurnstileHostnames)
	}
	authService, serviceError := service.NewAuthService(userRepository, refreshTokenRepository, loginFailureRepository, rateLimitRepository, loginEventRepository, appLogger,
		password.NewHasher(password.DefaultParameters()).WithConcurrencyLimit(authConfig.HashMaxConcurrency, hashSlotWait), token.NewService(keyStore, time.Now), service.Settings{
			Captcha: captchaVerifier, SignupMode: authConfig.SignupMode, LoginMaxFailures: authConfig.LoginMaxFailures, LoginLockDuration: authConfig.LoginLockDuration,
			LoginIPLimit: authConfig.LoginIPLimit, LoginIPWindow: authConfig.LoginIPWindow,
			SignupIPLimit: authConfig.SignupIPLimit, SignupIPWindow: authConfig.SignupIPWindow,
		})
	if serviceError != nil {
		return serviceError
	}
	invitationRepository := repository.NewInvitationRepository(appDatabase)
	adminService := service.NewAdminService(userRepository, invitationRepository, loginFailureRepository, appLogger,
		password.NewHasher(password.DefaultParameters()), token.NewService(keyStore, time.Now))
	// 管理画面のために、全ユーザーのログイン履歴を読めるようにする。
	adminService.SetLoginEventStore(loginEventRepository)
	// 最初の管理者の指定があれば、管理者が1人もいないときだけ作る(失敗したら、設定の間違いに気づけるよう、起動しない)。
	if authConfig.BootstrapAdminName != "" {
		created, bootstrapError := adminService.BootstrapAdmin(context.Background(), authConfig.BootstrapAdminName, authConfig.BootstrapAdminPassword)
		if bootstrapError != nil {
			return fmt.Errorf("最初の管理者を作れませんでした: %w", bootstrapError)
		}
		appLogger.Info("最初の管理者の確認を済ませた", "created", created, "user_name", authConfig.BootstrapAdminName)
	}
	echoServer := server.New(appLogger, authService, adminService, keyStore, resolver, controller.PublicConfig{TurnstileSiteKey: authConfig.TurnstileSiteKey})

	// 停止の合図で終わる context を作る。
	shutdownContext, stopListening := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopListening()

	// 期限切れの行を、定期的に消す。
	go runCleanup(shutdownContext, appLogger, refreshTokenRepository, loginFailureRepository, rateLimitRepository, loginEventRepository, authConfig)

	// 起動する。
	startConfig := echo.StartConfig{
		Address: fmt.Sprintf(":%d", authConfig.HTTPPort), HideBanner: true, HidePort: true, GracefulTimeout: gracefulShutdownTimeout,
		// 遅い接続(slowloris など)で、接続を占有されないように、読み書きの時間に上限を付ける。
		BeforeServeFunc: func(httpServer *http.Server) error {
			httpServer.ReadHeaderTimeout = 5 * time.Second
			httpServer.ReadTimeout = 10 * time.Second
			httpServer.WriteTimeout = 30 * time.Second
			httpServer.IdleTimeout = 60 * time.Second
			return nil
		},
	}
	appLogger.Info("認証サーバーを起動する", "port", authConfig.HTTPPort, "signup_mode", authConfig.SignupMode)
	if startError := startConfig.Start(shutdownContext, echoServer); startError != nil {
		return fmt.Errorf("サーバーが異常終了した: %w", startError)
	}
	appLogger.Info("認証サーバーを停止した")
	return nil
}
