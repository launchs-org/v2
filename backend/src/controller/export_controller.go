package controller

import (
	"context"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/backend/src/service"
	"launchs/shared/jst"
	"launchs/shared/model"
)

// ExportUseCase は、アクセスログの転送の処理を、controller から見たもの。service.ExportService が実装する。
type ExportUseCase interface {
	GoogleStatus(requestContext context.Context, userID string) (service.GoogleStatus, error)
	GoogleAuthorizeURL(userID string) (string, error)
	GoogleCallback(requestContext context.Context, code string, rawState string) error
	DisconnectGoogle(requestContext context.Context, userID string) error
	ListLogExports(requestContext context.Context, userID string, projectID string, ingressID string) ([]model.ExportDestination, error)
	CreateLogExport(requestContext context.Context, userID string, projectID string, ingressID string, input service.LogExportInput) (model.ExportDestination, string, error)
	UpdateLogExport(requestContext context.Context, userID string, projectID string, ingressID string, destinationID string, input service.LogExportInput) (model.ExportDestination, error)
	DeleteLogExport(requestContext context.Context, userID string, projectID string, ingressID string, destinationID string) error
	TestLogExport(requestContext context.Context, userID string, projectID string, ingressID string, destinationID string) (service.TestResult, error)
}

// GoogleDriveStatusResponse は、アカウントの Google Drive の状態のレスポンス。
type GoogleDriveStatusResponse struct {
	// Available は、サーバーで、Google Drive に新しくつなげるか(管理者が、設定しているか)。
	Available bool `json:"available"`

	// Connected は、つないでいるか。
	Connected bool `json:"connected"`

	// AccountEmail は、つないだ Google アカウントのメールアドレス(つないでいなければ空)。
	AccountEmail string `json:"accountEmail"`

	// Status は、接続の状態(active / reauth_required。つないでいなければ空)。
	Status string `json:"status"`

	// LastError は、最後のエラー(利用者に見せる短い理由)。
	LastError string `json:"lastError"`
}

// GoogleAuthorizeResponse は、同意の画面の URL のレスポンス。
type GoogleAuthorizeResponse struct {
	// URL は、Google の同意の画面の URL(ブラウザで開く)。
	URL string `json:"url"`
}

// CreateLogExportRequest は、転送の設定を作るリクエスト。
type CreateLogExportRequest struct {
	// Kind は、種類(webhook / google_drive)。
	Kind string `json:"kind" validate:"required,oneof=webhook google_drive"`

	// URL は、webhook の URL(webhook のとき必須。形は service で確かめる)。
	URL *string `json:"url" validate:"omitempty,max=2000"`

	// IntervalSeconds は、webhook の送る間隔(秒。10 / 30 / 60 / 300。省略は 30)。
	IntervalSeconds *int `json:"intervalSeconds"`
}

// UpdateLogExportRequest は、転送の設定を変えるリクエスト(指定した項目だけを変える)。
type UpdateLogExportRequest struct {
	// Enabled は、有効・無効。
	Enabled *bool `json:"enabled"`

	// URL は、webhook の URL。
	URL *string `json:"url" validate:"omitempty,max=2000"`

	// IntervalSeconds は、webhook の送る間隔(秒)。
	IntervalSeconds *int `json:"intervalSeconds"`
}

// LogExportResponse は、転送の設定 1 件のレスポンス。
type LogExportResponse struct {
	// LogExportID は、転送の設定の ID。
	LogExportID string `json:"logExportId"`

	// Kind は、種類(webhook / google_drive)。
	Kind string `json:"kind"`

	// Enabled は、有効かどうか。
	Enabled bool `json:"enabled"`

	// Status は、状態(active / failing / reauth_required)。
	Status string `json:"status"`

	// URL は、webhook の URL(クエリは、伏せる)。Google Drive では、含まれない。
	URL string `json:"url,omitempty"`

	// IntervalSeconds は、webhook の送る間隔(秒)。Google Drive では、含まれない。
	IntervalSeconds int `json:"intervalSeconds,omitempty"`

	// LastSuccessAt は、最後に送れた日時(JST)。まだ送れていなければ null。
	LastSuccessAt *string `json:"lastSuccessAt"`

	// LastError は、最後のエラー(利用者に見せる短い理由)。
	LastError string `json:"lastError"`

	// FailureCount は、続けて失敗した回数。
	FailureCount int `json:"failureCount"`

	// CreatedAt は、作った日時(JST)。
	CreatedAt string `json:"createdAt"`

	// Secret は、webhook の署名の秘密。作ったときのレスポンスにだけ入る(あとから見せない)。
	Secret string `json:"secret,omitempty"`
}

// ListLogExportsResponse は、転送の設定の一覧のレスポンス。
type ListLogExportsResponse struct {
	// LogExports は、作った順の設定(無ければ空の配列)。
	LogExports []LogExportResponse `json:"logExports"`
}

// TestLogExportResponse は、テスト送信の結果のレスポンス。
type TestLogExportResponse struct {
	// OK は、送れたか。
	OK bool `json:"ok"`

	// Message は、送れなかったときの理由(送れたときは空)。
	Message string `json:"message"`
}

// ExportController は、アクセスログの転送の HTTP ハンドラーをまとめる。
type ExportController struct {
	// useCase は、処理の本体。
	useCase ExportUseCase
}

// NewExportController は、ExportController を作る。
//
// 引数:
//   - useCase: 転送の処理。
//
// 戻り値:
//   - 作った controller。
func NewExportController(useCase ExportUseCase) *ExportController {
	// 渡された処理を保持する。
	return &ExportController{useCase: useCase}
}

// maskURL は、webhook の URL を、画面に出す形にする(クエリには、秘密が入りやすいので、伏せる)。
//
// 引数:
//   - rawURL: URL。
//
// 戻り値:
//   - スキーム、ホスト、パスだけにしたもの。クエリがあれば、末尾に「?…」を付ける。
func maskURL(rawURL string) string {
	// 解析できなければ、何も出さない。
	parsed, parseError := url.Parse(rawURL)
	if parseError != nil {
		return ""
	}
	masked := parsed.Scheme + "://" + parsed.Host + parsed.Path
	if parsed.RawQuery != "" {
		masked += "?…"
	}
	return masked
}

// newLogExportResponse は、転送の設定を、レスポンスの形にする。
//
// 引数:
//   - destination: 転送の設定。
//
// 戻り値:
//   - レスポンス(秘密は、入れない)。
func newLogExportResponse(destination model.ExportDestination) LogExportResponse {
	// 共通の項目を入れる。
	response := LogExportResponse{LogExportID: destination.ID, Kind: destination.Kind, Enabled: destination.Enabled, Status: destination.Status,
		LastError: destination.LastError, FailureCount: destination.FailureCount, CreatedAt: jst.Format(destination.CreatedAt)}
	if destination.LastSuccessAt != nil {
		formatted := jst.Format(*destination.LastSuccessAt)
		response.LastSuccessAt = &formatted
	}
	// webhook は、URL(伏せたもの)と、間隔を入れる。
	if config, configError := destination.WebhookConfig(); configError == nil {
		response.URL = maskURL(config.URL)
		response.IntervalSeconds = config.IntervalSeconds
	}
	return response
}

// GoogleStatus は、GET /account/google-drive のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 状態(200)。つないでいなくても 200。
func (exportController *ExportController) GoogleStatus(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	status, statusError := exportController.useCase.GoogleStatus(echoContext.Request().Context(), userID)
	if statusError != nil {
		return statusError
	}
	return echoContext.JSON(http.StatusOK, GoogleDriveStatusResponse{Available: status.Available, Connected: status.Connected, AccountEmail: status.Email, Status: status.Status, LastError: status.LastError})
}

// GoogleAuthorize は、POST /account/google-drive/authorize のハンドラー。同意の画面の URL を返す(再接続も同じ)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 同意の画面の URL(200)。サーバーで Drive が設定されていなければ 503。
func (exportController *ExportController) GoogleAuthorize(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	authorizeURL, authorizeError := exportController.useCase.GoogleAuthorizeURL(userID)
	if authorizeError != nil {
		return authorizeError
	}
	return echoContext.JSON(http.StatusOK, GoogleAuthorizeResponse{URL: authorizeURL})
}

// GoogleDisconnect は、DELETE /account/google-drive のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。つないでいなければ 404。
func (exportController *ExportController) GoogleDisconnect(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	if disconnectError := exportController.useCase.DisconnectGoogle(echoContext.Request().Context(), userID); disconnectError != nil {
		return disconnectError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// GoogleCallback は、GET /export/google/callback のハンドラー。Google の同意のあとの戻り先(認証のヘッダーは、無い)。
// 結果は、画面に戻して知らせる(`/?google=connected`、`/?google=denied`、`/?google=error`)。理由の文章は、URL に入れない。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 画面へのリダイレクト(302)。
func (exportController *ExportController) GoogleCallback(echoContext *echo.Context) error {
	// 利用者が、同意しなかった(Google が error を付けて戻した)ときは、そのことを知らせる。
	if echoContext.QueryParam("error") != "" {
		return echoContext.Redirect(http.StatusFound, "/?google=denied")
	}
	// コードを合鍵に交換して、保存する。
	if callbackError := exportController.useCase.GoogleCallback(echoContext.Request().Context(), echoContext.QueryParam("code"), echoContext.QueryParam("state")); callbackError != nil {
		return echoContext.Redirect(http.StatusFound, "/?google=error")
	}
	return echoContext.Redirect(http.StatusFound, "/?google=connected")
}

// List は、GET /projects/:projectId/ingresses/:ingressId/log-exports のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 転送の設定の一覧(200)。ingress が無ければ 404。
func (exportController *ExportController) List(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	destinations, listError := exportController.useCase.ListLogExports(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"))
	if listError != nil {
		return listError
	}
	// レスポンスにする(空でも配列)。
	responses := make([]LogExportResponse, 0, len(destinations))
	for _, destination := range destinations {
		responses = append(responses, newLogExportResponse(destination))
	}
	return echoContext.JSON(http.StatusOK, ListLogExportsResponse{LogExports: responses})
}

// Create は、POST /projects/:projectId/ingresses/:ingressId/log-exports のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 作った設定(201。webhook は、署名の秘密を 1 回だけ含む)。形が不正なら 400、無ければ 404、上限・重複・未接続なら 409、機能が無効なら 503。
func (exportController *ExportController) Create(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。リクエストの形を確かめる。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	var request CreateLogExportRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	destination, secret, createError := exportController.useCase.CreateLogExport(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"),
		service.LogExportInput{Kind: request.Kind, URL: request.URL, IntervalSeconds: request.IntervalSeconds})
	if createError != nil {
		return createError
	}
	response := newLogExportResponse(destination)
	response.Secret = secret
	return echoContext.JSON(http.StatusCreated, response)
}

// Update は、PATCH /projects/:projectId/ingresses/:ingressId/log-exports/:logExportId のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 変えたあとの設定(200)。形が不正なら 400、無ければ 404。
func (exportController *ExportController) Update(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。リクエストの形を確かめる。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	var request UpdateLogExportRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	destination, updateError := exportController.useCase.UpdateLogExport(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"), echoContext.Param("logExportId"),
		service.LogExportInput{Enabled: request.Enabled, URL: request.URL, IntervalSeconds: request.IntervalSeconds})
	if updateError != nil {
		return updateError
	}
	return echoContext.JSON(http.StatusOK, newLogExportResponse(destination))
}

// Delete は、DELETE /projects/:projectId/ingresses/:ingressId/log-exports/:logExportId のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404。
func (exportController *ExportController) Delete(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	if deleteError := exportController.useCase.DeleteLogExport(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"), echoContext.Param("logExportId")); deleteError != nil {
		return deleteError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// Test は、POST /projects/:projectId/ingresses/:ingressId/log-exports/:logExportId/test のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - テスト送信の結果(200。送れなかったときも 200 で、ok=false と理由)。設定が無ければ 404。
func (exportController *ExportController) Test(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	result, testError := exportController.useCase.TestLogExport(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"), echoContext.Param("logExportId"))
	if testError != nil {
		return testError
	}
	return echoContext.JSON(http.StatusOK, TestLogExportResponse{OK: result.OK, Message: result.Message})
}
