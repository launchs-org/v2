package model

import "time"

const (
	// ProjectTaskIDPrefix は、タスクの ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。
	ProjectTaskIDPrefix = "task-"

	// TaskStatusRunning は、実行中のタスク。
	TaskStatusRunning = "running"

	// TaskStatusSucceeded は、成功したタスク。
	TaskStatusSucceeded = "succeeded"

	// TaskStatusFailed は、失敗したタスク。
	TaskStatusFailed = "failed"

	// TaskStatusCanceled は、取り消されたタスク。
	TaskStatusCanceled = "canceled"

	// TaskStatusLost は、実行中のまま、終わりの記録が付かなかったタスク(worker が止まった、Workflow が期限で終わった、など)。保存はされず、読み出すときに決まる。
	TaskStatusLost = "lost"

	// TaskStaleAfter は、実行中のまま、これより長く経ったタスクを、lost として扱う長さ(Workflow の期限より長くしてある)。
	TaskStaleAfter = 2 * time.Hour
)

// ProjectTask は、project_tasks テーブルの 1 行。Temporal の Workflow 1 回に対応する。
type ProjectTask struct {
	// ID は、タスクの ID("task-" + UUID v4)。
	ID string `gorm:"column:id;primaryKey"`

	// ProjectID は、属するプロジェクトの ID。
	ProjectID string `gorm:"column:project_id"`

	// UserID は、持ち主のユーザー ID(プロジェクトが削除されても、削除のタスクを、持ち主が見られるように、控える)。
	UserID string `gorm:"column:user_id"`

	// ProjectName は、記録したときのプロジェクトの名前(プロジェクトが削除されたあとも、画面に出すため)。
	ProjectName string `gorm:"column:project_name"`

	// Kind は、種類(shared/projecttask の Kinds のどれか)。
	Kind string `gorm:"column:kind"`

	// WorkflowType は、Workflow の型の名前。
	WorkflowType string `gorm:"column:workflow_type"`

	// ResourceID は、対象のリソースの ID。
	ResourceID string `gorm:"column:resource_id"`

	// WorkflowID と RunID は、Temporal の Workflow の ID と、実行の ID。
	WorkflowID string `gorm:"column:workflow_id"`
	RunID      string `gorm:"column:run_id"`

	// ParentWorkflowID は、親の Workflow の ID(トップのタスクは空)。
	ParentWorkflowID string `gorm:"column:parent_workflow_id"`

	// Status は、保存されている状態(running、succeeded、failed、canceled)。
	Status string `gorm:"column:status"`

	// Error は、失敗の理由(整えたもの)。
	Error string `gorm:"column:error"`

	// StartedAt は、開始の日時。
	StartedAt time.Time `gorm:"column:started_at"`

	// FinishedAt は、終わった日時(実行中は nil)。
	FinishedAt *time.Time `gorm:"column:finished_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("project_tasks")。
func (ProjectTask) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "project_tasks"
}

// EffectiveStatus は、画面に出す状態を返す。実行中のまま、長く経ったものは lost にする。
//
// 引数:
//   - now: 現在の日時。
//
// 戻り値:
//   - 状態(running、succeeded、failed、canceled、lost のどれか)。
func (task ProjectTask) EffectiveStatus(now time.Time) string {
	// 実行中で、期限を過ぎていれば、終わりが記録されなかったものとして扱う。
	if task.Status == TaskStatusRunning && now.Sub(task.StartedAt) > TaskStaleAfter {
		return TaskStatusLost
	}
	return task.Status
}

// IsTopLevel は、トップのタスク(子の Workflow ではないもの)かを返す。
//
// 戻り値:
//   - 親が無ければ true。
func (task ProjectTask) IsTopLevel() bool {
	// 親の Workflow の ID が空なら、トップ。
	return task.ParentWorkflowID == ""
}
