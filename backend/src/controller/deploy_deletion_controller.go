package controller

import (
	"launchs/backend/src/middlewares"
	"net/http"

	"github.com/labstack/echo/v5"
)

// Delete は、deploy の削除を受け付ける(DELETE /deploy/:deployId)。
//
// 削除の完了は待たず、すぐに応答する。status はすぐに deleting になり、完了するとレコードが消えて GET が 404 になる。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い・他人のものなら not_found、反映中・起動中・削除中なら conflict、未認証なら unauthorized。
func (deployController *DeployController) Delete(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 削除の受け付けを、ビジネスルールに任せる。
	if deleteError := deployController.deployUseCase.Delete(echoContext.Request().Context(), userID, echoContext.Param("deployId")); deleteError != nil {
		return deleteError
	}
	// 空のオブジェクトを返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}
