package workflow

import (
	"time"

	"github.com/google/uuid"
)

// ingress の Workflow と Activity の名前、入出力の型を定める。
// ingress の作成は、データベースだけで終わる(Workflow は使わない)。ルールの反映と削除に、Workflow を使う。

const (
	// ApplyIngressRulesWorkflowName は、ルールの反映の Workflow の名前。
	ApplyIngressRulesWorkflowName = "ApplyIngressRulesWorkflow"

	// ApplyIngressRulesActivityName は、pending のルールで IngressRoute を更新する Activity の名前。
	ApplyIngressRulesActivityName = "ApplyIngressRules"

	// RollbackIngressRulesActivityName は、反映に失敗したとき、IngressRoute を現在のルールに戻す Activity の名前。
	RollbackIngressRulesActivityName = "RollbackIngressRules"

	// CommitIngressRulesResultActivityName は、反映の結果を DB に確定する Activity の名前。
	CommitIngressRulesResultActivityName = "CommitIngressRulesResult"

	// ApplyIngressRulesWorkflowTimeout は、ルールの反映の Workflow 全体の期限。
	ApplyIngressRulesWorkflowTimeout = 15 * time.Minute

	// ReapplyIngressRouteWorkflowName は、現在のルールのまま、IngressRoute を作り直す Workflow(アクセスの集計の切り替え)の名前。
	ReapplyIngressRouteWorkflowName = "ReapplyIngressRouteWorkflow"

	// ReapplyIngressRouteActivityName は、現在のルールで IngressRoute を反映する Activity の名前。
	ReapplyIngressRouteActivityName = "ReapplyIngressRoute"

	// DeleteIngressWorkflowName は、ingress の削除の Workflow の名前。
	DeleteIngressWorkflowName = "DeleteIngressWorkflow"

	// DeleteIngressObjectActivityName は、IngressRoute を削除する Activity の名前。
	DeleteIngressObjectActivityName = "DeleteIngressObject"

	// DeleteIngressRecordActivityName は、ingress のレコードを物理削除する Activity の名前。
	DeleteIngressRecordActivityName = "DeleteIngressRecord"

	// MarkIngressDeleteFailedActivityName は、削除に失敗した ingress を failed にする Activity の名前。
	MarkIngressDeleteFailedActivityName = "MarkIngressDeleteFailed"
)

// ApplyIngressRulesInput は、ルールの反映の Workflow の入力。
type ApplyIngressRulesInput struct {
	// IngressID は、反映する ingress の ID。
	IngressID string
}

// ApplyIngressRulesActivityInput は、IngressRoute を更新・巻き戻す Activity の入力。
type ApplyIngressRulesActivityInput struct {
	// IngressID は、対象の ingress の ID。
	IngressID string
}

// CommitIngressRulesResultInput は、反映の結果を確定する Activity の入力。
type CommitIngressRulesResultInput struct {
	// IngressID は、結果を確定する ingress の ID。
	IngressID string

	// Succeeded は、反映に成功したか。
	Succeeded bool
}

// ReapplyIngressRouteInput は、IngressRoute の作り直しの Workflow と Activity の入力。
type ReapplyIngressRouteInput struct {
	// IngressID は、対象の ingress の ID。
	IngressID string
}

// DeleteIngressInput は、ingress の削除の Workflow の入力。
type DeleteIngressInput struct {
	// IngressID は、削除する ingress の ID。
	IngressID string
}

// DeleteIngressObjectInput は、IngressRoute を削除する Activity の入力。
type DeleteIngressObjectInput struct {
	// IngressID は、対象の ingress の ID。
	IngressID string
}

// DeleteIngressRecordInput は、ingress のレコードを削除する Activity の入力。
type DeleteIngressRecordInput struct {
	// IngressID は、対象の ingress の ID。
	IngressID string
}

// MarkIngressDeleteFailedInput は、削除失敗を記録する Activity の入力。
type MarkIngressDeleteFailedInput struct {
	// IngressID は、対象の ingress の ID。
	IngressID string
}

// NewApplyIngressRulesWorkflowID は、ルールの反映の Workflow の ID を作る(実行ごとに別の ID)。
//
// 引数:
//   - ingressID: 反映する ingress の ID。
//
// 戻り値:
//   - Workflow の ID。
func NewApplyIngressRulesWorkflowID(ingressID string) string {
	// ingress の ID に、実行ごとの UUID を付ける。
	return "apply-rules-" + ingressID + "-" + uuid.NewString()
}

// NewDeleteIngressWorkflowID は、ingress の削除の Workflow の ID を作る(実行ごとに別の ID)。
//
// 引数:
//   - ingressID: 削除する ingress の ID。
//
// 戻り値:
//   - Workflow の ID。
func NewDeleteIngressWorkflowID(ingressID string) string {
	// ingress の ID に、実行ごとの UUID を付ける。
	return "delete-" + ingressID + "-" + uuid.NewString()
}

// NewReapplyIngressRouteWorkflowID は、IngressRoute の作り直しの Workflow の ID を作る(実行ごとに別の ID)。
//
// 引数:
//   - ingressID: 対象の ingress の ID。
//
// 戻り値:
//   - Workflow の ID。
func NewReapplyIngressRouteWorkflowID(ingressID string) string {
	// ingress の ID に、実行ごとの UUID を付ける。
	return "reapply-route-" + ingressID + "-" + uuid.NewString()
}
