package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// VolumeUseCase は、VolumeController が使う、volume のユースケース。
type VolumeUseCase interface {
	Create(requestContext context.Context, userID string, projectID string, name string, sizeMB int) (model.Volume, error)
	List(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.Volume, *pagination.Position, error)
	Get(requestContext context.Context, userID string, projectID string, volumeID string) (model.Volume, []string, error)
	Rename(requestContext context.Context, userID string, projectID string, volumeID string, name string) error
	UpdateSize(requestContext context.Context, userID string, projectID string, volumeID string, sizeMB int) error
	DiscardSize(requestContext context.Context, userID string, projectID string, volumeID string) error
	ApplySize(requestContext context.Context, userID string, projectID string, volumeID string) error
	Delete(requestContext context.Context, userID string, projectID string, volumeID string) error
}

// CreateVolumeRequest は、volume の作成のリクエスト。名前の書式と大きさの上限は、ユースケースで確かめる。
type CreateVolumeRequest struct {
	// Name は、volume の名前。
	Name string `json:"name" validate:"required,max=64"`

	// SizeMB は、大きさ(MB)。
	SizeMB int `json:"sizeMB" validate:"required,min=1"`
}

// RenameVolumeRequest は、volume の名前の変更のリクエスト。
type RenameVolumeRequest struct {
	// Name は、新しい名前。
	Name string `json:"name" validate:"required,max=64"`
}

// ResizeVolumeRequest は、volume の拡張のリクエスト。
type ResizeVolumeRequest struct {
	// SizeMB は、新しい大きさ(MB)。
	SizeMB int `json:"sizeMB" validate:"required,min=1"`
}

// VolumeResponse は、volume 1件のレスポンス(作成、一覧、取得)。
type VolumeResponse struct {
	// Tags は、タグ(昇順。無ければ空の配列)。
	Tags []string `json:"tags"`

	// VolumeID は、volume の ID。
	VolumeID string `json:"volumeId"`

	// Name は、volume の名前。
	Name string `json:"name"`

	// Status は、状態(active、applying、failed、deleting)。
	Status string `json:"status"`

	// PendingSizeMB は、まだ適用していない大きさ。変更が無ければ null。
	PendingSizeMB *int `json:"pending_sizeMB"`

	// SizeMB は、現在有効な大きさ(MB)。
	SizeMB int `json:"sizeMB"`

	// MountedBy は、マウントしている deploy の ID。取得のときだけ含める。
	MountedBy *[]string `json:"mountedBy,omitempty"`
}

// ListVolumesResponse は、volume の一覧のレスポンス。
type ListVolumesResponse struct {
	// Volumes は、このページの volume。
	Volumes []VolumeResponse `json:"volumes"`

	// NextCursor は、次のページを取る位置。最後のページなら null。
	NextCursor *string `json:"nextCursor"`
}

// VolumeController は、volume の API のハンドラー。
type VolumeController struct {
	volumeUseCase VolumeUseCase
}

// NewVolumeController は、VolumeController を作る。
//
// 引数:
//   - volumeUseCase: volume のユースケース。
//
// 戻り値:
//   - 作成した VolumeController。
func NewVolumeController(volumeUseCase VolumeUseCase) *VolumeController {
	// ユースケースを持たせて返す。
	return &VolumeController{volumeUseCase: volumeUseCase}
}

// newVolumeResponse は、volume から、レスポンスを作る。
//
// 引数:
//   - volume: レスポンスにする volume。
//
// 戻り値:
//   - レスポンス(mountedBy は含めない)。
func newVolumeResponse(volume model.Volume) VolumeResponse {
	// 値を写す。
	return VolumeResponse{VolumeID: volume.ID, Tags: volume.TagList(), Name: volume.Name, Status: volume.Status, PendingSizeMB: volume.PendingSizeMB, SizeMB: volume.SizeMB}
}

// Create は、POST /projects/:projectId/volumes のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 作成した volume のレスポンス(200)。不正なら 400、プロジェクトが無ければ 404、削除中や名前の重複は 409。
func (volumeController *VolumeController) Create(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request CreateVolumeRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// volume を作る。
	volume, createError := volumeController.volumeUseCase.Create(echoContext.Request().Context(), userID, echoContext.Param("projectId"), request.Name, request.SizeMB)
	if createError != nil {
		return createError
	}
	return echoContext.JSON(http.StatusOK, newVolumeResponse(volume))
}

// List は、GET /projects/:projectId/volumes のハンドラー。volume の一覧を、新しい順に返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 一覧のレスポンス(200)。limit や cursor が不正なら 400、プロジェクトが無ければ 404。
func (volumeController *VolumeController) List(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// limit と cursor を読む。
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
	// 一覧を取得する。
	volumes, nextPosition, listError := volumeController.volumeUseCase.List(echoContext.Request().Context(), userID, echoContext.Param("projectId"), limit, after)
	if listError != nil {
		return listError
	}
	// レスポンスにする。
	response := ListVolumesResponse{Volumes: make([]VolumeResponse, 0, len(volumes))}
	for _, volume := range volumes {
		response.Volumes = append(response.Volumes, newVolumeResponse(volume))
	}
	if nextPosition != nil {
		nextCursor := pagination.EncodeCursor(*nextPosition)
		response.NextCursor = &nextCursor
	}
	return echoContext.JSON(http.StatusOK, response)
}

// Get は、GET /projects/:projectId/volumes/:volumeId のハンドラー。マウントしている deploy も返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - volume のレスポンス(200)。無ければ 404。
func (volumeController *VolumeController) Get(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// volume を取得する。
	volume, deployIDs, getError := volumeController.volumeUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("volumeId"))
	if getError != nil {
		return getError
	}
	// mountedBy を付けて返す(無ければ空の配列)。
	response := newVolumeResponse(volume)
	if deployIDs == nil {
		deployIDs = []string{}
	}
	response.MountedBy = &deployIDs
	return echoContext.JSON(http.StatusOK, response)
}

// Rename は、PATCH /projects/:projectId/volumes/:volumeId のハンドラー。名前を変える(即時反映)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。不正なら 400、無ければ 404、反映中・削除中や名前の重複は 409。
func (volumeController *VolumeController) Rename(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request RenameVolumeRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 名前を変える。
	if renameError := volumeController.volumeUseCase.Rename(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("volumeId"), request.Name); renameError != nil {
		return renameError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// UpdateSize は、PATCH /projects/:projectId/volumes/:volumeId/size のハンドラー。大きさを、pending として保存する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。縮小・上限超過なら 400、無ければ 404、反映中・削除中なら 409。
func (volumeController *VolumeController) UpdateSize(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request ResizeVolumeRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 保存する。
	if updateError := volumeController.volumeUseCase.UpdateSize(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("volumeId"), request.SizeMB); updateError != nil {
		return updateError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// ApplySize は、POST /projects/:projectId/volumes/:volumeId/apply のハンドラー。反映を始めて、すぐ応答する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除中、または pending が無ければ 409。
func (volumeController *VolumeController) ApplySize(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 反映を始める。
	if applyError := volumeController.volumeUseCase.ApplySize(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("volumeId")); applyError != nil {
		return applyError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// DiscardSize は、POST /projects/:projectId/volumes/:volumeId/discard のハンドラー。pending を破棄する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除中、または pending が無ければ 409。
func (volumeController *VolumeController) DiscardSize(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// pending を破棄する。
	if discardError := volumeController.volumeUseCase.DiscardSize(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("volumeId")); discardError != nil {
		return discardError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// Delete は、DELETE /projects/:projectId/volumes/:volumeId のハンドラー。削除を受け付けて、すぐ応答する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除中・マウント中なら 409。
func (volumeController *VolumeController) Delete(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 削除を受け付ける。
	if deleteError := volumeController.volumeUseCase.Delete(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("volumeId")); deleteError != nil {
		return deleteError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}
