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
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	var request ProjectNameRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}

	projectID, createError := projectController.projectUseCase.Create(echoContext.Request().Context(), userID, request.Name)
	if createError != nil {
		return createError
	}
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
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
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

	projects, nextPosition, listError := projectController.projectUseCase.List(echoContext.Request().Context(), userID, limit, after)
	if listError != nil {
		return listError
	}

	response := ListProjectsResponse{Projects: make([]ProjectResponse, 0, len(projects))}
	for _, project := range projects {
		response.Projects = append(response.Projects, newProjectResponse(project))
	}
	if nextPosition != nil {
		nextCursor := pagination.EncodeCursor(*nextPosition)
		response.NextCursor = &nextCursor
	}
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
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}

	project, getError := projectController.projectUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("projectId"))
	if getError != nil {
		return getError
	}
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
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	var request ProjectNameRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}

	renameError := projectController.projectUseCase.Rename(echoContext.Request().Context(), userID, echoContext.Param("projectId"), request.Name)
	if renameError != nil {
		return renameError
	}
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
	return ProjectResponse{ProjectID: project.ID, Name: project.Name, Status: project.Status}
}
