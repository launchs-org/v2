package workflow

import (
	"time"

	"github.com/google/uuid"
)

const (
	// ApplyDeployFilesWorkflowName は、deploy のファイル(pending)を k8s に反映する Workflow の名前。
	ApplyDeployFilesWorkflowName = "ApplyDeployFilesWorkflow"

	// ApplyPendingFilesActivityName は、ConfigMap を作って、pending のファイルで Deployment を更新する Activity の名前。
	ApplyPendingFilesActivityName = "ApplyPendingFiles"

	// RollbackDeploymentFilesActivityName は、反映に失敗したとき、ConfigMap と Deployment を現在のファイルに戻す Activity の名前。
	RollbackDeploymentFilesActivityName = "RollbackDeploymentFiles"

	// CommitFilesResultActivityName は、ファイルの反映の結果を DB に確定する Activity の名前。
	CommitFilesResultActivityName = "CommitFilesResult"

	// ApplyDeployFilesWorkflowTimeout は、ファイルの反映の Workflow 全体の最長の実行時間(仮)。
	ApplyDeployFilesWorkflowTimeout = 15 * time.Minute
)

// ApplyDeployFilesInput は、ファイルの反映の Workflow の入力。
type ApplyDeployFilesInput struct {
	// DeployID は、反映する deploy の ID。
	DeployID string
}

// ApplyPendingFilesInput は、Deployment を pending のファイルで更新する Activity の入力。
type ApplyPendingFilesInput struct {
	// DeployID は、対象の deploy の ID。
	DeployID string
}

// RollbackDeploymentFilesInput は、ConfigMap と Deployment を現在のファイルに戻す Activity の入力。
type RollbackDeploymentFilesInput struct {
	// DeployID は、対象の deploy の ID。
	DeployID string
}

// CommitFilesResultInput は、ファイルの反映の結果を確定する Activity の入力。
type CommitFilesResultInput struct {
	// DeployID は、結果を確定する deploy の ID。
	DeployID string

	// Succeeded は、反映に成功したか。
	Succeeded bool
}

// NewApplyDeployFilesWorkflowID は、ファイルの反映の Workflow ID を作る(実行ごとに別の ID)。
//
// 引数:
//   - deployID: 反映する deploy の ID。
//
// 戻り値:
//   - Workflow の ID。
func NewApplyDeployFilesWorkflowID(deployID string) string {
	// deploy の ID に、実行ごとの UUID を付ける。
	return "apply-files-" + deployID + "-" + uuid.NewString()
}
