package workflow

import (
	"time"

	"github.com/google/uuid"
)

const (
	// ApplyDeployConfigWorkflowName は、deploy の起動設定(pending)を k8s に反映する Workflow の名前。
	ApplyDeployConfigWorkflowName = "ApplyDeployConfigWorkflow"

	// ApplyPendingConfigActivityName は、pending の起動設定で k8s の Deployment を更新する Activity の名前。
	ApplyPendingConfigActivityName = "ApplyPendingConfig"

	// CommitConfigResultActivityName は、反映の結果を DB に確定する Activity の名前。
	// 正式な値を pending の内容で上書きし、pending を空に戻して、status を決める。
	CommitConfigResultActivityName = "CommitConfigResult"

	// ApplyDeployConfigWorkflowTimeout は、起動設定の反映の Workflow 全体の最長の実行時間(仮)。
	ApplyDeployConfigWorkflowTimeout = 15 * time.Minute
)

// ApplyDeployConfigInput は、ApplyDeployConfigWorkflow の入力。
type ApplyDeployConfigInput struct {
	// DeployID は、起動設定を反映する deploy の ID。内容は、Activity が DB から読む。
	DeployID string
}

// ApplyPendingConfigInput は、ApplyPendingConfig Activity の入力。
type ApplyPendingConfigInput struct {
	// DeployID は、Deployment を更新する deploy の ID。
	DeployID string
}

// CommitConfigResultInput は、CommitConfigResult Activity の入力。
type CommitConfigResultInput struct {
	// DeployID は、結果を確定する deploy の ID。
	DeployID string

	// Succeeded は、反映(再起動)に成功したか。true なら active(replicas が 0 なら scaled_zero)、false なら failed にする。
	Succeeded bool
}

// NewApplyDeployConfigWorkflowID は、起動設定の反映の Workflow ID を作る。
//
// 形式は `{操作}-{リソースID}-{uuid}`(ハイフン区切り)。
//
// 引数:
//   - deployID: 反映する deploy の ID。
//
// 戻り値:
//   - Workflow ID。
func NewApplyDeployConfigWorkflowID(deployID string) string {
	// 操作ごとに一意にするため、末尾に UUID を付ける。
	return "apply-config-" + deployID + "-" + uuid.NewString()
}
