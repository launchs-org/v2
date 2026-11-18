package projecttask

import "launchs/shared/workflow"

// Step は、Workflow の手順(Activity)1 つの定義。画面の進行表示に使う。
type Step struct {
	// Activity は、Activity の名前(shared/workflow の ...ActivityName)。
	Activity string

	// Label は、画面に出す名前。
	Label string
}

// steps は、Workflow の型の名前 → 手順(成功したときに、順に通る Activity)。
// 失敗のときだけ呼ばれる後始末(Rollback…、Mark…Failed)は、含めない(動いたときだけ、追加の手順として出る)。
// ここに無い Workflow は、手順の総数が決まらない(進行は、動いた Activity の一覧だけを出す)。
var steps = map[string][]Step{
	workflow.CreateDeployWorkflowName: {
		{workflow.EnsureProjectNamespaceActivityName, "namespace を用意"}, {workflow.PrepareDeployDependenciesActivityName, "volume と ConfigMap を用意"},
		{workflow.ApplyDeploymentActivityName, "Deployment を作成"}, {workflow.WaitDeploymentReadyActivityName, "起動を待つ"}, {workflow.MarkDeployResultActivityName, "結果を記録"},
	},
	workflow.DeleteDeployWorkflowName: {
		{workflow.DeleteDeployServiceActivityName, "service を削除"}, {workflow.DeleteDeploymentActivityName, "Deployment を削除"}, {workflow.DeleteDeployRecordActivityName, "記録を削除"},
	},
	workflow.ApplyDeployConfigWorkflowName: {
		{workflow.ApplyPendingConfigActivityName, "起動設定を適用"}, {workflow.WaitDeploymentReadyActivityName, "起動を待つ"}, {workflow.CommitConfigResultActivityName, "結果を確定"},
	},
	workflow.ApplyDeployFilesWorkflowName: {
		{workflow.ApplyPendingFilesActivityName, "ファイルを適用"}, {workflow.WaitDeploymentReadyActivityName, "起動を待つ"}, {workflow.CommitFilesResultActivityName, "結果を確定"},
	},
	workflow.ApplyDeployMountsWorkflowName: {
		{workflow.ApplyPendingMountsActivityName, "volume のマウントを適用"}, {workflow.WaitDeploymentReadyActivityName, "起動を待つ"}, {workflow.CommitMountsResultActivityName, "結果を確定"},
	},
	workflow.ApplyDeployConfigMapMountsWorkflowName: {
		{workflow.ApplyPendingConfigMapMountsActivityName, "ConfigMap のマウントを適用"}, {workflow.WaitDeploymentReadyActivityName, "起動を待つ"},
		{workflow.CommitConfigMapMountsResultActivityName, "結果を確定"}, {workflow.CleanupConfigMapRevisionsActivityName, "古い版を片付ける"},
	},
	workflow.ScaleDeployWorkflowName: {
		{workflow.ApplyScaleActivityName, "レプリカ数を変更"}, {workflow.WaitDeploymentReadyActivityName, "起動を待つ"}, {workflow.MarkScaleResultActivityName, "結果を記録"},
	},
	workflow.StopCrashLoopWorkflowName: {
		{workflow.BeginCrashLoopStopActivityName, "停止を始める"}, {workflow.ScaleToZeroActivityName, "レプリカを 0 にする"}, {workflow.FinishCrashLoopStopActivityName, "結果を記録"},
	},
	workflow.CreateServiceWorkflowName: {
		{workflow.EnsureServiceActivityName, "Service を作成"}, {workflow.MarkServiceResultActivityName, "結果を記録"},
	},
	workflow.DeleteServiceWorkflowName: {
		{workflow.DeleteServiceObjectActivityName, "Service を削除"}, {workflow.DeleteServiceRecordActivityName, "記録を削除"},
	},
	workflow.ApplyServicePortsWorkflowName: {
		{workflow.ApplyServicePortsActivityName, "ポートを適用"}, {workflow.CommitServicePortsResultActivityName, "結果を確定"},
	},
	workflow.ApplyVolumeSizeWorkflowName: {
		{workflow.ExpandVolumeClaimActivityName, "volume を拡張"}, {workflow.CommitVolumeSizeResultActivityName, "結果を確定"},
	},
	workflow.DeleteVolumeWorkflowName: {
		{workflow.DeleteVolumeClaimActivityName, "PVC を削除"}, {workflow.DeleteVolumeRecordActivityName, "記録を削除"},
	},
	workflow.ApplyIngressRulesWorkflowName: {
		{workflow.ApplyIngressRulesActivityName, "IngressRoute を更新"}, {workflow.CommitIngressRulesResultActivityName, "結果を確定"},
	},
	workflow.ReapplyIngressRouteWorkflowName: {
		{workflow.ReapplyIngressRouteActivityName, "IngressRoute を作り直す"},
	},
	workflow.DeleteIngressWorkflowName: {
		{workflow.DeleteIngressObjectActivityName, "IngressRoute を削除"}, {workflow.DeleteIngressRecordActivityName, "記録を削除"},
	},
	workflow.DeleteProjectWorkflowName: {
		{workflow.DeleteProjectNamespaceActivityName, "namespace を削除"}, {workflow.DeleteProjectRecordsActivityName, "記録を削除"},
	},
}

// StepsOf は、Workflow の手順の定義を返す。
//
// 引数:
//   - workflowType: Workflow の型の名前。
//
// 戻り値:
//   - 手順(順番どおり)。定義が無ければ nil。
func StepsOf(workflowType string) []Step {
	// 表から引く。
	return steps[workflowType]
}

const (
	// StepDone は、終わった手順。
	StepDone = "done"

	// StepRunning は、いま動いている手順(やり直し中を含む)。
	StepRunning = "running"

	// StepFailed は、失敗した手順。
	StepFailed = "failed"

	// StepWaiting は、まだ始まっていない手順。
	StepWaiting = "waiting"
)

// ObservedActivity は、Temporal の履歴から読み取った、Activity の実行の様子(Activity の名前ごと)。
type ObservedActivity struct {
	// Name は、Activity の名前。
	Name string

	// Completed は、成功して終わった回数。
	Completed int

	// Failed は、最終的に失敗した(やり直しを使い切った、または、やり直さない)回数。
	Failed int

	// Pending は、いま動いている(または、やり直しを待っている)か。
	Pending bool

	// Attempt は、Pending のときの、何回目の試行か。
	Attempt int

	// Error は、直近の失敗の理由(整える前のもの)。
	Error string
}

// StepProgress は、画面に出す手順 1 つの進み具合。
type StepProgress struct {
	// Activity は、Activity の名前。
	Activity string `json:"activity"`

	// Label は、画面に出す名前。
	Label string `json:"label"`

	// State は、done、running、failed、waiting のどれか。
	State string `json:"state"`

	// Attempt は、動いている手順の、何回目の試行か(2 回目以降は、やり直し)。
	Attempt int `json:"attempt,omitempty"`

	// Error は、失敗の理由(失敗した手順と、やり直し中の手順だけ。整えてある)。
	Error string `json:"error,omitempty"`

	// Extra は、定義に無い手順(後始末の Rollback など)か。
	Extra bool `json:"extra,omitempty"`
}

// Progress は、タスクの進み具合。
type Progress struct {
	// Done は、終わった手順の数(定義した手順のうち)。
	Done int `json:"done"`

	// Total は、手順の総数(定義が無ければ 0。進行は、数ではなく、動いた手順の一覧で出す)。
	Total int `json:"total"`

	// Current は、いま動いている手順の名前(動いていなければ空)。
	Current string `json:"current"`

	// Steps は、手順の一覧(詳細のときだけ)。
	Steps []StepProgress `json:"steps,omitempty"`
}

// BuildProgress は、Workflow の手順の定義と、履歴から読み取った Activity の様子から、進み具合を組み立てる。
// 定義した手順は、順番どおりに、状態をつける。定義に無い Activity(後始末など)は、動いた・失敗したものだけを、後ろに足す。
//
// 引数:
//   - workflowType: Workflow の型の名前。
//   - observed: 履歴から読み取った、Activity の様子。
//
// 戻り値:
//   - 進み具合。
func BuildProgress(workflowType string, observed []ObservedActivity) Progress {
	// 名前で引けるようにする。
	byName := map[string]ObservedActivity{}
	for _, activity := range observed {
		byName[activity.Name] = activity
	}
	definition := StepsOf(workflowType)
	progress := Progress{Total: len(definition)}
	defined := map[string]bool{}
	// 定義した手順に、状態をつける。
	for _, step := range definition {
		defined[step.Activity] = true
		activity := byName[step.Activity]
		item := StepProgress{Activity: step.Activity, Label: step.Label, State: StepWaiting}
		switch {
		case activity.Pending:
			item.State, item.Attempt = StepRunning, activity.Attempt
			item.Error = Sanitize(activity.Error)
			if progress.Current == "" {
				progress.Current = step.Label
			}
		case activity.Failed > 0 && activity.Completed == 0:
			item.State, item.Error = StepFailed, Sanitize(activity.Error)
		case activity.Completed > 0:
			item.State = StepDone
			progress.Done++
		}
		progress.Steps = append(progress.Steps, item)
	}
	// 定義に無い Activity は、動いたものだけを、動いた順(名前の順)に足す。
	for _, activity := range observed {
		if defined[activity.Name] || (!activity.Pending && activity.Failed == 0 && activity.Completed == 0) {
			continue
		}
		item := StepProgress{Activity: activity.Name, Label: activity.Name, State: StepDone, Extra: true}
		switch {
		case activity.Pending:
			item.State, item.Attempt = StepRunning, activity.Attempt
			item.Error = Sanitize(activity.Error)
			if progress.Current == "" {
				progress.Current = activity.Name
			}
		case activity.Failed > 0 && activity.Completed == 0:
			item.State, item.Error = StepFailed, Sanitize(activity.Error)
		}
		progress.Steps = append(progress.Steps, item)
	}
	return progress
}
