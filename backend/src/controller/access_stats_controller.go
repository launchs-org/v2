package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/accessstats"
)

// AccessStatsUseCase は、AccessStatsController が使う、アクセスの集計のユースケース。
type AccessStatsUseCase interface {
	Get(requestContext context.Context, userID string, projectID string, ingressID string, rangeName string) (accessstats.Report, bool, error)
}

// AccessStatsResponse は、アクセスの集計のレスポンス。
type AccessStatsResponse struct {
	// Enabled は、この ingress で、集計が有効か(無効にした後も、過去の集計は読める)。
	Enabled bool `json:"enabled"`

	accessstats.Report
}

// AccessStatsController は、アクセスの集計の API のハンドラー。
type AccessStatsController struct {
	accessStatsUseCase AccessStatsUseCase
}

// NewAccessStatsController は、AccessStatsController を作る。
//
// 引数:
//   - accessStatsUseCase: アクセスの集計のユースケース。
//
// 戻り値:
//   - 作成した AccessStatsController。
func NewAccessStatsController(accessStatsUseCase AccessStatsUseCase) *AccessStatsController {
	// ユースケースを持たせて返す。
	return &AccessStatsController{accessStatsUseCase: accessStatsUseCase}
}

// Get は、GET /projects/:projectId/ingresses/:ingressId/access-stats のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。クエリ range(1h、24h、7d、30d。既定 1h)を読む。
//
// 戻り値:
//   - 集計のレスポンス(200)。range が不正なら 400、ingress が無ければ 404。
func (accessStatsController *AccessStatsController) Get(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 集計を取る。
	report, enabled, getError := accessStatsController.accessStatsUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"), echoContext.QueryParam("range"))
	if getError != nil {
		return getError
	}
	return echoContext.JSON(http.StatusOK, AccessStatsResponse{Enabled: enabled, Report: report})
}
