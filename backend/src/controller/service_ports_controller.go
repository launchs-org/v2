package controller

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
)

// UpdateServicePortsRequest は、service のポートの更新のリクエストのボディ。常に、全件を送って置き換える。
type UpdateServicePortsRequest struct {
	// Ports は、新しいポート。1〜20個。作成のときと同じ規則で検査する。
	Ports []ServicePortRequest `json:"ports" validate:"required,min=1,max=20,dive"`
}

// UpdatePorts は、ポートを、まだ適用しない値として保存する(PATCH /deploy/:deployId/service)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - ボディが不正なら invalid_request、無い・他人のものなら not_found、
//     deploy が反映中・削除の途中、または service が active でないなら conflict、未認証なら unauthorized。
func (serviceController *ServiceController) UpdatePorts(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ボディを読み込み、validate タグで検証する。
	var request UpdateServicePortsRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 配列の中の重複(名前、番号と protocol の組)を検査して、モデルにする。
	ports, portsError := newServicePorts(request.Ports)
	if portsError != nil {
		return portsError
	}
	// 保存を、ビジネスルールに任せる。
	if updateError := serviceController.serviceUseCase.UpdatePorts(echoContext.Request().Context(), userID, echoContext.Param("deployId"), ports); updateError != nil {
		return updateError
	}
	// 空のオブジェクトを返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// ApplyPorts は、まだ適用していないポートの反映を開始する(POST /deploy/:deployId/service/apply)。
//
// 反映の完了は待たず、すぐに応答する。結果は、deploy の status(applying → active、失敗は failed)で確かめる。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い・他人のものなら not_found、適用する値が無い・反映中・起動中・削除の途中・service が active でないなら conflict、未認証なら unauthorized。
func (serviceController *ServiceController) ApplyPorts(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 反映の開始を、ビジネスルールに任せる。
	if applyError := serviceController.serviceUseCase.ApplyPorts(echoContext.Request().Context(), userID, echoContext.Param("deployId")); applyError != nil {
		return applyError
	}
	// 空のオブジェクトを返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// DiscardPorts は、まだ適用していないポートを破棄する(POST /deploy/:deployId/service/discard)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い・他人のものなら not_found、反映中・削除の途中・破棄する値が無い・service が active でないなら conflict、未認証なら unauthorized。
func (serviceController *ServiceController) DiscardPorts(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 破棄を、ビジネスルールに任せる。
	if discardError := serviceController.serviceUseCase.DiscardPorts(echoContext.Request().Context(), userID, echoContext.Param("deployId")); discardError != nil {
		return discardError
	}
	// 空のオブジェクトを返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}
