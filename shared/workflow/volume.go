package workflow

import (
	"time"

	"github.com/google/uuid"
)

// volume の Workflow と Activity の名前、入出力の型を定める。
// volume の作成は、データベースだけで終わる(PVC は、最初にマウントを適用するときに作る)。拡張の反映と削除に、Workflow を使う。

const (
	// ApplyVolumeSizeWorkflowName は、拡張の反映の Workflow の名前。
	ApplyVolumeSizeWorkflowName = "ApplyVolumeSizeWorkflow"

	// ExpandVolumeClaimActivityName は、PVC を拡張して、反映されるのを待つ Activity の名前。
	ExpandVolumeClaimActivityName = "ExpandVolumeClaim"

	// CommitVolumeSizeResultActivityName は、拡張の結果を DB に確定する Activity の名前。
	CommitVolumeSizeResultActivityName = "CommitVolumeSizeResult"

	// ApplyVolumeSizeWorkflowTimeout は、拡張の反映の Workflow 全体の期限。
	ApplyVolumeSizeWorkflowTimeout = 15 * time.Minute

	// VolumeExpandTimeout は、PVC の拡張が反映される(容量が増える)のを待つ最長の時間。超えたら失敗にする。
	VolumeExpandTimeout = 5 * time.Minute

	// DeleteVolumeWorkflowName は、volume の削除の Workflow の名前。
	DeleteVolumeWorkflowName = "DeleteVolumeWorkflow"

	// DeleteVolumeClaimActivityName は、PVC を削除する Activity の名前。
	DeleteVolumeClaimActivityName = "DeleteVolumeClaim"

	// DeleteVolumeRecordActivityName は、volume のレコードを物理削除する Activity の名前。
	DeleteVolumeRecordActivityName = "DeleteVolumeRecord"

	// MarkVolumeDeleteFailedActivityName は、削除に失敗した volume を failed にする Activity の名前。
	MarkVolumeDeleteFailedActivityName = "MarkVolumeDeleteFailed"
)

// ApplyVolumeSizeInput は、拡張の反映の Workflow の入力。
type ApplyVolumeSizeInput struct {
	// VolumeID は、反映する volume の ID。
	VolumeID string
}

// ExpandVolumeClaimInput は、PVC を拡張する Activity の入力。
type ExpandVolumeClaimInput struct {
	// VolumeID は、対象の volume の ID。
	VolumeID string
}

// CommitVolumeSizeResultInput は、拡張の結果を確定する Activity の入力。
type CommitVolumeSizeResultInput struct {
	// VolumeID は、結果を確定する volume の ID。
	VolumeID string

	// Succeeded は、反映に成功したか。
	Succeeded bool
}

// DeleteVolumeInput は、volume の削除の Workflow の入力。
type DeleteVolumeInput struct {
	// VolumeID は、削除する volume の ID。
	VolumeID string
}

// DeleteVolumeClaimInput は、PVC を削除する Activity の入力。
type DeleteVolumeClaimInput struct {
	// VolumeID は、対象の volume の ID。
	VolumeID string
}

// DeleteVolumeRecordInput は、volume のレコードを削除する Activity の入力。
type DeleteVolumeRecordInput struct {
	// VolumeID は、対象の volume の ID。
	VolumeID string
}

// MarkVolumeDeleteFailedInput は、削除失敗を記録する Activity の入力。
type MarkVolumeDeleteFailedInput struct {
	// VolumeID は、対象の volume の ID。
	VolumeID string
}

// NewApplyVolumeSizeWorkflowID は、拡張の反映の Workflow の ID を作る(実行ごとに別の ID)。
//
// 引数:
//   - volumeID: 反映する volume の ID。
//
// 戻り値:
//   - Workflow の ID。
func NewApplyVolumeSizeWorkflowID(volumeID string) string {
	// volume の ID に、実行ごとの UUID を付ける。
	return "apply-size-" + volumeID + "-" + uuid.NewString()
}

// NewDeleteVolumeWorkflowID は、volume の削除の Workflow の ID を作る(実行ごとに別の ID)。
//
// 引数:
//   - volumeID: 削除する volume の ID。
//
// 戻り値:
//   - Workflow の ID。
func NewDeleteVolumeWorkflowID(volumeID string) string {
	// volume の ID に、実行ごとの UUID を付ける。
	return "delete-" + volumeID + "-" + uuid.NewString()
}
