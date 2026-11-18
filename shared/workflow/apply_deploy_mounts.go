package workflow

import (
	"time"

	"github.com/google/uuid"
)

const (
	// ApplyDeployMountsWorkflowName は、deploy のボリュームのマウント(pending)を k8s に反映する Workflow の名前。
	ApplyDeployMountsWorkflowName = "ApplyDeployMountsWorkflow"

	// ApplyPendingMountsActivityName は、PVC を用意して、pending のマウントで Deployment を更新する Activity の名前。
	ApplyPendingMountsActivityName = "ApplyPendingMounts"

	// RollbackDeploymentMountsActivityName は、反映に失敗したとき、Deployment を現在のマウントに戻す Activity の名前。
	RollbackDeploymentMountsActivityName = "RollbackDeploymentMounts"

	// CommitMountsResultActivityName は、マウントの反映の結果を DB に確定する Activity の名前。
	CommitMountsResultActivityName = "CommitMountsResult"

	// ApplyDeployMountsWorkflowTimeout は、マウントの反映の Workflow 全体の最長の実行時間(仮)。
	ApplyDeployMountsWorkflowTimeout = 15 * time.Minute
)

// ApplyDeployMountsInput は、マウントの反映の Workflow の入力。
type ApplyDeployMountsInput struct {
	// DeployID は、反映する deploy の ID。
	DeployID string
}

// ApplyPendingMountsInput は、Deployment を pending のマウントで更新する Activity の入力。
type ApplyPendingMountsInput struct {
	// DeployID は、対象の deploy の ID。
	DeployID string
}

// RollbackDeploymentMountsInput は、Deployment を現在のマウントに戻す Activity の入力。
type RollbackDeploymentMountsInput struct {
	// DeployID は、対象の deploy の ID。
	DeployID string
}

// CommitMountsResultInput は、マウントの反映の結果を確定する Activity の入力。
type CommitMountsResultInput struct {
	// DeployID は、結果を確定する deploy の ID。
	DeployID string

	// Succeeded は、反映に成功したか。
	Succeeded bool
}

// NewApplyDeployMountsWorkflowID は、マウントの反映の Workflow ID を作る(実行ごとに別の ID)。
//
// 引数:
//   - deployID: 反映する deploy の ID。
//
// 戻り値:
//   - Workflow の ID。
func NewApplyDeployMountsWorkflowID(deployID string) string {
	// deploy の ID に、実行ごとの UUID を付ける。
	return "apply-mounts-" + deployID + "-" + uuid.NewString()
}
