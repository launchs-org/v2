package middlewares

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

// NewAccessTokenMiddleware は、Authorization ヘッダの access token を検証するミドルウェアを作る。
//
// `Authorization: Bearer <access token>` を読み、検証して、通れば、ユーザーの ID・名前・役割を、コンテキストに入れる
// (後ろのハンドラーは、UserIDFrom で ID を取り出す)。失敗は、理由を区別せず、全て 401(WWW-Authenticate つき)。
//
// 引数:
//   - verifier: access token の検証。
//
// 戻り値:
//   - Echo に登録するミドルウェア。
func NewAccessTokenMiddleware(verifier AccessTokenVerifier) echo.MiddlewareFunc {
	// Echo の形式に合わせて、次のハンドラーを受け取り、認証を挟んだハンドラーを返す関数を返す。
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		// リクエストごとに呼ばれる、認証を挟んだハンドラーを返す。
		return func(echoContext *echo.Context) error {
			// "Bearer <トークン>" の形だけを受け付ける(Bearer は、大文字小文字を区別しない)。
			scheme, tokenText, found := strings.Cut(echoContext.Request().Header.Get("Authorization"), " ")
			tokenText = strings.TrimSpace(tokenText)
			if !found || !strings.EqualFold(scheme, "Bearer") || tokenText == "" {
				return unauthorized(echoContext)
			}
			// 検証する(署名・期限・発行者・種類)。
			claims, verifyError := verifier.Verify(echoContext.Request().Context(), tokenText)
			if verifyError != nil {
				return unauthorized(echoContext)
			}
			// 後ろのハンドラーが取り出せるよう、ユーザーの情報をコンテキストに入れる。
			echoContext.Set(userIDContextKey, claims.Subject)
			echoContext.Set(userNameContextKey, claims.Name)
			echoContext.Set(userRoleContextKey, claims.Role)
			// 認証できたので、次のハンドラーに処理を渡す。
			return next(echoContext)
		}
	}
}

// unauthorized は、401 のエラーを作る(理由は区別しない)。WWW-Authenticate を付ける。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - unauthorized のエラー(エラーハンドラーが 401 に変換する)。
func unauthorized(echoContext *echo.Context) error {
	// Bearer 認証が必要なことを、ヘッダで伝える。
	echoContext.Response().Header().Set("WWW-Authenticate", "Bearer")
	return apperror.New(apperror.KindUnauthorized, "認証が必要です")
}
