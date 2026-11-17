package controller

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/model"
)

// FileRequest は、ファイル1つのリクエストの形。path の書式と内容の大きさは、ユースケースで確かめる。
type FileRequest struct {
	// Path は、コンテナの中のファイルの path。
	Path string `json:"path" validate:"required,max=200"`

	// Content は、ファイルの内容(UTF-8 のテキスト。空でもよい)。
	Content string `json:"content"`
}

// UpdateFilesRequest は、ファイルの構成の更新(一括置き換え)のリクエスト。空の配列は、全て外す。
type UpdateFilesRequest struct {
	// Files は、新しいファイルの全件。
	Files []FileRequest `json:"files" validate:"required,max=20,dive"`
}

// FileResponse は、レスポンスに含める、ファイル1つ。
type FileResponse struct {
	// Path は、コンテナの中のファイルの path。
	Path string `json:"path"`

	// Content は、ファイルの内容。
	Content string `json:"content"`
}

// FilesResponse は、ファイルの構成のレスポンス。
type FilesResponse struct {
	// Files は、現在コンテナに反映されているファイル(無ければ空の配列)。
	Files []FileResponse `json:"files"`

	// PendingFiles は、まだ適用していないファイル。変更が無ければ null。
	PendingFiles []FileResponse `json:"pending_files"`
}

// newFilesResponse は、deploy から、ファイルの構成のレスポンスを作る(deploy の詳細にも、同じ形で入れる)。
//
// 引数:
//   - deploy: deploy のモデル。
//
// 戻り値:
//   - ファイルの構成のレスポンス。現在のファイルは、空でも配列。pending は、無ければ null(空の配列は、空の配列として返す)。
func newFilesResponse(deploy model.Deploy) FilesResponse {
	// 現在のファイルは、空でも配列にする。
	response := FilesResponse{Files: fileResponses(deploy.Files)}
	if response.Files == nil {
		response.Files = []FileResponse{}
	}
	// pending は、あるときだけ入れる。
	if deploy.HasPendingFiles() {
		response.PendingFiles = fileResponses(deploy.PendingFiles)
		if response.PendingFiles == nil {
			response.PendingFiles = []FileResponse{}
		}
	}
	return response
}

// GetFiles は、GET /deploy/:deployId/files のハンドラー。ファイルの構成を返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - ファイルの構成のレスポンス(200)。無ければ 404。
func (deployController *DeployController) GetFiles(echoContext *echo.Context) error {
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
	return echoContext.JSON(http.StatusOK, newFilesResponse(deploy))
}

// UpdateFiles は、PUT /deploy/:deployId/files のハンドラー。ファイルの構成を、pending として保存する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。不正なら 400、無ければ 404、反映中・削除の途中なら 409。
func (deployController *DeployController) UpdateFiles(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request UpdateFilesRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// ファイルに変換して、保存する(path の書式や内容は、ユースケースで確かめる)。
	files := make([]model.FileSpec, 0, len(request.Files))
	for _, fileRequest := range request.Files {
		files = append(files, model.FileSpec{Path: fileRequest.Path, Content: fileRequest.Content})
	}
	if updateError := deployController.deployUseCase.UpdateFiles(echoContext.Request().Context(), userID, echoContext.Param("deployId"), files); updateError != nil {
		return updateError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// ApplyFiles は、POST /deploy/:deployId/files/apply のハンドラー。反映を始めて、すぐ応答する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除の途中・起動中、pending が無い、マウントと重なるなら 409。
func (deployController *DeployController) ApplyFiles(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 反映を始める。
	if applyError := deployController.deployUseCase.ApplyFiles(echoContext.Request().Context(), userID, echoContext.Param("deployId")); applyError != nil {
		return applyError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// DiscardFiles は、POST /deploy/:deployId/files/discard のハンドラー。pending を破棄する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除の途中、または pending が無ければ 409。
func (deployController *DeployController) DiscardFiles(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// pending を破棄する。
	if discardError := deployController.deployUseCase.DiscardFiles(echoContext.Request().Context(), userID, echoContext.Param("deployId")); discardError != nil {
		return discardError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// fileResponses は、JSON のファイルの構成を、レスポンスの形にする。読めないときは nil。
//
// 引数:
//   - raw: ファイルの JSON の配列。
//
// 戻り値:
//   - レスポンスのファイルの一覧。
func fileResponses(raw []byte) []FileResponse {
	// 読み込む。読めなければ、空として扱う。
	files, parseError := model.ParseFiles(raw)
	if parseError != nil {
		return nil
	}
	// レスポンスの形にする。
	responses := make([]FileResponse, 0, len(files))
	for _, file := range files {
		responses = append(responses, FileResponse{Path: file.Path, Content: file.Content})
	}
	return responses
}
