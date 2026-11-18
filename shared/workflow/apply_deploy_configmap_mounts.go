package workflow

import (
	"time"

	"github.com/google/uuid"
)

const (
	// ApplyDeployConfigMapMountsWorkflowName は、deploy の ConfigMap のマウント(pending)を k8s に反映する Workflow の名前。
	ApplyDeployConfigMapMountsWorkflowName = "ApplyDeployConfigMapMountsWorkflow"

	// ApplyPendingConfigMapMountsActivityName は、版ごとの k8s の ConfigMap を作って、pending のマウントで Deployment を更新する Activity の名前。
	ApplyPendingConfigMapMountsActivityName = "ApplyPendingConfigMapMounts"

	// RollbackDeploymentConfigMapMountsActivityName は、反映に失敗したとき、Deployment を現在のマウントに戻す Activity の名前。
	RollbackDeploymentConfigMapMountsActivityName = "RollbackDeploymentConfigMapMounts"

	// CommitConfigMapMountsResultActivityName は、ConfigMap のマウントの反映の結果を DB に確定する Activity の名前。
	CommitConfigMapMountsResultActivityName = "CommitConfigMapMountsResult"

	// CleanupConfigMapRevisionsActivityName は、どの deploy も使っていない版の k8s の ConfigMap を削除する Activity の名前。
	CleanupConfigMapRevisionsActivityName = "CleanupConfigMapRevisions"

	// ApplyDeployConfigMapMountsWorkflowTimeout は、ConfigMap のマウントの反映の Workflow 全体の最長の実行時間(仮)。
	ApplyDeployConfigMapMountsWorkflowTimeout = 15 * time.Minute
)

// ApplyDeployConfigMapMountsInput は、ConfigMap のマウントの反映の Workflow の入力。
type ApplyDeployConfigMapMountsInput struct {
	// DeployID は、反映する deploy の ID。
	DeployID string
}

// ApplyPendingConfigMapMountsInput は、Deployment を pending の ConfigMap のマウントで更新する Activity の入力。
type ApplyPendingConfigMapMountsInput struct {
	// DeployID は、対象の deploy の ID。
	DeployID string
}

// RollbackDeploymentConfigMapMountsInput は、Deployment を現在の ConfigMap のマウントに戻す Activity の入力。
type RollbackDeploymentConfigMapMountsInput struct {
	// DeployID は、対象の deploy の ID。
	DeployID string
}

// CommitConfigMapMountsResultInput は、ConfigMap のマウントの反映の結果を確定する Activity の入力。
type CommitConfigMapMountsResultInput struct {
	// DeployID は、結果を確定する deploy の ID。
	DeployID string

	// Succeeded は、反映に成功したか。
	Succeeded bool
}

// CleanupConfigMapRevisionsInput は、使われていない版の k8s の ConfigMap を削除する Activity の入力。
type CleanupConfigMapRevisionsInput struct {
	// DeployID は、反映した deploy の ID(プロジェクトを知るために使う)。
	DeployID string
}

// NewApplyDeployConfigMapMountsWorkflowID は、ConfigMap のマウントの反映の Workflow ID を作る(実行ごとに別の ID)。
//
// 引数:
//   - deployID: 反映する deploy の ID。
//
// 戻り値:
//   - Workflow の ID。
func NewApplyDeployConfigMapMountsWorkflowID(deployID string) string {
	// deploy の ID に、実行ごとの UUID を付ける。
	return "apply-configmap-mounts-" + deployID + "-" + uuid.NewString()
}
