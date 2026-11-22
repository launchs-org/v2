package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/model"
)

// DeployImageHistoryUseCase は、DeployImageHistoryController が使う、履歴の取得のユースケース。
type DeployImageHistoryUseCase interface {
	List(requestContext context.Context, userID string, deployID string) ([]model.DeployImageHistory, error)
}

// DeployImageHistoryController は、deploy のイメージの更新履歴の API のハンドラー。
type DeployImageHistoryController struct {
	useCase DeployImageHistoryUseCase
}

// NewDeployImageHistoryController は、DeployImageHistoryController を作る。
//
// 引数:
//   - useCase: 履歴の取得のビジネスルール。
//
// 戻り値:
//   - 作成した DeployImageHistoryController。
func NewDeployImageHistoryController(useCase DeployImageHistoryUseCase) *DeployImageHistoryController {
	// ユースケースを持たせて返す。
	return &DeployImageHistoryController{useCase: useCase}
}

// imageHistoryItemResponse は、イメージの更新履歴の 1 件のレスポンス。
type imageHistoryItemResponse struct {
	PreviousImageRef string `json:"previousImageRef"`
	ImageRef         string `json:"imageRef"`
	Result           string `json:"result"`
	At               string `json:"at"`
}

// imageHistoryResponse は、イメージの更新履歴のレスポンス。
type imageHistoryResponse struct {
	History []imageHistoryItemResponse `json:"history"`
}

// List は、deploy のイメージの更新履歴を返す(GET /deploy/:deployId/image-history)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 履歴のレスポンス(200。新しい順。無ければ空の配列)。未認証なら 401、deploy が無い・他のユーザーのものなら 404。
func (deployImageHistoryController *DeployImageHistoryController) List(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ビジネスルールに、取得を任せる。
	rows, listError := deployImageHistoryController.useCase.List(echoContext.Request().Context(), userID, echoContext.Param("deployId"))
	if listError != nil {
		return listError
	}
	// 空でも、null ではなく、空の配列を返す。
	items := make([]imageHistoryItemResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, imageHistoryItemResponse{PreviousImageRef: row.PreviousImageRef, ImageRef: row.ImageRef, Result: row.Result, At: row.CreatedAt.Format(time.RFC3339)})
	}
	return echoContext.JSON(http.StatusOK, imageHistoryResponse{History: items})
}
