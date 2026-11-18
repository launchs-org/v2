package workflow

import "github.com/google/uuid"

const (
	// DeleteDeployWorkflowName は、deploy を削除する Workflow の名前。
	DeleteDeployWorkflowName = "DeleteDeployWorkflow"

	// DeleteDeploymentActivityName は、deploy に対応する k8s の Deployment を削除する Activity の名前。
	DeleteDeploymentActivityName = "DeleteDeployment"

	// DeleteDeployRecordActivityName は、deploy を DB から削除する Activity の名前。
	DeleteDeployRecordActivityName = "DeleteDeployRecord"

	// MarkDeployDeleteFailedActivityName は、削除に失敗した deploy を delete_failed にする Activity の名前。
	MarkDeployDeleteFailedActivityName = "MarkDeployDeleteFailed"
)

// DeleteDeployInput は、DeleteDeployWorkflow の入力。
type DeleteDeployInput struct {
	// DeployID は、削除する deploy の ID。属するプロジェクト(namespace)は、Activity が DB から読む。
	DeployID string
}

// DeleteDeploymentInput は、DeleteDeployment Activity の入力。
type DeleteDeploymentInput struct {
	// DeployID は、Deployment を削除する deploy の ID。
	DeployID string
}

// DeleteDeployRecordInput は、DeleteDeployRecord Activity の入力。
type DeleteDeployRecordInput struct {
	// DeployID は、DB から削除する deploy の ID。
	DeployID string
}

// MarkDeployDeleteFailedInput は、MarkDeployDeleteFailed Activity の入力。
type MarkDeployDeleteFailedInput struct {
	// DeployID は、削除に失敗した deploy の ID。
	DeployID string
}

// NewDeleteDeployWorkflowID は、deploy 削除の Workflow ID を作る。形式は `{操作}-{リソースID}-{uuid}`。
//
// 引数:
//   - deployID: 削除する deploy の ID。
//
// 戻り値:
//   - Workflow ID(例: delete-deploy-xxxxxxxx-...-yyyyyyyy-...)。
func NewDeleteDeployWorkflowID(deployID string) string {
	// 操作ごとに一意にするため、末尾に UUID を付ける。
	return "delete-" + deployID + "-" + uuid.NewString()
}
