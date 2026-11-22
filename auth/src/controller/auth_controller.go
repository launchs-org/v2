// Package controller は、認証コンテナの HTTP のハンドラーを定義する。
package controller

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	"launchs/auth/src/clientip"
	"launchs/auth/src/keys"
	"launchs/auth/src/model"
	"launchs/auth/src/service"
	"launchs/shared/apperror"
	"launchs/shared/jst"
	"launchs/shared/pagination"
)

// AuthUseCase は、AuthController が使う、認証のユースケース。
type AuthUseCase interface {
	Signup(requestContext context.Context, clientIP string, userAgent string, userName string, plainPassword string, invitationCode string, turnstileToken string) (string, error)
	Login(requestContext context.Context, clientIP string, userAgent string, userName string, plainPassword string, turnstileToken string) (string, error)
	LoginHistory(requestContext context.Context, accessToken string, limit int) ([]model.LoginEvent, error)
	Refresh(requestContext context.Context, refreshToken string) (string, error)
	Me(requestContext context.Context, accessToken string) (string, string, string, error)
	Logout(requestContext context.Context, refreshToken string) error
}

// SignupRequest は、サインアップのリクエスト。
type SignupRequest struct {
	// UserName は、ユーザー名。
	UserName string `json:"userName"`

	// Password は、パスワード。
	Password string `json:"password"`

	// InvitationCode は、招待コード。招待制のときだけ必要。
	InvitationCode string `json:"invitationCode"`

	// TurnstileToken は、Cloudflare Turnstile のトークン(人間であることの確認)。確認が有効なときだけ必要。
	TurnstileToken string `json:"turnstileToken"`
}

// LoginRequest は、ログインのリクエスト。
type LoginRequest struct {
	// UserName は、ユーザー名。
	UserName string `json:"userName"`

	// Password は、パスワード。
	Password string `json:"password"`

	// TurnstileToken は、Cloudflare Turnstile のトークン(人間であることの確認)。確認が有効なときだけ必要。
	TurnstileToken string `json:"turnstileToken"`
}

// TokenResponse は、トークンを返すレスポンス(signup と login は refresh token、refresh は access token)。
type TokenResponse struct {
	// Token は、トークンの文字列。
	Token string `json:"token"`
}

// MeResponse は、自分の情報のレスポンス。
type MeResponse struct {
	// UserID は、ユーザーの ID。
	UserID string `json:"userId"`

	// UserName は、ユーザー名。
	UserName string `json:"userName"`

	// Role は、役割(user か admin)。画面が、管理者向けの項目を出すかの判断に使う(権限の判定は、各 API が、毎回 DB で行う)。
	Role string `json:"role"`
}

// LoginEventResponse は、ログイン履歴1件のレスポンス。
type LoginEventResponse struct {
	// Kind は、種類(signup か login)。
	Kind string `json:"kind"`

	// Result は、結果(success か failure)。
	Result string `json:"result"`

	// Reason は、失敗の理由(unknown_user、wrong_password、locked、banned、invalid_input)。成功のときは null。
	Reason *string `json:"reason"`

	// IP は、クライアントの IP。
	IP string `json:"ip"`

	// UserAgent は、クライアントの User-Agent。
	UserAgent string `json:"userAgent"`

	// At は、試行の日時(JST)。
	At string `json:"at"`
}

// LoginHistoryResponse は、ログイン履歴のレスポンス。
type LoginHistoryResponse struct {
	// Events は、履歴(新しい順)。
	Events []LoginEventResponse `json:"events"`
}

// JSONWebKeySet は、JWKS のレスポンス。
type JSONWebKeySet struct {
	// Keys は、公開鍵の一覧。
	Keys []keys.JSONWebKey `json:"keys"`
}

// PublicConfig は、ブラウザに見せてよい、認証の設定(GET /config)。シークレットキーは、含めない。
type PublicConfig struct {
	// TurnstileSiteKey は、Cloudflare Turnstile のサイトキー。空なら、人間であることの確認は、無効。
	TurnstileSiteKey string
}

// PublicConfigResponse は、GET /config のレスポンス。
type PublicConfigResponse struct {
	// Turnstile は、人間であることの確認の設定。
	Turnstile TurnstileConfigResponse `json:"turnstile"`
}

// TurnstileConfigResponse は、人間であることの確認(Cloudflare Turnstile)の設定。
type TurnstileConfigResponse struct {
	// Enabled は、確認が有効か(有効なら、ログインとサインアップに、turnstileToken が要る)。
	Enabled bool `json:"enabled"`

	// SiteKey は、ウィジェットに渡すサイトキー。無効なら、省略。
	SiteKey string `json:"siteKey,omitempty"`
}

// AuthController は、認証の API のハンドラー。
type AuthController struct {
	authUseCase  AuthUseCase
	keyStore     *keys.KeyStore
	resolver     *clientip.Resolver
	publicConfig PublicConfig
}

// NewAuthController は、AuthController を作る。
//
// 引数:
//   - authUseCase: 認証のユースケース。
//   - keyStore: 署名鍵(JWKS の配信に使う)。
//   - resolver: クライアントの IP を決めるもの。
//   - publicConfig: ブラウザに見せてよい設定(GET /config)。
//
// 戻り値:
//   - 作成した AuthController。
func NewAuthController(authUseCase AuthUseCase, keyStore *keys.KeyStore, resolver *clientip.Resolver, publicConfig PublicConfig) *AuthController {
	// 依存するものを持たせて返す。
	return &AuthController{authUseCase: authUseCase, keyStore: keyStore, resolver: resolver, publicConfig: publicConfig}
}

// bearerToken は、Authorization ヘッダから、Bearer トークンを取り出す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - トークンの文字列。無い、または形が違うときは unauthorized。
func bearerToken(echoContext *echo.Context) (string, error) {
	// "Bearer <トークン>" の形だけを受け付ける(Bearer は、大文字小文字を区別しない)。
	scheme, tokenText, found := strings.Cut(echoContext.Request().Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(tokenText) == "" {
		return "", apperror.New(apperror.KindUnauthorized, "認証が必要です")
	}
	return strings.TrimSpace(tokenText), nil
}

// translateError は、レート制限のエラーを、Retry-After つきの 429 にする。それ以外のエラーは、そのまま返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//   - handledError: ユースケースのエラー。
//
// 戻り値:
//   - 返すエラー。
func translateError(echoContext *echo.Context, handledError error) error {
	// レート制限のときだけ、Retry-After(秒。切り上げ)を付けて、429 にする。
	if retryAfter, limited := service.IsRateLimited(handledError); limited {
		seconds := int(math.Ceil(retryAfter.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		echoContext.Response().Header().Set("Retry-After", strconv.Itoa(seconds))
		return apperror.New(apperror.KindTooManyRequests, "リクエストが多すぎます。しばらくしてから、もう一度試してください")
	}
	return handledError
}

// Signup は、POST /signup のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - refresh token のレスポンス(200)。入力が不正なら 400、名前の重複は 409、レート制限は 429。
func (authController *AuthController) Signup(echoContext *echo.Context) error {
	// リクエストを読む。
	var request SignupRequest
	if bindError := echoContext.Bind(&request); bindError != nil {
		return apperror.Wrap(apperror.KindInvalidRequest, "リクエストのボディを読み取れませんでした", bindError)
	}
	// サインアップする。
	refreshToken, signupError := authController.authUseCase.Signup(echoContext.Request().Context(), authController.resolver.Resolve(echoContext.Request()), echoContext.Request().UserAgent(), request.UserName, request.Password, request.InvitationCode, request.TurnstileToken)
	if signupError != nil {
		return translateError(echoContext, signupError)
	}
	return echoContext.JSON(http.StatusOK, TokenResponse{Token: refreshToken})
}

// Login は、POST /login のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - refresh token のレスポンス(200)。認証できなければ 401、レート制限は 429。
func (authController *AuthController) Login(echoContext *echo.Context) error {
	// リクエストを読む(形が不正なときは、401 ではなく 400)。
	var request LoginRequest
	if bindError := echoContext.Bind(&request); bindError != nil {
		return apperror.Wrap(apperror.KindInvalidRequest, "リクエストのボディを読み取れませんでした", bindError)
	}
	// ログインする。
	refreshToken, loginError := authController.authUseCase.Login(echoContext.Request().Context(), authController.resolver.Resolve(echoContext.Request()), echoContext.Request().UserAgent(), request.UserName, request.Password, request.TurnstileToken)
	if loginError != nil {
		return translateError(echoContext, loginError)
	}
	return echoContext.JSON(http.StatusOK, TokenResponse{Token: refreshToken})
}

// Refresh は、POST /refresh のハンドラー。refresh token から access token を返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - access token のレスポンス(200)。使えない refresh token は 401。
func (authController *AuthController) Refresh(echoContext *echo.Context) error {
	// Authorization ヘッダの refresh token を取り出す。
	refreshToken, tokenError := bearerToken(echoContext)
	if tokenError != nil {
		return tokenError
	}
	// access token を発行する。
	accessToken, refreshError := authController.authUseCase.Refresh(echoContext.Request().Context(), refreshToken)
	if refreshError != nil {
		return refreshError
	}
	return echoContext.JSON(http.StatusOK, TokenResponse{Token: accessToken})
}

// Me は、GET /me のハンドラー。access token の持ち主の情報を返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 自分の情報のレスポンス(200)。使えない access token は 401。
func (authController *AuthController) Me(echoContext *echo.Context) error {
	// Authorization ヘッダの access token を取り出す。
	accessToken, tokenError := bearerToken(echoContext)
	if tokenError != nil {
		return tokenError
	}
	// 持ち主の情報を取り出す。
	userID, userName, role, meError := authController.authUseCase.Me(echoContext.Request().Context(), accessToken)
	if meError != nil {
		return meError
	}
	return echoContext.JSON(http.StatusOK, MeResponse{UserID: userID, UserName: userName, Role: role})
}

// Logout は、POST /logout のハンドラー。refresh token を無効にする。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。使えない(ログアウト済みを含む)refresh token は 401。
func (authController *AuthController) Logout(echoContext *echo.Context) error {
	// Authorization ヘッダの refresh token を取り出す。
	refreshToken, tokenError := bearerToken(echoContext)
	if tokenError != nil {
		return tokenError
	}
	// 無効にする。
	if logoutError := authController.authUseCase.Logout(echoContext.Request().Context(), refreshToken); logoutError != nil {
		return logoutError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// LoginHistory は、GET /login-history のハンドラー。access token の持ち主の、ログイン履歴を返す(新しい順)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 履歴のレスポンス(200)。limit は 1〜100(既定 20)で、範囲外は 400。使えない access token は 401。
func (authController *AuthController) LoginHistory(echoContext *echo.Context) error {
	// Authorization ヘッダの access token を取り出す。
	accessToken, tokenError := bearerToken(echoContext)
	if tokenError != nil {
		return tokenError
	}
	// 件数を読む。
	limit, limitError := pagination.ParseLimit(echoContext.QueryParam("limit"))
	if limitError != nil {
		return limitError
	}
	// 履歴を取って、レスポンスにする(日時は JST)。
	events, historyError := authController.authUseCase.LoginHistory(echoContext.Request().Context(), accessToken, limit)
	if historyError != nil {
		return historyError
	}
	response := LoginHistoryResponse{Events: make([]LoginEventResponse, 0, len(events))}
	for _, event := range events {
		response.Events = append(response.Events, LoginEventResponse{
			Kind: event.Kind, Result: event.Result, Reason: event.Reason, IP: event.IP, UserAgent: event.UserAgent, At: jst.Format(event.OccurredAt),
		})
	}
	return echoContext.JSON(http.StatusOK, response)
}

// JSONWebKeys は、GET /.well-known/jwks.json のハンドラー。access token の検証用の公開鍵を配信する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - JWKS(200)。5分のキャッシュを許す。
func (authController *AuthController) JSONWebKeys(echoContext *echo.Context) error {
	// キャッシュを許して、公開鍵を返す(鍵の入れ替えは、古い鍵を15分残すので、5分の遅れは問題ない)。
	echoContext.Response().Header().Set("Cache-Control", "public, max-age=300")
	return echoContext.JSON(http.StatusOK, JSONWebKeySet{Keys: authController.keyStore.JSONWebKeys()})
}

// Config は、GET /config のハンドラー。ブラウザが、人間であることの確認(Turnstile)を、使うか、サイトキーは何かを、知るための API。認証は要らない。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 設定(200)。シークレットキーは、含めない。
func (authController *AuthController) Config(echoContext *echo.Context) error {
	// サイトキーがあれば、確認が有効。
	enabled := authController.publicConfig.TurnstileSiteKey != ""
	response := PublicConfigResponse{Turnstile: TurnstileConfigResponse{Enabled: enabled}}
	if enabled {
		response.Turnstile.SiteKey = authController.publicConfig.TurnstileSiteKey
	}
	return echoContext.JSON(http.StatusOK, response)
}

// Health は、GET /healthz のハンドラー。生きていることを返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 200。
func Health(echoContext *echo.Context) error {
	// 生きていることだけを返す。
	return echoContext.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
