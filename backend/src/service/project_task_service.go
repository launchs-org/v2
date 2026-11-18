package service

import (
	"context"
	"sync"
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

// ActivityObserver は、Workflow が動かした Activity の様子を、Temporal の履歴から読み取るもの。実体は TemporalActivityObserver。
type ActivityObserver interface {
	// Observe は、Activity ごとの様子(成功の回数、失敗、動いているか、何回目の試行か)を返す。
	Observe(requestContext context.Context, workflowID string, runID string) ([]projecttask.ObservedActivity, error)
}

// maxObservedTasks は、1 回の一覧で、進み具合を読み取る、実行中のタスクの数の上限(Temporal への問い合わせを、抑えるため)。
const maxObservedTasks = 10

// ProjectTaskService は、プロジェクトのタスク(Workflow の履歴)を返すユースケース。
type ProjectTaskService struct {
	store ProjectTaskStore
	// observer は、進み具合の読み取り。nil なら、進み具合を出さない。
	observer ActivityObserver
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

// WithActivityObserver は、タスクの進み具合を、Temporal の履歴から読み取るようにする。
//
// 引数:
//   - observer: 進み具合の読み取り(nil なら、進み具合を出さない)。
//
// 戻り値:
//   - 読み取りを持たせた ProjectTaskService(自分自身)。
func (projectTaskService *ProjectTaskService) WithActivityObserver(observer ActivityObserver) *ProjectTaskService {
	// 読み取りを持たせて返す(インターフェースの nil 値を、取り除く)。
	if observer != nil {
		projectTaskService.observer = observer
	}
	return projectTaskService
}

// ProgressFor は、タスクの進み具合を返す。
//
// 成功したタスクは、Temporal に問い合わせず、全ての手順が終わったものとして組み立てる。
// それ以外(実行中、失敗、取り消し、不明)は、Temporal の履歴から読み取る(履歴が無ければ、そのタスクの進み具合は無い)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - tasks: 進み具合を知りたいタスク。
//   - withSteps: 手順の一覧(Steps)を含めるか(詳細のとき true。一覧は false で、総数と、いま動いている手順だけ)。
//   - runningOnly: 実行中のタスクだけを対象にするか(一覧のとき true。件数は、maxObservedTasks まで)。
//
// 戻り値:
//   - タスクの ID → 進み具合。
func (projectTaskService *ProjectTaskService) ProgressFor(requestContext context.Context, tasks []model.ProjectTask, withSteps bool, runningOnly bool) map[string]projecttask.Progress {
	// 読み取りが無ければ、何も出さない。
	result := map[string]projecttask.Progress{}
	if projectTaskService.observer == nil {
		return result
	}
	now := projectTaskService.now()
	var mutex sync.Mutex
	var waitGroup sync.WaitGroup
	observed := 0
	for _, task := range tasks {
		// 定義の無い種類(プロジェクトの作成など)は、対象外。
		if task.WorkflowType == "" {
			continue
		}
		// 成功したタスクは、問い合わせずに、全て終わったものとする。
		if task.Status == model.TaskStatusSucceeded {
			if !runningOnly {
				result[task.ID] = succeededProgress(task.WorkflowType, withSteps)
			}
			continue
		}
		if runningOnly && task.EffectiveStatus(now) != model.TaskStatusRunning {
			continue
		}
		if runningOnly && observed >= maxObservedTasks {
			continue
		}
		observed++
		waitGroup.Add(1)
		go func(target model.ProjectTask) {
			defer waitGroup.Done()
			activities, observeError := projectTaskService.observer.Observe(requestContext, target.WorkflowID, target.RunID)
			if observeError != nil {
				return
			}
			progress := projecttask.BuildProgress(target.WorkflowType, activities)
			if !withSteps {
				progress.Steps = nil
			}
			mutex.Lock()
			result[target.ID] = progress
			mutex.Unlock()
		}(task)
	}
	waitGroup.Wait()
	return result
}

// succeededProgress は、成功したタスクの進み具合(全ての手順が終わったもの)を組み立てる。
func succeededProgress(workflowType string, withSteps bool) projecttask.Progress {
	// 定義した手順を、全て終わったものにする。
	definition := projecttask.StepsOf(workflowType)
	progress := projecttask.Progress{Done: len(definition), Total: len(definition)}
	if withSteps {
		for _, step := range definition {
			progress.Steps = append(progress.Steps, projecttask.StepProgress{Activity: step.Activity, Label: step.Label, State: projecttask.StepDone})
		}
	}
	return progress
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
