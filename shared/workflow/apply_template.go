package workflow

import "time"

// テンプレートから部品を作る処理(docs/templates.md 段階 C)の、Workflow と Activity の名前、入出力の型を定める。

const (
	// ApplyTemplateWorkflowName は、計画のステップを順に実行して、部品を作る Workflow の名前。
	ApplyTemplateWorkflowName = "ApplyTemplateWorkflow"

	// CreateTemplateVolumeActivityName 以下は、計画のステップを実行する Activity の名前(データベースの行を作る)。
	CreateTemplateVolumeActivityName    = "CreateTemplateVolume"
	CreateTemplateConfigMapActivityName = "CreateTemplateConfigMap"
	CreateTemplateDeployActivityName    = "CreateTemplateDeploy"
	CreateTemplateServiceActivityName   = "CreateTemplateService"
	CreateTemplateIngressActivityName   = "CreateTemplateIngress"

	// LoadTemplateStepsActivityName は、計画のステップの一覧(種類と name)を読む Activity の名前。
	LoadTemplateStepsActivityName = "LoadTemplateSteps"

	// UpdateTemplateStepActivityName は、ステップの進み具合を記録する Activity の名前。
	UpdateTemplateStepActivityName = "UpdateTemplateStep"

	// FinishTemplateApplyActivityName は、実行の結果を確定する Activity の名前。
	FinishTemplateApplyActivityName = "FinishTemplateApply"

	// ApplyTemplateWorkflowTimeout は、テンプレートの実行の Workflow 全体の期限(仮)。
	ApplyTemplateWorkflowTimeout = 1 * time.Hour
)

// ApplyTemplateInput は、テンプレートの実行の Workflow の入力。計画は、Activity が DB から読む。
type ApplyTemplateInput struct {
	// ApplyID は、実行の ID。
	ApplyID string
}

// TemplateStepInput は、ステップを実行する Activity の入力。
type TemplateStepInput struct {
	// ApplyID は、実行の ID。
	ApplyID string

	// Index は、ステップの位置(計画と同じ順)。
	Index int
}

// TemplateStepResult は、ステップを実行する Activity の結果。
type TemplateStepResult struct {
	// ResourceID は、作った(すでにあった)部品の ID。
	ResourceID string
}

// UpdateTemplateStepInput は、ステップの進み具合を記録する Activity の入力。
type UpdateTemplateStepInput struct {
	// ApplyID は、実行の ID。
	ApplyID string

	// Index は、ステップの位置。
	Index int

	// Status は、templateplan.StatusCreating などのどれか。
	Status string

	// ResourceID は、作った部品の ID(分かっていれば)。
	ResourceID string

	// Error は、失敗の理由(失敗のときだけ)。
	Error string
}

// TemplateStepRef は、計画のステップの、種類と name(Workflow が、順に実行するための目印。値は含まない)。
type TemplateStepRef struct {
	// Kind は、volume、configmap、deploy、service、ingress のどれか。
	Kind string

	// Name は、テンプレートの中の部品の name。
	Name string
}

// FinishTemplateApplyInput は、実行の結果を確定する Activity の入力。
type FinishTemplateApplyInput struct {
	// ApplyID は、実行の ID。
	ApplyID string

	// Succeeded は、全て作れたか。
	Succeeded bool

	// Error は、失敗の理由。
	Error string
}

// NewApplyTemplateWorkflowID は、テンプレートの実行の Workflow ID を作る(1 回の実行に、1 つだけ)。
//
// 引数:
//   - applyID: 実行の ID。
//
// 戻り値:
//   - Workflow の ID。
func NewApplyTemplateWorkflowID(applyID string) string {
	// 実行の ID が一意なので、そのまま使う(同じ実行を、二重に開始しない)。
	return "apply-template-" + applyID
}
