// Package middleware は、管理コンテナの認証のミドルウェアを置く。
package middleware

import (
	"context"
	"strings"

	"github.com/labstack/echo/v5"

	"launchs/shared/apperror"
	"launchs/shared/jwtverify"
)

// AccessTokenVerifier は、access token を検証するもの(jwtverify.Verifier が満たす)。
type AccessTokenVerifier interface {
	// Verify は、access token を検証して、中身を返す。使えなければエラー。
	Verify(requestContext context.Context, tokenText string) (*jwtverify.Claims, error)
}

// adminRole は、管理者の役割の名前(access token の role の値)。
const adminRole = "admin"

// userIDContextKey と userNameContextKey は、認証できたユーザーを、コンテキストに入れるキー。
const (
	userIDContextKey   = "admin.userId"
	userNameContextKey = "admin.userName"
)

// ActorFrom は、認証できた管理者の ID と名前を、コンテキストから取り出す(監査ログのため)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 管理者の ID と名前。認証のミドルウェアを通っていなければ、空。
func ActorFrom(echoContext *echo.Context) (string, string) {
	// 文字列として入れてあるものを、取り出す。
	userID, _ := echoContext.Get(userIDContextKey).(string)
	userName, _ := echoContext.Get(userNameContextKey).(string)
	return userID, userName
}

// NewAdminAuthMiddleware は、管理者の access token だけを通すミドルウェアを作る。
//
// `Authorization: Bearer <access token>` を検証し、role が admin でなければ 403 にする。トークンが使えなければ 401(理由は区別しない)。
// role は access token の値なので、管理者でなくなった人が、最大 15 分、通ることがある(auth の /auth/admin は、毎回 DB で確かめる)。
//
// 引数:
//   - verifier: access token の検証。
//
// 戻り値:
//   - Echo に登録するミドルウェア。
func NewAdminAuthMiddleware(verifier AccessTokenVerifier) echo.MiddlewareFunc {
	// 次のハンドラーを受け取り、認証を挟んだハンドラーを返す。
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(echoContext *echo.Context) error {
			// "Bearer <トークン>" の形だけを受け付ける。
			scheme, tokenText, found := strings.Cut(echoContext.Request().Header.Get("Authorization"), " ")
			tokenText = strings.TrimSpace(tokenText)
			if !found || !strings.EqualFold(scheme, "Bearer") || tokenText == "" {
				return unauthorized(echoContext)
			}
			// 署名・期限・発行者・種類を検証する。
			claims, verifyError := verifier.Verify(echoContext.Request().Context(), tokenText)
			if verifyError != nil {
				return unauthorized(echoContext)
			}
			// 管理者でなければ、権限が無い。
			if claims.Role != adminRole {
				return apperror.New(apperror.KindForbidden, "この操作は、管理者だけが行えます")
			}
			// 後ろのハンドラーが使えるよう、管理者の情報を入れて、進める。
			echoContext.Set(userIDContextKey, claims.Subject)
			echoContext.Set(userNameContextKey, claims.Name)
			return next(echoContext)
		}
	}
}

// unauthorized は、401 のエラーを作る。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - unauthorized のエラー。
func unauthorized(echoContext *echo.Context) error {
	// Bearer 認証が必要なことを、ヘッダで伝える。
	echoContext.Response().Header().Set("WWW-Authenticate", "Bearer")
	return apperror.New(apperror.KindUnauthorized, "認証が必要です")
}
