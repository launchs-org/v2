package controller

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/model"
)

// MountRequest は、マウント1つのリクエストの形。volumeId の形と path の書式は、ユースケースで確かめる。
type MountRequest struct {
	// VolumeID は、マウントする volume の ID。
	VolumeID string `json:"volumeId" validate:"required"`

	// MountPath は、コンテナの中のマウント先の path。
	MountPath string `json:"mountPath" validate:"required,max=200"`
}

// UpdateMountsRequest は、マウントの構成の更新(一括置き換え)のリクエスト。空の配列は、全て外す。
type UpdateMountsRequest struct {
	// Mounts は、新しいマウントの全件。
	Mounts []MountRequest `json:"mounts" validate:"required,max=10,dive"`
}

// MountResponse は、レスポンスに含める、マウント1つ。
type MountResponse struct {
	// VolumeID は、マウントする volume の ID。
	VolumeID string `json:"volumeId"`

	// MountPath は、コンテナの中のマウント先の path。
	MountPath string `json:"mountPath"`
}

// MountsResponse は、マウントの構成のレスポンス。
type MountsResponse struct {
	// Mounts は、現在コンテナに反映されているマウント(無ければ空の配列)。
	Mounts []MountResponse `json:"mounts"`

	// PendingMounts は、まだ適用していないマウント。変更が無ければ null。
	PendingMounts []MountResponse `json:"pending_mounts"`
}

// GetMounts は、GET /deploy/:deployId/mounts のハンドラー。マウントの構成を返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - マウントの構成のレスポンス(200)。無ければ 404。
func (deployController *DeployController) GetMounts(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// deploy を取得する。
	deploy, getError := deployController.deployUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("deployId"))
	if getError != nil {
		return getError
	}
	// 現在のマウントは、空でも配列にする。pending は、無ければ null(空の配列は、空の配列として返す)。
	response := MountsResponse{Mounts: mountResponses(deploy.Mounts)}
	if response.Mounts == nil {
		response.Mounts = []MountResponse{}
	}
	if deploy.HasPendingMounts() {
		response.PendingMounts = mountResponses(deploy.PendingMounts)
		if response.PendingMounts == nil {
			response.PendingMounts = []MountResponse{}
		}
	}
	return echoContext.JSON(http.StatusOK, response)
}

// UpdateMounts は、PUT /deploy/:deployId/mounts のハンドラー。マウントの構成を、pending として保存する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。不正なら 400、無ければ 404、反映中・削除の途中なら 409。
func (deployController *DeployController) UpdateMounts(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request UpdateMountsRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// マウントに変換して、保存する(path の書式や volume は、ユースケースで確かめる)。
	mounts := make([]model.MountSpec, 0, len(request.Mounts))
	for _, mountRequest := range request.Mounts {
		mounts = append(mounts, model.MountSpec{VolumeID: mountRequest.VolumeID, MountPath: mountRequest.MountPath})
	}
	if updateError := deployController.deployUseCase.UpdateMounts(echoContext.Request().Context(), userID, echoContext.Param("deployId"), mounts); updateError != nil {
		return updateError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// ApplyMounts は、POST /deploy/:deployId/mounts/apply のハンドラー。反映を始めて、すぐ応答する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除の途中・起動中、pending が無い、volume が不正なら 409。
func (deployController *DeployController) ApplyMounts(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 反映を始める。
	if applyError := deployController.deployUseCase.ApplyMounts(echoContext.Request().Context(), userID, echoContext.Param("deployId")); applyError != nil {
		return applyError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// DiscardMounts は、POST /deploy/:deployId/mounts/discard のハンドラー。pending を破棄する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除の途中、または pending が無ければ 409。
func (deployController *DeployController) DiscardMounts(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// pending を破棄する。
	if discardError := deployController.deployUseCase.DiscardMounts(echoContext.Request().Context(), userID, echoContext.Param("deployId")); discardError != nil {
		return discardError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// mountResponses は、JSON のマウントの構成を、レスポンスの形にする。読めないときは nil。
//
// 引数:
//   - raw: マウントの JSON の配列。
//
// 戻り値:
//   - レスポンスのマウントの一覧。
func mountResponses(raw []byte) []MountResponse {
	// 読み込む。読めなければ、空として扱う。
	mounts, parseError := model.ParseMounts(raw)
	if parseError != nil {
		return nil
	}
	// レスポンスの形にする。
	responses := make([]MountResponse, 0, len(mounts))
	for _, mount := range mounts {
		responses = append(responses, MountResponse{VolumeID: mount.VolumeID, MountPath: mount.MountPath})
	}
	return responses
}
