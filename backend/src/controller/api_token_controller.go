package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/apperror"
	"launchs/shared/jst"
	"launchs/shared/model"
)

// APITokenUseCase は、APITokenController が使う、API トークンのユースケース。
type APITokenUseCase interface {
	Create(requestContext context.Context, userID string, name string, scope []string, deploys []string, projects []string, expiresAt *time.Time) (model.APIToken, string, error)
	List(requestContext context.Context, userID string) ([]model.APIToken, error)
	Revoke(requestContext context.Context, userID string, tokenID string) error
}

// APITokenController は、API トークンの発行・一覧・失効の HTTP のハンドラー。
type APITokenController struct {
	apiTokenUseCase APITokenUseCase
}

// NewAPITokenController は、APITokenController を作る。
//
// 引数:
//   - apiTokenUseCase: API トークンのユースケース。
//
// 戻り値:
//   - 作成した APITokenController。
func NewAPITokenController(apiTokenUseCase APITokenUseCase) *APITokenController {
	// ユースケースを持たせて返す。
	return &APITokenController{apiTokenUseCase: apiTokenUseCase}
}

// CreateAPITokenRequest は、API トークンの発行のリクエストのボディ。
type CreateAPITokenRequest struct {
	// Name は、名前(用途のメモ)。100文字まで。
	Name string `json:"name" validate:"required,max=100"`

	// Scope は、許す操作(read、write、deploy:image)。
	Scope []string `json:"scope" validate:"required,min=1,max=3,dive,max=32"`

	// Deploys は、対象の deploy の ID(省略なら絞らない)。50個まで。
	Deploys []string `json:"deploys" validate:"omitempty,max=50,dive,max=128"`

	// Projects は、対象のプロジェクトの ID(省略なら絞らない)。50個まで。
	Projects []string `json:"projects" validate:"omitempty,max=50,dive,max=128"`

	// ExpiresInDays は、有効な日数(1〜3650)。省略なら無期限。
	ExpiresInDays *int `json:"expiresInDays" validate:"omitempty,min=1,max=3650"`
}

// APITokenResponse は、API トークンの情報(トークンの本体は含めない)。
type APITokenResponse struct {
	TokenID    string   `json:"tokenId"`
	Name       string   `json:"name"`
	Scope      []string `json:"scope"`
	Deploys    []string `json:"deploys"`
	Projects   []string `json:"projects"`
	ExpiresAt  *string  `json:"expiresAt"`
	RevokedAt  *string  `json:"revokedAt"`
	LastUsedAt *string  `json:"lastUsedAt"`
	CreatedAt  string   `json:"createdAt"`
}

// CreateAPITokenResponse は、発行の結果。token は、このときだけ返す。
type CreateAPITokenResponse struct {
	APITokenResponse
	Token string `json:"token"`
}

// requireLoginUser は、ログインした利用者の ID を返す。API トークンでの呼び出しは、forbidden にする(トークンが別のトークンを作れないように)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - ユーザー ID。未認証なら unauthorized、API トークンなら forbidden。
func requireLoginUser(echoContext *echo.Context) (string, error) {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return "", userIDError
	}
	// API トークンでは、トークンを管理できない。
	if middlewares.IsAPITokenRequest(echoContext) {
		return "", apperror.New(apperror.KindForbidden, "API トークンの管理は、ログインしたユーザーだけができます")
	}
	return userID, nil
}

// Create は、API トークンを発行する(POST /api-tokens)。トークンの本体は、この応答でだけ返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - ボディが不正なら invalid_request、API トークンでの呼び出しなら forbidden。
func (apiTokenController *APITokenController) Create(echoContext *echo.Context) error {
	// ログインしたユーザーだけが、発行できる。
	userID, userError := requireLoginUser(echoContext)
	if userError != nil {
		return userError
	}
	// ボディを読み込み、検証する。
	var request CreateAPITokenRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 期限を、日数から決める。
	var expiresAt *time.Time
	if request.ExpiresInDays != nil {
		deadline := time.Now().Add(time.Duration(*request.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &deadline
	}
	// 発行を、ユースケースに任せる。
	token, tokenText, createError := apiTokenController.apiTokenUseCase.Create(echoContext.Request().Context(), userID, request.Name, request.Scope, request.Deploys, request.Projects, expiresAt)
	if createError != nil {
		return createError
	}
	return echoContext.JSON(http.StatusCreated, CreateAPITokenResponse{APITokenResponse: newAPITokenResponse(token), Token: tokenText})
}

// List は、API トークンの一覧を返す(GET /api-tokens)。トークンの本体は含めない。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - トークンの一覧。
func (apiTokenController *APITokenController) List(echoContext *echo.Context) error {
	// ログインしたユーザーだけが、見られる。
	userID, userError := requireLoginUser(echoContext)
	if userError != nil {
		return userError
	}
	tokens, listError := apiTokenController.apiTokenUseCase.List(echoContext.Request().Context(), userID)
	if listError != nil {
		return listError
	}
	// 応答の形にする。
	items := make([]APITokenResponse, 0, len(tokens))
	for _, token := range tokens {
		items = append(items, newAPITokenResponse(token))
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"tokens": items})
}

// Revoke は、API トークンを失効させる(DELETE /api-tokens/:tokenId)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 無い・他人のものなら not_found。
func (apiTokenController *APITokenController) Revoke(echoContext *echo.Context) error {
	// ログインしたユーザーだけが、失効できる。
	userID, userError := requireLoginUser(echoContext)
	if userError != nil {
		return userError
	}
	if revokeError := apiTokenController.apiTokenUseCase.Revoke(echoContext.Request().Context(), userID, echoContext.Param("tokenId")); revokeError != nil {
		return revokeError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// newAPITokenResponse は、保存された行から、応答の形を作る。
//
// 引数:
//   - token: 保存された行。
//
// 戻り値:
//   - 応答の形(日時は日本時間)。
func newAPITokenResponse(token model.APIToken) APITokenResponse {
	// 日時を、あるときだけ日本時間の文字列にする。
	format := func(value *time.Time) *string {
		if value == nil {
			return nil
		}
		text := jst.Format(*value)
		return &text
	}
	return APITokenResponse{TokenID: token.ID, Name: token.Name, Scope: stringList(token.Scope), Deploys: stringList(token.Deploys), Projects: stringList(token.Projects),
		ExpiresAt: format(token.ExpiresAt), RevokedAt: format(token.RevokedAt), LastUsedAt: format(token.LastUsedAt), CreatedAt: jst.Format(token.CreatedAt)}
}

// stringList は、JSON の文字列の配列を、文字列のスライスにする(読めないときと空のときは、空のスライス)。
//
// 引数:
//   - raw: JSON の配列。
//
// 戻り値:
//   - 文字列のスライス(nil にはしない)。
func stringList(raw []byte) []string {
	// 読めなければ、空のスライスにする。
	values := []string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &values)
	}
	return values
}
