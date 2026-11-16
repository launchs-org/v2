package controller

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/jst"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// DeploySummaryResponse は、deploy 一覧の1件。
type DeploySummaryResponse struct {
	// DeployID は、deploy の ID。
	DeployID string `json:"deployId"`

	// Status は、deploy の状態。
	Status string `json:"status"`

	// Spec は、スペックの名前(small、medium、large)。
	Spec string `json:"spec"`

	// Replicas は、希望するレプリカ数。
	Replicas int `json:"replicas"`

	// ImageRef は、起動しているイメージ。
	ImageRef string `json:"imageRef"`

	// CreatedAt は、作成日時(JST)。
	CreatedAt string `json:"createdAt"`
}

// ListDeploysResponse は、deploy 一覧のレスポンスのボディ。
type ListDeploysResponse struct {
	// Deploys は、deploy の一覧(作成日時の新しい順)。
	Deploys []DeploySummaryResponse `json:"deploys"`

	// NextCursor は、次のページを取るためのカーソル。最後のページなら null。
	NextCursor *string `json:"nextCursor"`
}

// ListByProject は、プロジェクトの deploy の履歴を返す(GET /projects/:projectId/deploys)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 一覧(200)。limit や cursor が不正なら invalid_request(400)、未認証なら unauthorized(401)、プロジェクトが無い・他のユーザーのものなら not_found(404)。
func (deployController *DeployController) ListByProject(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 件数と、続きの位置を読む。
	limit, limitError := pagination.ParseLimit(echoContext.QueryParam("limit"))
	if limitError != nil {
		return limitError
	}
	var after *pagination.Position
	if cursorText := echoContext.QueryParam("cursor"); cursorText != "" {
		position, cursorError := pagination.DecodeCursor(cursorText)
		if cursorError != nil {
			return cursorError
		}
		after = &position
	}
	// ビジネスルールに、取得を任せる。
	deploys, nextPosition, listError := deployController.deployUseCase.List(echoContext.Request().Context(), userID, echoContext.Param("projectId"), limit, after)
	if listError != nil {
		return listError
	}
	// レスポンスにする。
	response := ListDeploysResponse{Deploys: make([]DeploySummaryResponse, 0, len(deploys))}
	for _, deploy := range deploys {
		response.Deploys = append(response.Deploys, newDeploySummaryResponse(deploy))
	}
	if nextPosition != nil {
		nextCursor := pagination.EncodeCursor(*nextPosition)
		response.NextCursor = &nextCursor
	}
	return echoContext.JSON(http.StatusOK, response)
}

// newDeploySummaryResponse は、deploy を、一覧の1件のレスポンスにする。
//
// 引数:
//   - deploy: deploy のモデル。
//
// 戻り値:
//   - 一覧の1件。
func newDeploySummaryResponse(deploy model.Deploy) DeploySummaryResponse {
	// 日時を JST の文字列にして、項目を写す。
	return DeploySummaryResponse{DeployID: deploy.ID, Status: deploy.Status, Spec: deploy.Spec, Replicas: deploy.Replicas, ImageRef: deploy.ImageRef, CreatedAt: jst.Format(deploy.CreatedAt)}
}
