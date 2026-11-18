package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/model"
	"launchs/shared/pagination"
	"launchs/shared/projecttask"
	"launchs/shared/repository"
)

// ProjectTaskUseCase は、ProjectTaskController が使う、タスクのユースケース。
type ProjectTaskUseCase interface {
	List(requestContext context.Context, userID string, projectID string, filter repository.TaskFilter, limit int, after *pagination.Position) ([]model.ProjectTask, *pagination.Position, repository.TaskCounts, time.Time, error)
	ListAll(requestContext context.Context, userID string, filter repository.TaskFilter, limit int, after *pagination.Position) ([]model.ProjectTask, *pagination.Position, repository.TaskCounts, time.Time, error)
	Get(requestContext context.Context, userID string, projectID string, taskID string) (model.ProjectTask, []model.ProjectTask, time.Time, error)
	ProgressFor(requestContext context.Context, tasks []model.ProjectTask, withSteps bool, runningOnly bool) map[string]projecttask.Progress
}

// ProjectTaskResponse は、タスク 1 件のレスポンス。
type ProjectTaskResponse struct {
	// TaskID は、タスクの ID。
	TaskID string `json:"taskId"`

	// ProjectID と ProjectName は、属するプロジェクトの ID と、記録したときの名前(プロジェクトが削除されたあとも、削除のタスクに出る)。
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`

	// Kind は、種類(deploy_create、ingress_apply など)。
	Kind string `json:"kind"`

	// ResourceID は、対象のリソースの ID(deploy、service、ingress、volume、テンプレートの適用、プロジェクトの ID)。
	ResourceID string `json:"resourceId"`

	// Status は、状態(running、succeeded、failed、canceled、lost)。
	Status string `json:"status"`

	// Error は、失敗の理由(失敗したときだけ。秘密を含みうる部分は伏せてある)。
	Error string `json:"error"`

	// StartedAt と FinishedAt は、開始と終了の日時(実行中は、終了が null)。
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`

	// DurationMs は、かかった時間(ミリ秒。実行中は、いままでの時間)。
	DurationMs int64 `json:"durationMs"`

	// WorkflowID は、Temporal の Workflow の ID(運用で、Temporal の画面を探すときに使う)。
	WorkflowID string `json:"workflowId"`

	// Progress は、進み具合(手順の総数と、終わった数、いま動いている手順。詳細では、手順の一覧も)。実行中のタスクと、詳細のときだけ。
	Progress *projecttask.Progress `json:"progress,omitempty"`

	// Children は、子の Workflow のタスク(詳細のときだけ)。
	Children []ProjectTaskResponse `json:"children,omitempty"`
}

// TaskCountsResponse は、タスクの数(上のバーの表示に使う)。
type TaskCountsResponse struct {
	// Running は、実行中の数。
	Running int64 `json:"running"`

	// FailedRecent は、直近 24 時間に失敗した数。
	FailedRecent int64 `json:"failedRecent"`
}

// ListProjectTasksResponse は、タスクの一覧のレスポンス。
type ListProjectTasksResponse struct {
	// Tasks は、このページのタスク(新しい順)。
	Tasks []ProjectTaskResponse `json:"tasks"`

	// NextCursor は、次のページを取る位置。最後のページなら null。
	NextCursor *string `json:"nextCursor"`

	// Counts は、プロジェクトのタスクの数。
	Counts TaskCountsResponse `json:"counts"`
}

// ProjectTaskController は、プロジェクトのタスクの API のハンドラー。
type ProjectTaskController struct {
	projectTaskUseCase ProjectTaskUseCase
}

// NewProjectTaskController は、ProjectTaskController を作る。
//
// 引数:
//   - projectTaskUseCase: タスクのユースケース。
//
// 戻り値:
//   - 作成した ProjectTaskController。
func NewProjectTaskController(projectTaskUseCase ProjectTaskUseCase) *ProjectTaskController {
	// ユースケースを持たせて返す。
	return &ProjectTaskController{projectTaskUseCase: projectTaskUseCase}
}

// newProjectTaskResponse は、タスクを、レスポンスの形にする。
//
// 引数:
//   - task: タスク。
//   - now: 現在の日時(状態と、実行中の時間の計算に使う)。
//
// 戻り値:
//   - レスポンス。
func newProjectTaskResponse(task model.ProjectTask, now time.Time) ProjectTaskResponse {
	// 終わっていれば、終わりまでの時間。実行中なら、いままでの時間。
	end := now
	if task.FinishedAt != nil {
		end = *task.FinishedAt
	}
	return ProjectTaskResponse{TaskID: task.ID, ProjectID: task.ProjectID, ProjectName: task.ProjectName, Kind: task.Kind, ResourceID: task.ResourceID, Status: task.EffectiveStatus(now), Error: task.Error,
		StartedAt: task.StartedAt, FinishedAt: task.FinishedAt, DurationMs: end.Sub(task.StartedAt).Milliseconds(), WorkflowID: task.WorkflowID}
}

// readListQuery は、一覧のクエリ(limit、cursor、status、kind)を読む。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 件数、次のページの位置、絞り込み。クエリが不正なら 400。
func readListQuery(echoContext *echo.Context) (int, *pagination.Position, repository.TaskFilter, error) {
	// limit と cursor を読む。
	limit, limitError := pagination.ParseLimit(echoContext.QueryParam("limit"))
	if limitError != nil {
		return 0, nil, repository.TaskFilter{}, limitError
	}
	var after *pagination.Position
	if cursorText := echoContext.QueryParam("cursor"); cursorText != "" {
		position, cursorError := pagination.DecodeCursor(cursorText)
		if cursorError != nil {
			return 0, nil, repository.TaskFilter{}, cursorError
		}
		after = &position
	}
	return limit, after, repository.TaskFilter{Status: echoContext.QueryParam("status"), Kind: echoContext.QueryParam("kind")}, nil
}

// listResponse は、タスクの一覧を、レスポンスの形にする。
func listResponse(tasks []model.ProjectTask, nextPosition *pagination.Position, counts repository.TaskCounts, now time.Time, progress map[string]projecttask.Progress) ListProjectTasksResponse {
	// 1 件ずつ、レスポンスにする(空でも、配列にする)。
	response := ListProjectTasksResponse{Tasks: make([]ProjectTaskResponse, 0, len(tasks)), Counts: TaskCountsResponse{Running: counts.Running, FailedRecent: counts.FailedRecent}}
	for _, task := range tasks {
		item := newProjectTaskResponse(task, now)
		if taskProgress, found := progress[task.ID]; found {
			item.Progress = &taskProgress
		}
		response.Tasks = append(response.Tasks, item)
	}
	if nextPosition != nil {
		nextCursor := pagination.EncodeCursor(*nextPosition)
		response.NextCursor = &nextCursor
	}
	return response
}

// List は、GET /projects/:projectId/tasks のハンドラー。プロジェクトのタスクを、新しい順に返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。クエリ limit、cursor、status、kind を読む。
//
// 戻り値:
//   - 一覧のレスポンス(200)。クエリが不正なら 400、プロジェクトが無ければ 404。
func (projectTaskController *ProjectTaskController) List(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	limit, after, filter, queryError := readListQuery(echoContext)
	if queryError != nil {
		return queryError
	}
	// 取得して、レスポンスにする。
	tasks, nextPosition, counts, now, listError := projectTaskController.projectTaskUseCase.List(echoContext.Request().Context(), userID, echoContext.Param("projectId"), filter, limit, after)
	if listError != nil {
		return listError
	}
	return echoContext.JSON(http.StatusOK, listResponse(tasks, nextPosition, counts, now, projectTaskController.projectTaskUseCase.ProgressFor(echoContext.Request().Context(), tasks, false, true)))
}

// ListAll は、GET /tasks のハンドラー。ユーザーの全てのプロジェクトのタスクを、新しい順に返す(削除されたプロジェクトのタスクも含む)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。クエリ limit、cursor、status、kind を読む。
//
// 戻り値:
//   - 一覧のレスポンス(200)。クエリが不正なら 400。
func (projectTaskController *ProjectTaskController) ListAll(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	limit, after, filter, queryError := readListQuery(echoContext)
	if queryError != nil {
		return queryError
	}
	// 取得して、レスポンスにする。
	tasks, nextPosition, counts, now, listError := projectTaskController.projectTaskUseCase.ListAll(echoContext.Request().Context(), userID, filter, limit, after)
	if listError != nil {
		return listError
	}
	return echoContext.JSON(http.StatusOK, listResponse(tasks, nextPosition, counts, now, projectTaskController.projectTaskUseCase.ProgressFor(echoContext.Request().Context(), tasks, false, true)))
}

// Get は、GET /projects/:projectId/tasks/:taskId のハンドラー。タスクと、その子のタスクを返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - タスクのレスポンス(200)。無ければ 404。
func (projectTaskController *ProjectTaskController) Get(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 取得して、レスポンスにする。
	task, children, now, getError := projectTaskController.projectTaskUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("taskId"))
	if getError != nil {
		return getError
	}
	// 本人と子の、進み具合(手順の一覧つき)を読み取る。
	progress := projectTaskController.projectTaskUseCase.ProgressFor(echoContext.Request().Context(), append([]model.ProjectTask{task}, children...), true, false)
	response := newProjectTaskResponse(task, now)
	if taskProgress, found := progress[task.ID]; found {
		response.Progress = &taskProgress
	}
	response.Children = make([]ProjectTaskResponse, 0, len(children))
	for _, child := range children {
		item := newProjectTaskResponse(child, now)
		if childProgress, found := progress[child.ID]; found {
			item.Progress = &childProgress
		}
		response.Children = append(response.Children, item)
	}
	return echoContext.JSON(http.StatusOK, response)
}
