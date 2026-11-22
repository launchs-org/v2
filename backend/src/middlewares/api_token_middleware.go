package middlewares

import (
	"context"
	"strings"

	"github.com/labstack/echo/v5"

	"launchs/shared/apitoken"
	"launchs/shared/apperror"
)

// apiTokenClaimsContextKey は、API トークンで認証したときに、claims を入れておく context のキー。
const apiTokenClaimsContextKey = "api_token_claims"

// APITokenActiveChecker は、API トークンが有効(失効していない)かを確かめるもの。
type APITokenActiveChecker interface {
	IsActive(requestContext context.Context, tokenID string) (bool, error)
}

// NewTokenDispatchMiddleware は、トークンの種類で、認証の方法を振り分けるミドルウェアを作る。
//
// 接頭辞 "lkt_" のトークンは、API トークンとして、署名・失効・権限(scope と対象)を確かめる。
// それ以外は、利用者のログインの JWT として、渡された認証のミドルウェアに任せる。
//
// 引数:
//   - loginAuthentication: 利用者のログインの JWT を検証するミドルウェア。
//   - signer: API トークンの検証(nil なら、API トークンは全て拒否する)。
//   - checker: 失効の確認。
//
// 戻り値:
//   - Echo に登録するミドルウェア。
func NewTokenDispatchMiddleware(loginAuthentication echo.MiddlewareFunc, signer *apitoken.Signer, checker APITokenActiveChecker) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		// ログインの JWT の検証を、先に組み立てておく。
		loginHandler := loginAuthentication(next)
		return func(echoContext *echo.Context) error {
			// Authorization ヘッダから、トークンを取り出す。
			_, tokenText, _ := strings.Cut(echoContext.Request().Header.Get("Authorization"), " ")
			// API トークンでなければ、ログインの JWT の認証に任せる。
			if !strings.HasPrefix(strings.TrimSpace(tokenText), apitoken.Prefix) {
				return loginHandler(echoContext)
			}
			// API トークンを検証して、利用者として扱う。
			claims, authenticateError := AuthenticateAPIToken(echoContext, signer, checker, strings.TrimSpace(tokenText))
			if authenticateError != nil {
				return authenticateError
			}
			// 操作と対象が、権限の範囲内かを確かめる。
			if authorizeError := claims.Authorize(echoContext.Request().Method, echoContext.Path(), echoContext.Param("deployId"), echoContext.Param("projectId")); authorizeError != nil {
				return apperror.New(apperror.KindForbidden, authorizeError.Error())
			}
			return next(echoContext)
		}
	}
}

// AuthenticateAPIToken は、API トークンの署名・期限・失効を確かめ、成功したら、利用者を context に入れる。
//
// 権限(scope と対象)の判定は、呼び出し側が行う。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//   - signer: API トークンの検証(nil ならエラー)。
//   - checker: 失効の確認。
//   - tokenText: 接頭辞つきのトークン。
//
// 戻り値:
//   - 検証済みの claims。認証できなければ unauthorized。
func AuthenticateAPIToken(echoContext *echo.Context, signer *apitoken.Signer, checker APITokenActiveChecker, tokenText string) (*apitoken.Claims, error) {
	// 署名の秘密が無いときは、API トークンを受け付けない。
	if signer == nil {
		return nil, unauthorized(echoContext)
	}
	// 署名と期限を確かめる。
	claims, parseError := signer.Parse(tokenText)
	if parseError != nil {
		return nil, unauthorized(echoContext)
	}
	// 失効していないことを確かめる(確認できないときも、通さない)。
	active, checkError := checker.IsActive(echoContext.Request().Context(), claims.ID)
	if checkError != nil || !active {
		return nil, unauthorized(echoContext)
	}
	// 利用者として扱う(管理者の権限は、与えない)。
	echoContext.Set(userIDContextKey, claims.Subject)
	echoContext.Set(userRoleContextKey, "user")
	echoContext.Set(apiTokenClaimsContextKey, claims)
	return claims, nil
}

// IsAPITokenRequest は、このリクエストが、API トークンで認証されたものかを返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - API トークンなら true。
func IsAPITokenRequest(echoContext *echo.Context) bool {
	// 認証のときに入れた claims があるかを返す。
	_, exists := echoContext.Get(apiTokenClaimsContextKey).(*apitoken.Claims)
	return exists
}
