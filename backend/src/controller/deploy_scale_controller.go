package controller

import (
	"launchs/backend/src/middlewares"
	"net/http"

	"github.com/labstack/echo/v5"
)

// ScaleDeployRequest は、スケーリングのリクエストのボディ。
type ScaleDeployRequest struct {
	// Replicas は、新しいレプリカ数。0〜5(0 は停止相当。上限は model.MaxReplicas で、モデルが確かめる)。ポインタなのは、0 と未指定を区別するため。
	Replicas *int `json:"replicas" validate:"required"`
}

// ScaleDeployResponse は、スケーリングのレスポンスのボディ。
type ScaleDeployResponse struct {
	// DeployID は、スケーリングした deploy の ID。
	DeployID string `json:"deployId"`

	// Replicas は、新しいレプリカ数。
	Replicas int `json:"replicas"`
}

// Scale は、deploy のレプリカ数を変える(POST /deploy/:deployId/scale)。
//
// 反映の完了は待たず、すぐに応答する。結果は、deploy の status(applying → active か scaled_zero、失敗は failed)で確かめる。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - ボディが不正なら invalid_request、無い・他人のものなら not_found、
//     反映中・起動中・削除の途中なら conflict、未認証なら unauthorized。
func (deployController *DeployController) Scale(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ボディを読み込み、validate タグで検証する。
	var request ScaleDeployRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// スケーリングを、ビジネスルールに任せる。
	deployID := echoContext.Param("deployId")
	if scaleError := deployController.deployUseCase.Scale(echoContext.Request().Context(), userID, deployID, *request.Replicas); scaleError != nil {
		return scaleError
	}
	// deploy の ID と、新しい replicas を返す。
	return echoContext.JSON(http.StatusOK, ScaleDeployResponse{DeployID: deployID, Replicas: *request.Replicas})
}
