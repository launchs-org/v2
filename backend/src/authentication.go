package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/config"
	"launchs/backend/src/middlewares"
	"launchs/shared/jwtverify"
)

// jwksInitialFetchTimeout は、起動時に JWKS を取得するときに待つ時間の上限。
const jwksInitialFetchTimeout = 5 * time.Second

// newAuthentication は、/api/v1 に付ける認証のミドルウェアを決めて作る。
//
// AUTH_JWKS_URL があれば、auth の公開鍵(JWKS)で access token を検証する。起動時に、JWKS を1度取得する。
// 取得できなくても、起動は止めない(auth が遅れて起動しても、最初のリクエストのときに、取得し直す)。
// AUTH_JWKS_URL が無ければ(設定の読み込みが、DEV_USER_ID があることを保証している)、開発用の仮の認証にして、警告を出す。
//
// 引数:
//   - appLogger: ログの出力先。
//   - backendConfig: バックエンドの設定。
//
// 戻り値:
//   - Echo に登録する認証のミドルウェア。
func newAuthentication(appLogger *slog.Logger, backendConfig config.BackendConfig) echo.MiddlewareFunc {
	// JWKS の URL が無いときは、開発用の仮の認証にする(全員が同じユーザーになるので、警告を出す)。
	if backendConfig.AuthJWKSURL == "" {
		appLogger.Warn("開発用の仮の認証で動いている(全てのリクエストが、同じユーザーとして扱われる)。本番では、AUTH_JWKS_URL を指定すること", "dev_user_id", backendConfig.DevUserID)
		return middlewares.NewDevAuthenticationMiddleware(backendConfig.DevUserID)
	}
	// 検証するものを作り、起動時に1度、公開鍵を取得してみる。
	verifier := jwtverify.New(jwtverify.Options{URL: backendConfig.AuthJWKSURL, Logger: appLogger})
	fetchContext, cancel := context.WithTimeout(context.Background(), jwksInitialFetchTimeout)
	defer cancel()
	if fetchError := verifier.Refresh(fetchContext); fetchError != nil {
		// 取得できなくても、起動は続ける(最初のリクエストのときに、取得し直す)。
		appLogger.Warn("起動時に JWKS を取得できなかった(リクエストのときに、取得し直す)", "url", backendConfig.AuthJWKSURL, "error", fetchError)
	} else {
		appLogger.Info("JWKS を取得した", "url", backendConfig.AuthJWKSURL)
	}
	// access token を検証するミドルウェアにする。
	return middlewares.NewAccessTokenMiddleware(verifier)
}
