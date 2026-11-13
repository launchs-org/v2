package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// ProjectUseCase は、ProjectController が呼ぶ、プロジェクトのビジネスルール。
//
// 実体は service の ProjectService。テストでは fake に差し替える。
type ProjectUseCase interface {
	Create(requestContext context.Context, userID string, name string) (string, error)
	Get(requestContext context.Context, userID string, projectID string) (model.Project, error)
	List(requestContext context.Context, userID string, limit int, after *pagination.Position) ([]model.Project, *pagination.Position, error)
	Rename(requestContext context.Context, userID string, projectID string, newName string) error
}

// ProjectNameRequest は、プロジェクトの作成・更新のリクエストのボディ。
type ProjectNameRequest struct {
	// Name は、プロジェクトの名前。英数字とハイフンのみ、1〜64文字。
	Name string `json:"name" validate:"required,min=1,max=64,resourcename"`
}

// CreateProjectResponse は、プロジェクト作成のレスポンスのボディ。
type CreateProjectResponse struct {
	// ProjectID は、作成したプロジェクトの ID。
	ProjectID string `json:"projectId"`
}

// ProjectResponse は、プロジェクト1件のレスポンスのボディ。一覧の要素にも使う。
type ProjectResponse struct {
	// ProjectID は、プロジェクトの ID。
	ProjectID string `json:"projectId"`

	// Name は、プロジェクトの名前。
	Name string `json:"name"`

	// Status は、プロジェクトの状態("active" または "deleting")。
	Status string `json:"status"`
}

// ListProjectsResponse は、プロジェクト一覧のレスポンスのボディ。
type ListProjectsResponse struct {
	// Projects は、プロジェクトの一覧(作成日時の新しい順)。
	Projects []ProjectResponse `json:"projects"`

	// NextCursor は、次のページを取るためのカーソル。最後のページなら null。
	NextCursor *string `json:"nextCursor"`
}

// ProjectController は、/projects の HTTP の入出力を担当する。
type ProjectController struct {
	projectUseCase ProjectUseCase
}

// NewProjectController は、ProjectController を作る。
//
// 引数:
//   - projectUseCase: プロジェクトのビジネスルール。
//
// 戻り値:
//   - 作成した ProjectController。
func NewProjectController(projectUseCase ProjectUseCase) *ProjectController {
	return &ProjectController{projectUseCase: projectUseCase}
}

// Create は、プロジェクトを作成する(POST /projects)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - ボディが不正なら invalid_request、名前の重複なら conflict、未認証なら unauthorized。
func (projectController *ProjectController) Create(echoContext *echo.Context) error {
	// 認証のミドルウェアが入れたユーザー ID を取り出す。無ければ未認証のエラーになる。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	// ユーザー ID を取れなければ、そのエラー(unauthorized)を返す。
	if userIDError != nil {
		// 未認証のエラーをそのまま返す。
		return userIDError
	}
	// ボディの読み込み先を用意する。
	var request ProjectNameRequest
	// ボディを読み込んで検証する。
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		// 不正なボディのエラー(invalid_request)を返す。
		return bindError
	}

	// ビジネスルールに任せて、プロジェクトを作成する。
	projectID, createError := projectController.projectUseCase.Create(echoContext.Request().Context(), userID, request.Name)
	// 作成に失敗(名前の重複など)したら、そのエラーを返す。
	if createError != nil {
		// 作成のエラーをそのまま返す。
		return createError
	}
	// 作成したプロジェクトの ID を、200 で返す。
	return echoContext.JSON(http.StatusOK, CreateProjectResponse{ProjectID: projectID})
}

// List は、プロジェクトの一覧を返す(GET /projects)。
//
// クエリの limit(1〜100、既定20)と cursor で、ページングする。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - limit や cursor が不正なら invalid_request、未認証なら unauthorized。
func (projectController *ProjectController) List(echoContext *echo.Context) error {
	// 認証のミドルウェアが入れたユーザー ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	// 取れなければ未認証のエラーを返す。
	if userIDError != nil {
		// 未認証のエラーをそのまま返す。
		return userIDError
	}
	// クエリの limit を読み取る。未指定なら既定値になり、範囲外なら失敗する。
	limit, limitError := pagination.ParseLimit(echoContext.QueryParam("limit"))
	// limit が不正なら、そのエラー(invalid_request)を返す。
	if limitError != nil {
		// limit のエラーをそのまま返す。
		return limitError
	}
	// cursor が無い(最初のページ)ときは nil のままにする。
	var after *pagination.Position
	// cursor が指定されているときだけ、位置に戻す。
	if cursorText := echoContext.QueryParam("cursor"); cursorText != "" {
		// cursor の文字列を、前のページの最後の位置に戻す。
		position, cursorError := pagination.DecodeCursor(cursorText)
		// cursor が不正なら、そのエラーを返す。
		if cursorError != nil {
			// cursor のエラーをそのまま返す。
			return cursorError
		}
		// 取り出した位置を、続きの取得の起点にする。
		after = &position
	}

	// ビジネスルールに任せて、1ページ分を取得する。
	projects, nextPosition, listError := projectController.projectUseCase.List(echoContext.Request().Context(), userID, limit, after)
	// 取得に失敗したら、そのエラーを返す。
	if listError != nil {
		// 取得のエラーをそのまま返す。
		return listError
	}

	// 一覧が空でも null ではなく [] で返すため、空のスライスで初期化する。
	response := ListProjectsResponse{Projects: make([]ProjectResponse, 0, len(projects))}
	// モデルを、1件ずつレスポンスの形にして並べる。
	for _, project := range projects {
		// 変換した1件をレスポンスに追加する。
		response.Projects = append(response.Projects, newProjectResponse(project))
	}
	// 次のページがあるときだけ、カーソルを作る。
	if nextPosition != nil {
		// 最後の行の位置を、文字列のカーソルにする。
		nextCursor := pagination.EncodeCursor(*nextPosition)
		// カーソルをレスポンスに入れる。最後のページでは nil のままで、JSON では null になる。
		response.NextCursor = &nextCursor
	}
	// 一覧を 200 で返す。
	return echoContext.JSON(http.StatusOK, response)
}

// Get は、プロジェクトを1件返す(GET /projects/:projectId)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い、または他のユーザーのものなら not_found、未認証なら unauthorized。
func (projectController *ProjectController) Get(echoContext *echo.Context) error {
	// 認証のミドルウェアが入れたユーザー ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	// 取れなければ未認証のエラーを返す。
	if userIDError != nil {
		// 未認証のエラーをそのまま返す。
		return userIDError
	}

	// パスの projectId のプロジェクトを、ログイン中のユーザーのものとして取得する。
	project, getError := projectController.projectUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("projectId"))
	// 無い、または他のユーザーのものなら、not_found のエラーを返す。
	if getError != nil {
		// 取得のエラーをそのまま返す。
		return getError
	}
	// 取得したプロジェクトを、レスポンスの形にして 200 で返す。
	return echoContext.JSON(http.StatusOK, newProjectResponse(project))
}

// Rename は、プロジェクトの名前を変える(PATCH /projects/:projectId)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - ボディが不正なら invalid_request、無い・他のユーザーのものなら not_found、
//     削除中・名前の重複なら conflict、未認証なら unauthorized。
func (projectController *ProjectController) Rename(echoContext *echo.Context) error {
	// 認証のミドルウェアが入れたユーザー ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	// 取れなければ未認証のエラーを返す。
	if userIDError != nil {
		// 未認証のエラーをそのまま返す。
		return userIDError
	}
	// 新しい名前の読み込み先を用意する。
	var request ProjectNameRequest
	// ボディを読み込んで検証する。
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		// 不正なボディのエラーを返す。
		return bindError
	}

	// ビジネスルールに任せて、名前を変更する。
	renameError := projectController.projectUseCase.Rename(echoContext.Request().Context(), userID, echoContext.Param("projectId"), request.Name)
	// 変更に失敗(無い、削除中、重複)したら、そのエラーを返す。
	if renameError != nil {
		// 変更のエラーをそのまま返す。
		return renameError
	}
	// 返す内容は無いので、空のオブジェクトを 200 で返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// newProjectResponse は、モデルを、レスポンスの形にする。
//
// 引数:
//   - project: プロジェクトのモデル。
//
// 戻り値:
//   - レスポンスのボディ。
func newProjectResponse(project model.Project) ProjectResponse {
	// モデルから、API に出す項目(ID、名前、状態)だけを取り出して詰める。
	return ProjectResponse{ProjectID: project.ID, Name: project.Name, Status: project.Status}
}
