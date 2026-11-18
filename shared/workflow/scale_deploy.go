package workflow

import (
	"time"

	"github.com/google/uuid"
)

const (
	// ScaleDeployWorkflowName は、deploy のレプリカ数を k8s に反映する Workflow の名前。
	ScaleDeployWorkflowName = "ScaleDeployWorkflow"

	// ApplyScaleActivityName は、DB の現在の値(新しい replicas を含む)で k8s の Deployment を更新する Activity の名前。
	ApplyScaleActivityName = "ApplyScale"

	// MarkScaleResultActivityName は、スケーリングの結果を DB に記録する Activity の名前。
	MarkScaleResultActivityName = "MarkScaleResult"

	// ScaleDeployWorkflowTimeout は、スケーリングの Workflow 全体の最長の実行時間(仮)。
	ScaleDeployWorkflowTimeout = 15 * time.Minute

	// MaxDeployReplicas は、1つの deploy に指定できるレプリカ数の上限(仮)。
	MaxDeployReplicas = 10
)

// ScaleDeployInput は、ScaleDeployWorkflow の入力。
type ScaleDeployInput struct {
	// DeployID は、スケーリングする deploy の ID。新しい replicas は、すでに DB に保存してある。
	DeployID string
}

// ApplyScaleInput は、ApplyScale Activity の入力。
type ApplyScaleInput struct {
	// DeployID は、Deployment を更新する deploy の ID。
	DeployID string
}

// MarkScaleResultInput は、MarkScaleResult Activity の入力。
type MarkScaleResultInput struct {
	// DeployID は、結果を記録する deploy の ID。
	DeployID string

	// Succeeded は、スケーリングに成功したか。true なら active(replicas が 0 なら scaled_zero)、false なら failed にする。
	Succeeded bool
}

// NewScaleDeployWorkflowID は、スケーリングの Workflow ID を作る。形式は `{操作}-{リソースID}-{uuid}`。
//
// 引数:
//   - deployID: スケーリングする deploy の ID。
//
// 戻り値:
//   - Workflow ID。
func NewScaleDeployWorkflowID(deployID string) string {
	// 操作ごとに一意にするため、末尾に UUID を付ける。
	return "scale-" + deployID + "-" + uuid.NewString()
}
