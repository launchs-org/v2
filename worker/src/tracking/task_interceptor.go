// Package tracking は、worker が実行する Workflow を、プロジェクトのタスクとして記録する(docs/tasks.md)。
//
// Workflow の開始と終了のところに、interceptor で割り込む。各 Workflow のコードには、手を入れない。
// 記録の失敗は、Workflow を失敗させない(ログに残すだけ)。
package tracking

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"launchs/shared/model"
	"launchs/shared/projecttask"
	"launchs/shared/repository"
)

const (
	// trackingVersionID は、記録の処理を足した版の印。記録を足す前に始まった Workflow は、この印が無いので、記録せずに、そのまま再生する(再生の不一致を、起こさない)。
	trackingVersionID = "project-task-tracking"

	// recordTimeout は、記録の 1 回の上限の時間。
	recordTimeout = 10 * time.Second
)

// TaskStore は、タスクの保存先。実体は repository.ProjectTaskRepository。
type TaskStore interface {
	// ResolveProject は、Workflow の入力から、対象のプロジェクト(持ち主と名前を含む)と、リソースの ID を決める。
	ResolveProject(requestContext context.Context, input map[string]any) (repository.TaskTarget, bool, error)

	// Start は、タスクを実行中として保存する(同じ実行は二重にならない)。
	Start(requestContext context.Context, task model.ProjectTask) error

	// Finish は、実行中のタスクを、終わった状態にする。
	Finish(requestContext context.Context, workflowID string, runID string, status string, reason string, finishedAt time.Time) error
}

// startRecord は、開始の記録の入力(Workflow の履歴に残るので、JSON にできる形にする)。
type startRecord struct {
	Kind             string
	WorkflowType     string
	WorkflowID       string
	RunID            string
	ParentWorkflowID string
	Input            map[string]any
	StartedAt        time.Time
}

// finishRecord は、終わりの記録の入力。
type finishRecord struct {
	WorkflowID string
	RunID      string
	Status     string
	Reason     string
	FinishedAt time.Time
}

// TaskInterceptor は、Workflow の開始と終了を、プロジェクトのタスクとして記録する worker の interceptor。
type TaskInterceptor struct {
	interceptor.WorkerInterceptorBase
	store     TaskStore
	appLogger *slog.Logger
}

// NewTaskInterceptor は、TaskInterceptor を作る。
//
// 引数:
//   - store: タスクの保存先。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した TaskInterceptor(worker.Options の Interceptors に渡す)。
func NewTaskInterceptor(store TaskStore, appLogger *slog.Logger) *TaskInterceptor {
	// 受け取ったものを持たせて返す。
	return &TaskInterceptor{store: store, appLogger: appLogger}
}

// InterceptWorkflow は、Workflow の実行に割り込む interceptor を返す(Temporal が、Workflow ごとに呼ぶ)。
//
// 引数:
//   - workflowContext: 割り込み先の context。
//   - next: 次の interceptor。
//
// 戻り値:
//   - 記録の処理を持つ interceptor。
func (taskInterceptor *TaskInterceptor) InterceptWorkflow(workflowContext workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	// 次の interceptor に渡す、記録の処理を作る。
	return &workflowTracker{WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next}, owner: taskInterceptor}
}

// workflowTracker は、1 つの Workflow の実行を記録する。
type workflowTracker struct {
	interceptor.WorkflowInboundInterceptorBase
	owner *TaskInterceptor
}

// ExecuteWorkflow は、Workflow の本体を実行する前に、タスクの開始を記録し、終わったら、結果を記録する。
//
// 記録の対象は、種類が決まっている Workflow だけ。対象のリソースが無い(もう削除されている)ときも、記録しない。
//
// 引数:
//   - workflowContext: Workflow の context。
//   - input: Workflow の入力。
//
// 戻り値:
//   - Workflow 本体の結果とエラー(記録の失敗は、返さない)。
func (tracker *workflowTracker) ExecuteWorkflow(workflowContext workflow.Context, input *interceptor.ExecuteWorkflowInput) (interface{}, error) {
	// 記録の対象でない Workflow は、そのまま実行する。
	info := workflow.GetInfo(workflowContext)
	kind, tracked := projecttask.KindOf(info.WorkflowType.Name)
	if !tracked || workflow.GetVersion(workflowContext, trackingVersionID, workflow.DefaultVersion, 1) == workflow.DefaultVersion {
		return tracker.Next.ExecuteWorkflow(workflowContext, input)
	}
	// 開始を記録する(失敗しても、Workflow は続ける)。記録の対象にならなかった(リソースが無い)ときは、終わりも記録しない。
	parentWorkflowID := ""
	if info.ParentWorkflowExecution != nil {
		parentWorkflowID = info.ParentWorkflowExecution.ID
	}
	var recorded bool
	startContext := workflow.WithLocalActivityOptions(workflowContext, localOptions())
	startError := workflow.ExecuteLocalActivity(startContext, tracker.owner.recordStart, startRecord{
		Kind: kind, WorkflowType: info.WorkflowType.Name, WorkflowID: info.WorkflowExecution.ID, RunID: info.WorkflowExecution.RunID,
		ParentWorkflowID: parentWorkflowID, Input: inputAsMap(input.Args), StartedAt: workflow.Now(workflowContext),
	}).Get(workflowContext, &recorded)
	if startError != nil {
		recorded = false
	}
	// 本体を実行する。
	result, workflowError := tracker.Next.ExecuteWorkflow(workflowContext, input)
	if !recorded {
		return result, workflowError
	}
	// 終わりを記録する(取り消されたあとも実行できるよう、切り離した context で行う)。
	disconnectedContext, cancelDisconnected := workflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	finish := finishRecord{WorkflowID: info.WorkflowExecution.ID, RunID: info.WorkflowExecution.RunID, Status: model.TaskStatusSucceeded, FinishedAt: workflow.Now(workflowContext)}
	if workflowError != nil {
		finish.Status = model.TaskStatusFailed
		if temporal.IsCanceledError(workflowError) {
			finish.Status = model.TaskStatusCanceled
		}
		finish.Reason = projecttask.ErrorMessage(workflowError)
	}
	_ = workflow.ExecuteLocalActivity(workflow.WithLocalActivityOptions(disconnectedContext, localOptions()), tracker.owner.recordFinish, finish).Get(disconnectedContext, nil)
	return result, workflowError
}

// localOptions は、記録のローカルアクティビティの設定(短い時間で、数回だけやり直す)を返す。
func localOptions() workflow.LocalActivityOptions {
	// 時間と、やり直しの回数に、上限を付ける。
	return workflow.LocalActivityOptions{StartToCloseTimeout: recordTimeout, ScheduleToCloseTimeout: 3 * recordTimeout, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 3}}
}

// inputAsMap は、Workflow の入力(最初の引数の構造体)を、項目名 → 値の表にする。
//
// 引数:
//   - args: Workflow の引数。
//
// 戻り値:
//   - 項目名 → 値(入力が無い、または構造体でないときは、空の表)。
func inputAsMap(args []interface{}) map[string]any {
	// 最初の引数を、JSON にして、読み直す。
	result := map[string]any{}
	if len(args) == 0 {
		return result
	}
	encoded, marshalError := json.Marshal(args[0])
	if marshalError != nil {
		return result
	}
	_ = json.Unmarshal(encoded, &result)
	return result
}

// recordStart は、タスクの開始を保存する(ローカルアクティビティ)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - record: 開始の情報。
//
// 戻り値:
//   - 保存したか(対象のリソースが無ければ false)と、保存のエラー。
func (taskInterceptor *TaskInterceptor) recordStart(requestContext context.Context, record startRecord) (bool, error) {
	// 対象のプロジェクトを決める。無ければ、記録しない。
	target, found, resolveError := taskInterceptor.store.ResolveProject(requestContext, record.Input)
	if resolveError != nil {
		taskInterceptor.appLogger.Warn("タスクの対象のプロジェクトを決められませんでした", "workflow_id", record.WorkflowID, "error", resolveError)
		return false, resolveError
	}
	if !found {
		return false, nil
	}
	// タスクを、実行中として保存する。
	saveError := taskInterceptor.store.Start(requestContext, model.ProjectTask{
		ID: model.ProjectTaskIDPrefix + uuid.NewString(), ProjectID: target.ProjectID, UserID: target.UserID, ProjectName: target.ProjectName, Kind: record.Kind, WorkflowType: record.WorkflowType, ResourceID: target.ResourceID,
		WorkflowID: record.WorkflowID, RunID: record.RunID, ParentWorkflowID: record.ParentWorkflowID, StartedAt: record.StartedAt,
	})
	if saveError != nil {
		taskInterceptor.appLogger.Warn("タスクを保存できませんでした", "workflow_id", record.WorkflowID, "error", saveError)
		return false, saveError
	}
	return true, nil
}

// recordFinish は、タスクの結果を保存する(ローカルアクティビティ)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - record: 終わりの情報。
//
// 戻り値:
//   - 保存のエラー。
func (taskInterceptor *TaskInterceptor) recordFinish(requestContext context.Context, record finishRecord) error {
	// 結果を保存する。失敗は、ログに残す。
	finishError := taskInterceptor.store.Finish(requestContext, record.WorkflowID, record.RunID, record.Status, record.Reason, record.FinishedAt)
	if finishError != nil {
		taskInterceptor.appLogger.Warn("タスクの結果を保存できませんでした", "workflow_id", record.WorkflowID, "error", finishError)
	}
	return finishError
}
