package controller

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/model"
)

// ConfigMapMountRequest は、ConfigMap のマウント1つのリクエストの形。ID と key の形、path の書式、mode は、ユースケースで確かめる。
type ConfigMapMountRequest struct {
	// ConfigMapID は、マウントする ConfigMap の ID。
	ConfigMapID string `json:"configMapId" validate:"required,max=128"`

	// Revision は、固定する版。省略(0)なら、サーバーが決める(すでにマウントしていれば、その版。無ければ最新)。
	Revision int `json:"revision" validate:"min=0"`

	// Key は、ConfigMap の中のファイル名。
	Key string `json:"key" validate:"required,max=253"`

	// Path は、コンテナの中のファイルの path。
	Path string `json:"path" validate:"required,max=200"`

	// Mode は、権限(8進数の4桁の文字列)。省略は "0644"。
	Mode string `json:"mode" validate:"max=4"`
}

// UpdateConfigMapMountsRequest は、ConfigMap のマウント構成の更新(一括置き換え)のリクエスト。空の配列は、全て外す。
type UpdateConfigMapMountsRequest struct {
	// Mounts は、新しいマウントの全件。
	Mounts []ConfigMapMountRequest `json:"mounts" validate:"required,max=30,dive"`
}

// ConfigMapMountResponse は、レスポンスに含める、ConfigMap のマウント1つ。
type ConfigMapMountResponse struct {
	// ConfigMapID は、マウントしている ConfigMap の ID。
	ConfigMapID string `json:"configMapId"`

	// ConfigMapName は、ConfigMap の名前(取得のときだけ含める)。
	ConfigMapName *string `json:"configMapName,omitempty"`

	// Revision は、固定している版。
	Revision int `json:"revision"`

	// LatestRevision は、ConfigMap の今の最新の版(取得のときだけ含める。画面が「新しい版があります」と出す)。
	LatestRevision *int `json:"latestRevision,omitempty"`

	// Key は、ConfigMap の中のファイル名。
	Key string `json:"key"`

	// Path は、マウント先の path。
	Path string `json:"path"`

	// Mode は、権限(8進数の4桁の文字列)。
	Mode string `json:"mode"`
}

// ConfigMapMountsResponse は、ConfigMap のマウント構成のレスポンス。
type ConfigMapMountsResponse struct {
	// Mounts は、現在コンテナに反映されているマウント(無ければ空の配列)。
	Mounts []ConfigMapMountResponse `json:"mounts"`

	// PendingMounts は、まだ適用していないマウント。変更が無ければ null。
	PendingMounts []ConfigMapMountResponse `json:"pending_mounts"`
}

// configMapMountResponses は、JSON の ConfigMap のマウント構成を、レスポンスの形にする。
//
// 引数:
//   - raw: マウント構成の JSON の配列。
//   - configMaps: ConfigMap の ID から行への対応(最新の版と名前を付けるとき。nil なら付けない)。
//
// 戻り値:
//   - レスポンスのマウントの一覧(読めなければ nil)。
func configMapMountResponses(raw []byte, configMaps map[string]model.ConfigMap) []ConfigMapMountResponse {
	// 読み込む。読めなければ、空として扱う。
	mounts, parseError := model.ParseConfigMapMounts(raw)
	if parseError != nil {
		return nil
	}
	// レスポンスの形にする(ConfigMap が分かるときだけ、名前と最新の版を付ける)。
	responses := make([]ConfigMapMountResponse, 0, len(mounts))
	for _, mount := range model.SortConfigMapMounts(mounts) {
		response := ConfigMapMountResponse{ConfigMapID: mount.ConfigMapID, Revision: mount.Revision, Key: mount.Key, Path: mount.Path, Mode: mount.Mode}
		if configMap, found := configMaps[mount.ConfigMapID]; found {
			name, latest := configMap.Name, configMap.CurrentRevision
			response.ConfigMapName, response.LatestRevision = &name, &latest
		}
		responses = append(responses, response)
	}
	return responses
}

// newConfigMapMountsResponse は、deploy から、ConfigMap のマウント構成のレスポンスを作る(deploy の詳細にも、同じ形で入れる)。
//
// 引数:
//   - deploy: deploy のモデル。
//   - configMaps: ConfigMap の ID から行への対応(nil なら、名前と最新の版は付けない)。
//
// 戻り値:
//   - レスポンス。現在のマウントは、空でも配列。pending は、無ければ null(空の配列は、空の配列として返す)。
func newConfigMapMountsResponse(deploy model.Deploy, configMaps map[string]model.ConfigMap) ConfigMapMountsResponse {
	// 現在のマウントは、空でも配列にする。
	response := ConfigMapMountsResponse{Mounts: configMapMountResponses(deploy.ConfigMapMounts, configMaps)}
	if response.Mounts == nil {
		response.Mounts = []ConfigMapMountResponse{}
	}
	// pending は、あるときだけ入れる。
	if deploy.HasPendingConfigMapMounts() {
		response.PendingMounts = configMapMountResponses(deploy.PendingConfigMapMounts, configMaps)
		if response.PendingMounts == nil {
			response.PendingMounts = []ConfigMapMountResponse{}
		}
	}
	return response
}

// GetConfigMapMounts は、GET /deploy/:deployId/configmap-mounts のハンドラー。ConfigMap のマウント構成を、最新の版つきで返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - マウント構成のレスポンス(200)。無ければ 404。
func (deployController *DeployController) GetConfigMapMounts(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// deploy と、ConfigMap の最新の版を取得する。
	deploy, configMaps, getError := deployController.deployUseCase.GetConfigMapMounts(echoContext.Request().Context(), userID, echoContext.Param("deployId"))
	if getError != nil {
		return getError
	}
	return echoContext.JSON(http.StatusOK, newConfigMapMountsResponse(deploy, configMaps))
}

// UpdateConfigMapMounts は、PUT /deploy/:deployId/configmap-mounts のハンドラー。マウント構成を、pending として保存する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。不正なら 400、無ければ 404、反映中・削除の途中なら 409。
func (deployController *DeployController) UpdateConfigMapMounts(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request UpdateConfigMapMountsRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// モデルに変換して、保存する(書式と、ConfigMap の実在は、ユースケースで確かめる)。
	mounts := make([]model.ConfigMapMountSpec, 0, len(request.Mounts))
	for _, mountRequest := range request.Mounts {
		mounts = append(mounts, model.ConfigMapMountSpec{ConfigMapID: mountRequest.ConfigMapID, Revision: mountRequest.Revision, Key: mountRequest.Key, Path: mountRequest.Path, Mode: mountRequest.Mode})
	}
	if updateError := deployController.deployUseCase.UpdateConfigMapMounts(echoContext.Request().Context(), userID, echoContext.Param("deployId"), mounts); updateError != nil {
		return updateError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// ApplyConfigMapMounts は、POST /deploy/:deployId/configmap-mounts/apply のハンドラー。反映を始めて、すぐ応答する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除の途中・起動中、pending が無い、ConfigMap が不正、path が重なるなら 409。
func (deployController *DeployController) ApplyConfigMapMounts(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 反映を始める。
	if applyError := deployController.deployUseCase.ApplyConfigMapMounts(echoContext.Request().Context(), userID, echoContext.Param("deployId")); applyError != nil {
		return applyError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// DiscardConfigMapMounts は、POST /deploy/:deployId/configmap-mounts/discard のハンドラー。pending を破棄する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除の途中、または pending が無ければ 409。
func (deployController *DeployController) DiscardConfigMapMounts(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// pending を破棄する。
	if discardError := deployController.deployUseCase.DiscardConfigMapMounts(echoContext.Request().Context(), userID, echoContext.Param("deployId")); discardError != nil {
		return discardError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}
