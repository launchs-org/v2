// Package workflow は、バックエンドとワーカーが共有する、Temporal の Workflow と Activity の取り決めを持つ。
//
// 名前、入出力の型、Task Queue 名、タイムアウトの定数、Workflow ID の作り方を、
// ここに置く。実装は worker/src/workflow と worker/src/activity にある。
package workflow

import (
	"time"

	"github.com/google/uuid"
)

const (
	// TaskQueueGeneral は、ビルド以外の操作(削除、apply など)を処理する Task Queue の名前。
	TaskQueueGeneral = "general"

	// DeleteProjectWorkflowName は、プロジェクトを削除する Workflow の名前。
	DeleteProjectWorkflowName = "DeleteProjectWorkflow"

	// DeleteProjectNamespaceActivityName は、プロジェクトの k8s の namespace を削除する Activity の名前。
	DeleteProjectNamespaceActivityName = "DeleteProjectNamespace"

	// DeleteProjectRecordsActivityName は、プロジェクトと配下のレコードを DB から削除する Activity の名前。
	DeleteProjectRecordsActivityName = "DeleteProjectRecords"

	// MarkProjectDeleteFailedActivityName は、削除に失敗したプロジェクトを delete_failed にする Activity の名前。
	MarkProjectDeleteFailedActivityName = "MarkProjectDeleteFailed"

	// DeleteProjectRetryWindow は、削除の Activity を、失敗してもリトライし続ける最長の時間(具体値は仮)。
	// リトライの待ち時間も含めた、最初の開始から数えた時間。これを超えたら、Activity は失敗とし、
	// Workflow は、プロジェクトを delete_failed にして終わる。
	DeleteProjectRetryWindow = 15 * time.Minute

	// DeleteProjectActivityTimeout は、削除の Activity 1回あたりの最長の実行時間(具体値は仮)。
	// namespace の削除は、PVC のデータを含めて消えるのを待つので、長めにしてある。
	DeleteProjectActivityTimeout = 5 * time.Minute

	// DeleteProjectActivityHeartbeatTimeout は、Activity が生きていることを伝える間隔の上限。
	// これを超えて連絡が無いと、Activity は止まったとみなされ、リトライされる。
	DeleteProjectActivityHeartbeatTimeout = 30 * time.Second

	// DeleteProjectWorkflowTimeout は、削除の Workflow 全体の最長の実行時間(具体値は仮)。
	DeleteProjectWorkflowTimeout = 45 * time.Minute
)

// DeleteProjectInput は、DeleteProjectWorkflow の入力。
type DeleteProjectInput struct {
	// ProjectID は、削除するプロジェクトの ID。k8s の namespace の名前にも使われる。
	ProjectID string
}

// DeleteProjectNamespaceInput は、DeleteProjectNamespace Activity の入力。
type DeleteProjectNamespaceInput struct {
	// ProjectID は、namespace を削除するプロジェクトの ID(namespace の名前と同じ)。
	ProjectID string
}

// DeleteProjectRecordsInput は、DeleteProjectRecords Activity の入力。
type DeleteProjectRecordsInput struct {
	// ProjectID は、レコードを削除するプロジェクトの ID。
	ProjectID string
}

// MarkProjectDeleteFailedInput は、MarkProjectDeleteFailed Activity の入力。
type MarkProjectDeleteFailedInput struct {
	// ProjectID は、削除に失敗したプロジェクトの ID。
	ProjectID string
}

// NewDeleteProjectWorkflowID は、プロジェクト削除の Workflow ID を作る。
//
// 形式は `{操作}-{リソースID}-{uuid}`(ハイフン区切り)。ID から対象のリソースが分かる。
//
// 引数:
//   - projectID: 削除するプロジェクトの ID。
//
// 戻り値:
//   - Workflow ID(例: delete-project-project-xxxxxxxx-...-yyyyyyyy-...)。
func NewDeleteProjectWorkflowID(projectID string) string {
	// 操作ごとに一意にするため、末尾に UUID を付ける。
	return "delete-" + projectID + "-" + uuid.NewString()
}
