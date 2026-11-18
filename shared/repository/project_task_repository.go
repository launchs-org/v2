package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// taskResourceTables は、Workflow の入力に含まれる ID の項目名 → その ID を持つテーブル。テーブルの project_id から、プロジェクトを決める。
// 順番に調べる(ProjectID は、プロジェクト自身)。
var taskResourceTables = []struct{ field, table string }{
	{"ApplyID", "template_applies"},
	{"DeployID", "deploys"},
	{"ServiceID", "services"},
	{"IngressID", "ingresses"},
	{"VolumeID", "volumes"},
}

// TaskFilter は、タスクの一覧の絞り込み(空の項目は、絞り込まない)。
type TaskFilter struct {
	// Status は、状態(running、succeeded、failed、canceled、lost)。
	Status string

	// Kind は、種類。
	Kind string
}

// TaskCounts は、プロジェクトのタスクの数(上のバーの表示に使う)。
type TaskCounts struct {
	// Running は、実行中の数(lost は含まない)。
	Running int64

	// FailedRecent は、直近 24 時間の、失敗の数。
	FailedRecent int64
}

// ProjectTaskRepository は、プロジェクトのタスクの保存と読み出しを行う。
type ProjectTaskRepository struct {
	database *gorm.DB
}

// NewProjectTaskRepository は、ProjectTaskRepository を作る。
//
// 引数:
//   - database: 使うデータベース。
//
// 戻り値:
//   - 作成した ProjectTaskRepository。
func NewProjectTaskRepository(database *gorm.DB) *ProjectTaskRepository {
	// データベースを持たせて返す。
	return &ProjectTaskRepository{database: database}
}

// TaskTarget は、タスクの対象(Workflow の入力から決めたもの)。
type TaskTarget struct {
	// ProjectID は、対象のプロジェクトの ID。
	ProjectID string

	// UserID は、プロジェクトの持ち主のユーザー ID。
	UserID string

	// ProjectName は、プロジェクトの名前。
	ProjectName string

	// ResourceID は、対象のリソースの ID(プロジェクト自身のときは、プロジェクトの ID)。
	ResourceID string
}

// ResolveProject は、Workflow の入力(項目名 → 値)から、対象のプロジェクトと、対象のリソースの ID を決める。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - input: Workflow の入力を、項目名 → 値にしたもの(JSON にして読み直したもの)。
//
// 戻り値:
//   - 対象、見つかったか(リソースやプロジェクトがもう無いときは、見つからない)。
func (projectTaskRepository *ProjectTaskRepository) ResolveProject(requestContext context.Context, input map[string]any) (TaskTarget, bool, error) {
	// 対象のプロジェクトの ID と、リソースの ID を決める(プロジェクトの ID が入力にあれば、それ。無ければ、リソースのテーブルの project_id)。
	projectID, resourceID, found, resolveError := projectTaskRepository.resolveIDs(requestContext, input)
	if resolveError != nil || !found {
		return TaskTarget{}, false, resolveError
	}
	// プロジェクトの持ち主と名前を引く(プロジェクトが無ければ、記録しない)。
	var projects []model.Project
	if findError := projectTaskRepository.database.WithContext(requestContext).Where("id = ?", projectID).Limit(1).Find(&projects).Error; findError != nil {
		return TaskTarget{}, false, apperror.Wrap(apperror.KindInternal, "タスクの対象のプロジェクトを取得できませんでした", findError)
	}
	if len(projects) == 0 {
		return TaskTarget{}, false, nil
	}
	return TaskTarget{ProjectID: projectID, UserID: projects[0].UserID, ProjectName: projects[0].Name, ResourceID: resourceID}, true, nil
}

// resolveIDs は、Workflow の入力から、プロジェクトの ID と、リソースの ID を決める。
func (projectTaskRepository *ProjectTaskRepository) resolveIDs(requestContext context.Context, input map[string]any) (string, string, bool, error) {
	// プロジェクトの ID が入力にあれば、それが対象。
	if projectID, _ := input["ProjectID"].(string); projectID != "" {
		return projectID, projectID, true, nil
	}
	// ID の項目を順に見て、そのテーブルの project_id を引く。
	for _, candidate := range taskResourceTables {
		resourceID, _ := input[candidate.field].(string)
		if resourceID == "" {
			continue
		}
		var projectIDs []string
		if findError := projectTaskRepository.database.WithContext(requestContext).Table(candidate.table).Where("id = ?", resourceID).Limit(1).Pluck("project_id", &projectIDs).Error; findError != nil {
			return "", "", false, apperror.Wrap(apperror.KindInternal, "タスクの対象のプロジェクトを取得できませんでした", findError)
		}
		if len(projectIDs) == 0 {
			return "", "", false, nil
		}
		return projectIDs[0], resourceID, true, nil
	}
	return "", "", false, nil
}

// Start は、タスクを実行中として保存する(同じ Workflow の実行は、二重に保存しない。再試行しても安全)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - task: 保存するタスク(ID、状態、開始の日時を含む)。
//
// 戻り値:
//   - データベースのエラー。
func (projectTaskRepository *ProjectTaskRepository) Start(requestContext context.Context, task model.ProjectTask) error {
	// 同じ (workflow_id, run_id) があれば、何もしない。
	insertError := projectTaskRepository.database.WithContext(requestContext).Exec(
		`INSERT INTO project_tasks (id, project_id, user_id, project_name, kind, workflow_type, resource_id, workflow_id, run_id, parent_workflow_id, status, error, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?) ON CONFLICT (workflow_id, run_id) DO NOTHING`,
		task.ID, task.ProjectID, task.UserID, task.ProjectName, task.Kind, task.WorkflowType, task.ResourceID, task.WorkflowID, task.RunID, task.ParentWorkflowID, model.TaskStatusRunning, task.StartedAt).Error
	if insertError != nil {
		return apperror.Wrap(apperror.KindInternal, "タスクを保存できませんでした", insertError)
	}
	return nil
}

// RecordFinished は、すぐに終わる出来事(プロジェクトの作成など)を、終わったタスクとして、1 回で保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - task: 保存するタスク(ID、持ち主、種類、対象、状態、開始と終了の日時を含む)。
//
// 戻り値:
//   - データベースのエラー。
func (projectTaskRepository *ProjectTaskRepository) RecordFinished(requestContext context.Context, task model.ProjectTask) error {
	// 同じ (workflow_id, run_id) があれば、何もしない。
	insertError := projectTaskRepository.database.WithContext(requestContext).Exec(
		`INSERT INTO project_tasks (id, project_id, user_id, project_name, kind, workflow_type, resource_id, workflow_id, run_id, parent_workflow_id, status, error, started_at, finished_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?) ON CONFLICT (workflow_id, run_id) DO NOTHING`,
		task.ID, task.ProjectID, task.UserID, task.ProjectName, task.Kind, task.WorkflowType, task.ResourceID, task.WorkflowID, task.RunID, task.Status, task.Error, task.StartedAt, task.FinishedAt).Error
	if insertError != nil {
		return apperror.Wrap(apperror.KindInternal, "タスクを保存できませんでした", insertError)
	}
	return nil
}

// Finish は、実行中のタスクを、終わった状態にする(実行中でなければ、何もしない。再試行しても安全)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - workflowID: Workflow の ID。
//   - runID: 実行の ID。
//   - status: 終わった状態(succeeded、failed、canceled)。
//   - reason: 失敗の理由(整えたもの。成功なら空)。
//   - finishedAt: 終わった日時。
//
// 戻り値:
//   - データベースのエラー。
func (projectTaskRepository *ProjectTaskRepository) Finish(requestContext context.Context, workflowID string, runID string, status string, reason string, finishedAt time.Time) error {
	// 実行中のものだけを、更新する。
	updateError := projectTaskRepository.database.WithContext(requestContext).Exec(
		"UPDATE project_tasks SET status = ?, error = ?, finished_at = ? WHERE workflow_id = ? AND run_id = ? AND status = ?",
		status, reason, finishedAt, workflowID, runID, model.TaskStatusRunning).Error
	if updateError != nil {
		return apperror.Wrap(apperror.KindInternal, "タスクの結果を保存できませんでした", updateError)
	}
	return nil
}

// ensureProjectOwned は、プロジェクトが、ユーザーのものかを確かめる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//
// 戻り値:
//   - 無い、または他のユーザーのものなら not_found。
func (projectTaskRepository *ProjectTaskRepository) ensureProjectOwned(requestContext context.Context, userID string, projectID string) error {
	// 条件の値が空だと、Gorm が条件を無視するので、先に拒否する。
	if hasEmpty(userID, projectID) {
		return apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	var count int64
	if countError := projectTaskRepository.database.WithContext(requestContext).Model(&model.Project{}).Where(&model.Project{ID: projectID, UserID: userID}).Count(&count).Error; countError != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", countError)
	}
	if count == 0 {
		return apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	return nil
}

// applyListFilter は、一覧の検索に、絞り込みと、ページの位置を足す。
//
// 引数:
//   - query: トップのタスクを、新しい順に並べた検索。
//   - filter: 絞り込み。
//   - after: 前のページの最後の位置。最初は nil。
//   - now: 現在の日時(lost の判定に使う)。
//
// 戻り値:
//   - 足した検索。絞り込みが不正なら invalid_request。
func applyListFilter(query *gorm.DB, filter TaskFilter, after *pagination.Position, now time.Time) (*gorm.DB, error) {
	// ページの位置、種類、状態の順に足す。
	if after != nil {
		query = query.Where("(started_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}
	if filter.Kind != "" {
		query = query.Where("kind = ?", filter.Kind)
	}
	staleBefore := now.Add(-model.TaskStaleAfter)
	switch filter.Status {
	case "":
	case model.TaskStatusRunning:
		query = query.Where("status = ? AND started_at >= ?", model.TaskStatusRunning, staleBefore)
	case model.TaskStatusLost:
		query = query.Where("status = ? AND started_at < ?", model.TaskStatusRunning, staleBefore)
	case model.TaskStatusSucceeded, model.TaskStatusFailed, model.TaskStatusCanceled:
		query = query.Where("status = ?", filter.Status)
	default:
		return nil, apperror.New(apperror.KindInvalidRequest, "status は running、succeeded、failed、canceled、lost のどれかにしてください")
	}
	return query, nil
}

// listPage は、検索を実行し、limit 件までと、次のページの位置を返す。
func listPage(query *gorm.DB, limit int) ([]model.ProjectTask, *pagination.Position, error) {
	// 次のページがあるかを知るため、1 件多く読む。
	var tasks []model.ProjectTask
	if findError := query.Order("started_at DESC, id DESC").Limit(limit + 1).Find(&tasks).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "タスクの一覧を取得できませんでした", findError)
	}
	// limit 件以下なら、次のページは無い。
	if len(tasks) <= limit {
		return tasks, nil, nil
	}
	tasks = tasks[:limit]
	last := tasks[limit-1]
	return tasks, &pagination.Position{CreatedAt: last.StartedAt, ID: last.ID}, nil
}

// List は、プロジェクトのトップのタスク(子の Workflow を除く)を、新しい順に返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - filter: 絞り込み。
//   - limit: 1 ページの件数。
//   - after: 前のページの最後の位置。最初は nil。
//   - now: 現在の日時(lost の判定に使う)。
//
// 戻り値:
//   - タスクと、次のページの位置(最後のページなら nil)。プロジェクトが無ければ not_found、絞り込みが不正なら invalid_request。
func (projectTaskRepository *ProjectTaskRepository) List(requestContext context.Context, userID string, projectID string, filter TaskFilter, limit int, after *pagination.Position, now time.Time) ([]model.ProjectTask, *pagination.Position, error) {
	// プロジェクトが、ユーザーのものかを確かめる。
	if ownedError := projectTaskRepository.ensureProjectOwned(requestContext, userID, projectID); ownedError != nil {
		return nil, nil, ownedError
	}
	query, filterError := applyListFilter(projectTaskRepository.database.WithContext(requestContext).Where("project_id = ? AND parent_workflow_id = ''", projectID), filter, after, now)
	if filterError != nil {
		return nil, nil, filterError
	}
	return listPage(query, limit)
}

// ListByUser は、ユーザーの全てのプロジェクトの、トップのタスクを、新しい順に返す(削除されたプロジェクトのタスクも含む)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - filter: 絞り込み。
//   - limit: 1 ページの件数。
//   - after: 前のページの最後の位置。最初は nil。
//   - now: 現在の日時(lost の判定に使う)。
//
// 戻り値:
//   - タスクと、次のページの位置(最後のページなら nil)。絞り込みが不正なら invalid_request。
func (projectTaskRepository *ProjectTaskRepository) ListByUser(requestContext context.Context, userID string, filter TaskFilter, limit int, after *pagination.Position, now time.Time) ([]model.ProjectTask, *pagination.Position, error) {
	// 条件の値が空だと、Gorm が条件を無視して他のユーザーの行が対象になるので、先に拒否する。
	if hasEmpty(userID) {
		return nil, nil, apperror.New(apperror.KindNotFound, "ユーザーが見つかりません")
	}
	query, filterError := applyListFilter(projectTaskRepository.database.WithContext(requestContext).Where("user_id = ? AND parent_workflow_id = ''", userID), filter, after, now)
	if filterError != nil {
		return nil, nil, filterError
	}
	return listPage(query, limit)
}

// Get は、タスク 1 件と、その子のタスク(開始の昇順)を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - taskID: タスクの ID。
//
// 戻り値:
//   - タスクと、子のタスク。無ければ not_found。
func (projectTaskRepository *ProjectTaskRepository) Get(requestContext context.Context, userID string, projectID string, taskID string) (model.ProjectTask, []model.ProjectTask, error) {
	// プロジェクトが、ユーザーのものかを確かめる。
	if ownedError := projectTaskRepository.ensureProjectOwned(requestContext, userID, projectID); ownedError != nil {
		return model.ProjectTask{}, nil, ownedError
	}
	if hasEmpty(taskID) {
		return model.ProjectTask{}, nil, apperror.New(apperror.KindNotFound, "タスクが見つかりません")
	}
	var task model.ProjectTask
	findError := projectTaskRepository.database.WithContext(requestContext).Where("id = ? AND project_id = ?", taskID, projectID).Take(&task).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.ProjectTask{}, nil, apperror.Wrap(apperror.KindNotFound, "タスクが見つかりません", findError)
	}
	if findError != nil {
		return model.ProjectTask{}, nil, apperror.Wrap(apperror.KindInternal, "タスクを取得できませんでした", findError)
	}
	var children []model.ProjectTask
	if childError := projectTaskRepository.database.WithContext(requestContext).Where("parent_workflow_id = ? AND project_id = ?", task.WorkflowID, projectID).Order("started_at ASC, id ASC").Find(&children).Error; childError != nil {
		return model.ProjectTask{}, nil, apperror.Wrap(apperror.KindInternal, "子のタスクを取得できませんでした", childError)
	}
	return task, children, nil
}

// Counts は、プロジェクトの、実行中のタスクの数と、直近 24 時間に失敗したタスクの数を返す(トップのタスクだけ)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - now: 現在の日時。
//
// 戻り値:
//   - 数。プロジェクトが無ければ not_found。
func (projectTaskRepository *ProjectTaskRepository) Counts(requestContext context.Context, userID string, projectID string, now time.Time) (TaskCounts, error) {
	// プロジェクトが、ユーザーのものかを確かめる。
	if ownedError := projectTaskRepository.ensureProjectOwned(requestContext, userID, projectID); ownedError != nil {
		return TaskCounts{}, ownedError
	}
	var counts TaskCounts
	row := projectTaskRepository.database.WithContext(requestContext).Raw(
		`SELECT count(*) FILTER (WHERE status = 'running' AND started_at >= ?),
		        count(*) FILTER (WHERE status = 'failed' AND started_at >= ?)
		   FROM project_tasks WHERE project_id = ? AND parent_workflow_id = ''`, now.Add(-model.TaskStaleAfter), now.Add(-24*time.Hour), projectID).Row()
	if scanError := row.Scan(&counts.Running, &counts.FailedRecent); scanError != nil {
		return TaskCounts{}, apperror.Wrap(apperror.KindInternal, "タスクの数を取得できませんでした", scanError)
	}
	return counts, nil
}

// CountsByUser は、ユーザーの全てのプロジェクトの、実行中のタスクの数と、直近 24 時間に失敗したタスクの数を返す(トップのタスクだけ)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - now: 現在の日時。
//
// 戻り値:
//   - 数。
func (projectTaskRepository *ProjectTaskRepository) CountsByUser(requestContext context.Context, userID string, now time.Time) (TaskCounts, error) {
	// 条件の値が空だと、Gorm が条件を無視するので、先に拒否する。
	if hasEmpty(userID) {
		return TaskCounts{}, apperror.New(apperror.KindNotFound, "ユーザーが見つかりません")
	}
	var counts TaskCounts
	row := projectTaskRepository.database.WithContext(requestContext).Raw(
		`SELECT count(*) FILTER (WHERE status = 'running' AND started_at >= ?),
		        count(*) FILTER (WHERE status = 'failed' AND started_at >= ?)
		   FROM project_tasks WHERE user_id = ? AND parent_workflow_id = ''`, now.Add(-model.TaskStaleAfter), now.Add(-24*time.Hour), userID).Row()
	if scanError := row.Scan(&counts.Running, &counts.FailedRecent); scanError != nil {
		return TaskCounts{}, apperror.Wrap(apperror.KindInternal, "タスクの数を取得できませんでした", scanError)
	}
	return counts, nil
}

// Prune は、保存期間を過ぎたタスクと、プロジェクトごとの件数の上限を超えた古いタスクを削除する(プロジェクトが削除されたあとのタスクは、残す。削除の履歴を見るため)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - cutoff: これより前に始まったタスクを消す。
//   - maxPerProject: プロジェクトごとに残す最大の件数(新しい順)。
//
// 戻り値:
//   - データベースのエラー。
func (projectTaskRepository *ProjectTaskRepository) Prune(requestContext context.Context, cutoff time.Time, maxPerProject int) error {
	// 期間、件数の順に消す。
	statements := []struct {
		sql       string
		arguments []any
	}{
		{"DELETE FROM project_tasks WHERE started_at < ?", []any{cutoff}},
		{`DELETE FROM project_tasks WHERE id IN (
		    SELECT id FROM (SELECT id, row_number() OVER (PARTITION BY project_id ORDER BY started_at DESC, id DESC) AS position FROM project_tasks) ranked
		    WHERE position > ?)`, []any{maxPerProject}},
	}
	for _, statement := range statements {
		if deleteError := projectTaskRepository.database.WithContext(requestContext).Exec(statement.sql, statement.arguments...).Error; deleteError != nil {
			return apperror.Wrap(apperror.KindInternal, "古いタスクを削除できませんでした", deleteError)
		}
	}
	return nil
}
