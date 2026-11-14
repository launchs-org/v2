package controller

import (
	"launchs/backend/src/middlewares"
	"net/http"

	"github.com/labstack/echo/v5"
)

// UpdateDeployConfigRequest は、起動設定の更新のリクエストのボディ。3つとも、常に全件を送って置き換える。
type UpdateDeployConfigRequest struct {
	// Env は、環境変数。必須(空にするなら {})。100個まで、名前は128文字まで、値は4096文字まで。
	Env map[string]string `json:"env" validate:"required,max=100,dive,keys,envname,max=128,endkeys,max=4096"`

	// Command は、起動コマンド。null か省略ならイメージの既定。1024文字まで。
	Command *string `json:"command" validate:"omitempty,min=1,max=1024"`

	// Args は、起動引数。null か省略ならイメージの既定。100個まで、1つは4096文字まで。
	Args []string `json:"args" validate:"omitempty,max=100,dive,max=4096"`
}

// GetConfig は、deploy の起動設定を返す(GET /deploy/:deployId/config)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い、または他のユーザーのものなら not_found、未認証なら unauthorized。
func (deployController *DeployController) GetConfig(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// deploy を取得する。
	deploy, getError := deployController.deployUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("deployId"))
	if getError != nil {
		return getError
	}
	// 詳細のうち、起動設定の部分だけを返す。
	return echoContext.JSON(http.StatusOK, newDeployResponse(deploy).Config)
}

// UpdateConfig は、起動設定を、まだ適用しない値として保存する(PUT /deploy/:deployId/config)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - ボディが不正なら invalid_request、無い・他人のものなら not_found、
//     反映中や削除の途中なら conflict、未認証なら unauthorized。
func (deployController *DeployController) UpdateConfig(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ボディを読み込み、validate タグで検証する。
	var request UpdateDeployConfigRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 保存を、ビジネスルールに任せる。
	updateError := deployController.deployUseCase.UpdateConfig(echoContext.Request().Context(), userID, echoContext.Param("deployId"), request.Env, request.Command, request.Args)
	if updateError != nil {
		return updateError
	}
	// 空のオブジェクトを返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// DiscardConfig は、まだ適用していない起動設定を破棄する(POST /deploy/:deployId/config/discard)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い・他人のものなら not_found、反映中・削除の途中・破棄する値が無いなら conflict、未認証なら unauthorized。
func (deployController *DeployController) DiscardConfig(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 破棄を、ビジネスルールに任せる。
	if discardError := deployController.deployUseCase.DiscardConfig(echoContext.Request().Context(), userID, echoContext.Param("deployId")); discardError != nil {
		return discardError
	}
	// 空のオブジェクトを返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// ApplyConfig は、まだ適用していない起動設定の反映を開始する(POST /deploy/:deployId/config/apply)。
//
// 反映の完了は待たず、すぐに応答する。結果は、deploy の status(applying → active か failed)で確かめる。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い・他人のものなら not_found、適用する値が無い・反映中・起動中・削除の途中なら conflict、未認証なら unauthorized。
func (deployController *DeployController) ApplyConfig(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 反映の開始を、ビジネスルールに任せる。
	if applyError := deployController.deployUseCase.ApplyConfig(echoContext.Request().Context(), userID, echoContext.Param("deployId")); applyError != nil {
		return applyError
	}
	// 空のオブジェクトを返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}
