package controller

import (
	"context"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/apperror"
	"launchs/shared/jst"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// ConfigMapUseCase は、ConfigMapController が使う、ConfigMap のユースケース。
type ConfigMapUseCase interface {
	Create(requestContext context.Context, userID string, projectID string, name string, files []model.ConfigMapFile) (model.ConfigMap, error)
	List(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.ConfigMapSummary, *pagination.Position, error)
	Get(requestContext context.Context, userID string, configMapID string, revisionNumber *int) (model.ConfigMap, model.ConfigMapRevision, []model.ConfigMapFile, error)
	ReplaceFiles(requestContext context.Context, userID string, configMapID string, files []model.ConfigMapFile) (bool, int, error)
	Rename(requestContext context.Context, userID string, configMapID string, name string) error
	Delete(requestContext context.Context, userID string, configMapID string) error
	ListRevisions(requestContext context.Context, userID string, configMapID string) ([]model.ConfigMapRevisionSummary, error)
	Usage(requestContext context.Context, userID string, configMapID string) ([]model.ConfigMapUsage, error)
}

// ConfigMapFileRequest は、ConfigMap のファイル1つのリクエスト(作成と置き換え)。内容の検査は、ユースケースで行う。
type ConfigMapFileRequest struct {
	// Key は、ファイル名。
	Key string `json:"key" validate:"required,max=253"`

	// Encoding は、内容の持ち方。text か base64。省略は text。
	Encoding string `json:"encoding"`

	// Content は、内容(text はそのままの文字列、base64 は base64 の文字列)。空でもよい。
	Content string `json:"content"`
}

// CreateConfigMapRequest は、ConfigMap の作成のリクエスト。名前の書式は、ユースケースで確かめる。
type CreateConfigMapRequest struct {
	// Name は、ConfigMap の名前。
	Name string `json:"name" validate:"required,max=64"`

	// Files は、最初のファイル(省略または空でもよい)。
	Files []ConfigMapFileRequest `json:"files" validate:"max=50,dive"`
}

// ReplaceConfigMapRequest は、ConfigMap のファイルの全件置き換えのリクエスト。
type ReplaceConfigMapRequest struct {
	// Files は、新しいファイルの全て(空は、全て外す)。必須(省略は、間違いなので拒否する)。
	Files *[]ConfigMapFileRequest `json:"files" validate:"required,max=50,dive"`
}

// RenameConfigMapRequest は、ConfigMap の名前の変更のリクエスト。
type RenameConfigMapRequest struct {
	// Name は、新しい名前。
	Name string `json:"name" validate:"required,max=64"`
}

// ConfigMapFileResponse は、ConfigMap のファイル1つのレスポンス。
type ConfigMapFileResponse struct {
	// Key は、ファイル名。
	Key string `json:"key"`

	// Encoding は、内容の持ち方(text か base64)。
	Encoding string `json:"encoding"`

	// Content は、内容。
	Content string `json:"content"`

	// Size は、元のバイト数(base64 はデコードした大きさ)。
	Size int `json:"size"`
}

// ConfigMapResponse は、ConfigMap 1件(版の中身つき)のレスポンス。
type ConfigMapResponse struct {
	// ConfigMapID は、ConfigMap の ID。
	ConfigMapID string `json:"configMapId"`

	// ProjectID は、属するプロジェクトの ID。
	ProjectID string `json:"projectId"`

	// Name は、名前。
	Name string `json:"name"`

	// CurrentRevision は、最新の版の番号。
	CurrentRevision int `json:"currentRevision"`

	// Revision は、files が、どの版の中身か。
	Revision int `json:"revision"`

	// Files は、その版のファイル(key の昇順)。
	Files []ConfigMapFileResponse `json:"files"`

	// CreatedAt は、作成日時(JST)。
	CreatedAt string `json:"createdAt"`

	// UpdatedAt は、更新日時(JST)。
	UpdatedAt string `json:"updatedAt"`
}

// ConfigMapSummaryResponse は、ConfigMap の一覧の1件(中身は含めない)。
type ConfigMapSummaryResponse struct {
	// ConfigMapID は、ConfigMap の ID。
	ConfigMapID string `json:"configMapId"`

	// Name は、名前。
	Name string `json:"name"`

	// CurrentRevision は、最新の版の番号。
	CurrentRevision int `json:"currentRevision"`

	// FileCount は、最新の版のファイルの数。
	FileCount int `json:"fileCount"`

	// TotalBytes は、最新の版の、保存する大きさの合計(バイト)。
	TotalBytes int `json:"totalBytes"`

	// UsedBy は、使っている deploy の数(pending のマウントも含む)。
	UsedBy int `json:"usedBy"`

	// UpdatedAt は、更新日時(JST)。
	UpdatedAt string `json:"updatedAt"`
}

// ConfigMapUsageResponse は、ConfigMap を使っている deploy のマウント1つ。
type ConfigMapUsageResponse struct {
	// DeployID は、使っている deploy の ID。
	DeployID string `json:"deployId"`

	// Revision は、使っている(固定している)版。
	Revision int `json:"revision"`

	// Key は、マウントしているファイル名。
	Key string `json:"key"`

	// Path は、マウント先の path。
	Path string `json:"path"`

	// Pending は、まだ適用していない(pending の)マウントなら true。
	Pending bool `json:"pending"`
}

// ListConfigMapsResponse は、ConfigMap の一覧のレスポンス。
type ListConfigMapsResponse struct {
	// ConfigMaps は、このページの ConfigMap。
	ConfigMaps []ConfigMapSummaryResponse `json:"configmaps"`

	// NextCursor は、次のページを取る位置。最後のページなら null。
	NextCursor *string `json:"nextCursor"`
}

// ConfigMapRevisionResponse は、版の一覧の1件。
type ConfigMapRevisionResponse struct {
	// Revision は、版の番号。
	Revision int `json:"revision"`

	// FileCount は、ファイルの数。
	FileCount int `json:"fileCount"`

	// TotalBytes は、保存する大きさの合計(バイト)。
	TotalBytes int `json:"totalBytes"`

	// CreatedAt は、この版を作った日時(JST)。
	CreatedAt string `json:"createdAt"`
}

// ConfigMapController は、ConfigMap の API のハンドラー。
type ConfigMapController struct {
	configMapUseCase ConfigMapUseCase
}

// NewConfigMapController は、ConfigMapController を作る。
//
// 引数:
//   - configMapUseCase: ConfigMap のユースケース。
//
// 戻り値:
//   - 作成した ConfigMapController。
func NewConfigMapController(configMapUseCase ConfigMapUseCase) *ConfigMapController {
	// ユースケースを持たせて返す。
	return &ConfigMapController{configMapUseCase: configMapUseCase}
}

// toConfigMapFiles は、リクエストのファイルを、モデルのファイルにする(encoding の省略は text)。
//
// 引数:
//   - requests: リクエストのファイル。
//
// 戻り値:
//   - モデルのファイル(nil でも、空の配列を返す)。
func toConfigMapFiles(requests []ConfigMapFileRequest) []model.ConfigMapFile {
	// 1つずつ写す。
	files := make([]model.ConfigMapFile, 0, len(requests))
	for _, request := range requests {
		// 省略された encoding は、text にする。
		encoding := request.Encoding
		if encoding == "" {
			encoding = model.ConfigMapEncodingText
		}
		files = append(files, model.ConfigMapFile{Key: request.Key, Encoding: encoding, Content: request.Content})
	}
	return files
}

// newConfigMapResponse は、ConfigMap と版の中身から、レスポンスを作る。
//
// 引数:
//   - configMap: ConfigMap。
//   - revision: 中身を返す版。
//   - files: その版のファイル。
//
// 戻り値:
//   - レスポンス。
func newConfigMapResponse(configMap model.ConfigMap, revision model.ConfigMapRevision, files []model.ConfigMapFile) ConfigMapResponse {
	// ファイルを、大きさつきで写す。
	fileResponses := make([]ConfigMapFileResponse, 0, len(files))
	for _, file := range files {
		fileResponses = append(fileResponses, ConfigMapFileResponse{Key: file.Key, Encoding: file.Encoding, Content: file.Content, Size: file.DecodedBytes()})
	}
	return ConfigMapResponse{
		ConfigMapID: configMap.ID, ProjectID: configMap.ProjectID, Name: configMap.Name,
		CurrentRevision: configMap.CurrentRevision, Revision: revision.Revision, Files: fileResponses,
		CreatedAt: jst.Format(configMap.CreatedAt), UpdatedAt: jst.Format(configMap.UpdatedAt),
	}
}

// Create は、POST /projects/:projectId/configmaps のハンドラー。ConfigMap を、版 1 で作る。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 作成した ConfigMap のレスポンス(200)。不正なら 400、プロジェクトが無ければ 404、削除中・名前の重複・数の上限は 409。
func (configMapController *ConfigMapController) Create(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request CreateConfigMapRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// ConfigMap を作る。
	configMap, createError := configMapController.configMapUseCase.Create(echoContext.Request().Context(), userID, echoContext.Param("projectId"), request.Name, toConfigMapFiles(request.Files))
	if createError != nil {
		return createError
	}
	// 作った版の中身を読み直して返す(画面が、そのまま使えるように)。
	return configMapController.respondWithConfigMap(echoContext, userID, configMap.ID, nil)
}

// List は、GET /projects/:projectId/configmaps のハンドラー。ConfigMap の一覧を、新しい順に返す(中身は含めない)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 一覧のレスポンス(200)。limit や cursor が不正なら 400、プロジェクトが無ければ 404。
func (configMapController *ConfigMapController) List(echoContext *echo.Context) error {
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
	summaries, nextPosition, listError := configMapController.configMapUseCase.List(echoContext.Request().Context(), userID, echoContext.Param("projectId"), limit, after)
	if listError != nil {
		return listError
	}
	// レスポンスにする。
	response := ListConfigMapsResponse{ConfigMaps: make([]ConfigMapSummaryResponse, 0, len(summaries))}
	for _, summary := range summaries {
		response.ConfigMaps = append(response.ConfigMaps, ConfigMapSummaryResponse{
			ConfigMapID: summary.ConfigMap.ID, Name: summary.ConfigMap.Name, CurrentRevision: summary.ConfigMap.CurrentRevision,
			FileCount: summary.FileCount, TotalBytes: summary.TotalBytes, UsedBy: summary.UsedBy, UpdatedAt: jst.Format(summary.ConfigMap.UpdatedAt),
		})
	}
	if nextPosition != nil {
		nextCursor := pagination.EncodeCursor(*nextPosition)
		response.NextCursor = &nextCursor
	}
	return echoContext.JSON(http.StatusOK, response)
}

// respondWithConfigMap は、ConfigMap の指定した版を読み、レスポンスとして返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//   - revisionNumber: 返す版の番号。nil なら最新。
//
// 戻り値:
//   - レスポンスの書き込み結果。読めなければ、そのエラー。
func (configMapController *ConfigMapController) respondWithConfigMap(echoContext *echo.Context, userID string, configMapID string, revisionNumber *int) error {
	// ConfigMap と版の中身を読む。
	configMap, revision, files, getError := configMapController.configMapUseCase.Get(echoContext.Request().Context(), userID, configMapID, revisionNumber)
	if getError != nil {
		return getError
	}
	return echoContext.JSON(http.StatusOK, newConfigMapResponse(configMap, revision, files))
}

// Get は、GET /configmaps/:configMapId のハンドラー。最新の版(?revision=N で、その版)の中身を返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - ConfigMap のレスポンス(200)。revision が不正なら 400、ConfigMap や版が無ければ 404。
func (configMapController *ConfigMapController) Get(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 版の指定があれば、1以上の整数として読む。
	var revisionNumber *int
	if revisionText := echoContext.QueryParam("revision"); revisionText != "" {
		parsed, parseError := strconv.Atoi(revisionText)
		if parseError != nil || parsed < 1 {
			return apperror.New(apperror.KindInvalidRequest, "revision は、1以上の整数でなければなりません")
		}
		revisionNumber = &parsed
	}
	// 読んで返す。
	return configMapController.respondWithConfigMap(echoContext, userID, echoContext.Param("configMapId"), revisionNumber)
}

// Replace は、PUT /configmaps/:configMapId のハンドラー。ファイルを全件置き換える(同じ中身なら、版を増やさない)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 操作の後の ConfigMap のレスポンス(200)。不正なら 400、無ければ 404。
func (configMapController *ConfigMapController) Replace(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request ReplaceConfigMapRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 置き換える(新しい版を作ったかどうかは、応答の revision から分かる)。
	if _, _, replaceError := configMapController.configMapUseCase.ReplaceFiles(echoContext.Request().Context(), userID, echoContext.Param("configMapId"), toConfigMapFiles(*request.Files)); replaceError != nil {
		return replaceError
	}
	// 操作の後の最新の版を返す。
	return configMapController.respondWithConfigMap(echoContext, userID, echoContext.Param("configMapId"), nil)
}

// Rename は、PATCH /configmaps/:configMapId のハンドラー。名前を変える。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。不正なら 400、無ければ 404、名前の重複は 409。
func (configMapController *ConfigMapController) Rename(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request RenameConfigMapRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 名前を変える。
	if renameError := configMapController.configMapUseCase.Rename(echoContext.Request().Context(), userID, echoContext.Param("configMapId"), request.Name); renameError != nil {
		return renameError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// Delete は、DELETE /configmaps/:configMapId のハンドラー。ConfigMap を、全ての版ごと削除する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404。
func (configMapController *ConfigMapController) Delete(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 削除する。
	if deleteError := configMapController.configMapUseCase.Delete(echoContext.Request().Context(), userID, echoContext.Param("configMapId")); deleteError != nil {
		return deleteError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// ListRevisions は、GET /configmaps/:configMapId/revisions のハンドラー。版の一覧を、新しい順に返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 版の一覧のレスポンス(200)。無ければ 404。
func (configMapController *ConfigMapController) ListRevisions(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 版の一覧を取得する。
	revisions, listError := configMapController.configMapUseCase.ListRevisions(echoContext.Request().Context(), userID, echoContext.Param("configMapId"))
	if listError != nil {
		return listError
	}
	// レスポンスにする。
	responses := make([]ConfigMapRevisionResponse, 0, len(revisions))
	for _, revision := range revisions {
		responses = append(responses, ConfigMapRevisionResponse{Revision: revision.Revision, FileCount: revision.FileCount, TotalBytes: revision.TotalBytes, CreatedAt: jst.Format(revision.CreatedAt)})
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"revisions": responses})
}

// Usage は、GET /configmaps/:configMapId/usage のハンドラー。ConfigMap を使っている deploy のマウントを返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 使っているマウントの一覧のレスポンス(200)。無ければ 404。
func (configMapController *ConfigMapController) Usage(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 使っているマウントを取得する。
	usages, usageError := configMapController.configMapUseCase.Usage(echoContext.Request().Context(), userID, echoContext.Param("configMapId"))
	if usageError != nil {
		return usageError
	}
	// レスポンスにする。
	responses := make([]ConfigMapUsageResponse, 0, len(usages))
	for _, usage := range usages {
		responses = append(responses, ConfigMapUsageResponse{DeployID: usage.DeployID, Revision: usage.Revision, Key: usage.Key, Path: usage.Path, Pending: usage.Pending})
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"usages": responses})
}
