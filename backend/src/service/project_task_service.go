package service

import (
	"context"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
	"launchs/shared/projecttask"
	"launchs/shared/repository"
)

// ProjectTaskStore は、ProjectTaskService が使う、タスクの読み出し。実体は repository.ProjectTaskRepository。
type ProjectTaskStore interface {
	// List は、プロジェクトのトップのタスクを、新しい順に返す。
	List(requestContext context.Context, userID string, projectID string, filter repository.TaskFilter, limit int, after *pagination.Position, now time.Time) ([]model.ProjectTask, *pagination.Position, error)

	// ListByUser は、ユーザーの全てのプロジェクトの、トップのタスクを、新しい順に返す(削除されたプロジェクトのタスクも含む)。
	ListByUser(requestContext context.Context, userID string, filter repository.TaskFilter, limit int, after *pagination.Position, now time.Time) ([]model.ProjectTask, *pagination.Position, error)

	// CountsByUser は、ユーザーの全てのプロジェクトの、実行中のタスクの数と、直近 24 時間に失敗したタスクの数を返す。
	CountsByUser(requestContext context.Context, userID string, now time.Time) (repository.TaskCounts, error)

	// Get は、タスク 1 件と、その子のタスクを返す。
	Get(requestContext context.Context, userID string, projectID string, taskID string) (model.ProjectTask, []model.ProjectTask, error)

	// Counts は、実行中のタスクの数と、直近 24 時間に失敗したタスクの数を返す。
	Counts(requestContext context.Context, userID string, projectID string, now time.Time) (repository.TaskCounts, error)
}

// ProjectTaskService は、プロジェクトのタスク(Workflow の履歴)を返すユースケース。
type ProjectTaskService struct {
	store ProjectTaskStore
	// now は、現在の時刻を返す関数(試験で差し替える)。
	now func() time.Time
}

// NewProjectTaskService は、ProjectTaskService を作る。
//
// 引数:
//   - store: タスクの読み出し。
//
// 戻り値:
//   - 作成した ProjectTaskService。
func NewProjectTaskService(store ProjectTaskStore) *ProjectTaskService {
	// 保存先と、実際の時計を持たせて返す。
	return &ProjectTaskService{store: store, now: time.Now}
}

// List は、プロジェクトのタスクを、新しい順に返す(トップのタスクだけ。子は、詳細で返す)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - filter: 絞り込み(種類が不正なら invalid_request)。
//   - limit: 1 ページの件数。
//   - after: 前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - タスク、次のページの位置、数(実行中と、直近の失敗)、現在の日時(状態の判定に使った値)。プロジェクトが無ければ not_found。
func (projectTaskService *ProjectTaskService) List(requestContext context.Context, userID string, projectID string, filter repository.TaskFilter, limit int, after *pagination.Position) ([]model.ProjectTask, *pagination.Position, repository.TaskCounts, time.Time, error) {
	// 種類を検査する。
	if filter.Kind != "" && !projecttask.Kinds()[filter.Kind] {
		return nil, nil, repository.TaskCounts{}, time.Time{}, apperror.New(apperror.KindInvalidRequest, "kind が正しくありません")
	}
	// 一覧と数を取る。
	current := projectTaskService.now()
	tasks, next, listError := projectTaskService.store.List(requestContext, userID, projectID, filter, limit, after, current)
	if listError != nil {
		return nil, nil, repository.TaskCounts{}, time.Time{}, listError
	}
	counts, countError := projectTaskService.store.Counts(requestContext, userID, projectID, current)
	if countError != nil {
		return nil, nil, repository.TaskCounts{}, time.Time{}, countError
	}
	return tasks, next, counts, current, nil
}

// ListAll は、ユーザーの全てのプロジェクトの、タスクを、新しい順に返す(プロジェクト一覧の画面用。削除されたプロジェクトのタスクも含む)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - filter: 絞り込み(種類が不正なら invalid_request)。
//   - limit: 1 ページの件数。
//   - after: 前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - タスク、次のページの位置、数(実行中と、直近の失敗)、現在の日時。
func (projectTaskService *ProjectTaskService) ListAll(requestContext context.Context, userID string, filter repository.TaskFilter, limit int, after *pagination.Position) ([]model.ProjectTask, *pagination.Position, repository.TaskCounts, time.Time, error) {
	// 種類を検査する。
	if filter.Kind != "" && !projecttask.Kinds()[filter.Kind] {
		return nil, nil, repository.TaskCounts{}, time.Time{}, apperror.New(apperror.KindInvalidRequest, "kind が正しくありません")
	}
	// 一覧と数を取る。
	current := projectTaskService.now()
	tasks, next, listError := projectTaskService.store.ListByUser(requestContext, userID, filter, limit, after, current)
	if listError != nil {
		return nil, nil, repository.TaskCounts{}, time.Time{}, listError
	}
	counts, countError := projectTaskService.store.CountsByUser(requestContext, userID, current)
	if countError != nil {
		return nil, nil, repository.TaskCounts{}, time.Time{}, countError
	}
	return tasks, next, counts, current, nil
}

// Get は、タスク 1 件と、その子のタスクを返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - taskID: タスクの ID。
//
// 戻り値:
//   - タスク、子のタスク、現在の日時。無ければ not_found。
func (projectTaskService *ProjectTaskService) Get(requestContext context.Context, userID string, projectID string, taskID string) (model.ProjectTask, []model.ProjectTask, time.Time, error) {
	// 保存先に任せる。
	task, children, getError := projectTaskService.store.Get(requestContext, userID, projectID, taskID)
	return task, children, projectTaskService.now(), getError
}
