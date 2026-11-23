// Package controller は、管理コンテナの HTTP の入出力を担当する。
package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/admin/src/repository"
)

// OverviewUseCase は、OverviewController が呼ぶ、概要の取得。
type OverviewUseCase interface {
	// Get は、全体の概要を返す。
	Get(requestContext context.Context) (repository.Overview, error)
}

// OverviewController は、GET /admin-api/overview を担当する。
type OverviewController struct {
	overviewUseCase OverviewUseCase
}

// NewOverviewController は、OverviewController を作る。
//
// 引数:
//   - overviewUseCase: 概要の取得。
//
// 戻り値:
//   - 作成した OverviewController。
func NewOverviewController(overviewUseCase OverviewUseCase) *OverviewController {
	// 依存するものを持たせて返す。
	return &OverviewController{overviewUseCase: overviewUseCase}
}

// Get は、全体の概要(プロジェクト、deploy、ingress、volume の状態ごとの数)を返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 概要(200)。管理者でなければ 403(ミドルウェア)。
func (overviewController *OverviewController) Get(echoContext *echo.Context) error {
	// 概要を取得して、そのまま返す。
	overview, getError := overviewController.overviewUseCase.Get(echoContext.Request().Context())
	if getError != nil {
		return getError
	}
	return echoContext.JSON(http.StatusOK, overview)
}

// Health は、GET /healthz のハンドラー。動いていることだけを返す(認証なし)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - {"status":"ok"}(200)。
func Health(echoContext *echo.Context) error {
	// 外部には問い合わせない。
	return echoContext.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
