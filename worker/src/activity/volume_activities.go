package activity

import (
	"context"
	"fmt"
	"log/slog"

	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// VolumeStore は、volume の Activity が使う、データベースの操作。
type VolumeStore interface {
	// FindForWorker は、持ち主を問わずに、volume を1件取得する。
	FindForWorker(requestContext context.Context, volumeID string) (model.Volume, error)

	// CommitSizeApply は、拡張の結果を確定する(applying の volume だけ)。確定できたかを返す。
	CommitSizeApply(requestContext context.Context, volumeID string, succeeded bool) (bool, error)

	// DeleteRecord は、削除中の volume を物理削除する。
	DeleteRecord(requestContext context.Context, volumeID string) error

	// MarkDeleteFailed は、削除中の volume を failed にする。
	MarkDeleteFailed(requestContext context.Context, volumeID string) error
}

// VolumeActivities は、volume の Workflow が使う Activity をまとめたもの。
type VolumeActivities struct {
	volumeStore    VolumeStore
	volumeOperator VolumeOperator
	appLogger      *slog.Logger
}

// NewVolumeActivities は、VolumeActivities を作る。
//
// 引数:
//   - volumeStore: データベースの操作。
//   - volumeOperator: k8s の PVC を操作するもの。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した VolumeActivities。
func NewVolumeActivities(volumeStore VolumeStore, volumeOperator VolumeOperator, appLogger *slog.Logger) *VolumeActivities {
	// 受け取ったものを持たせる。
	return &VolumeActivities{volumeStore: volumeStore, volumeOperator: volumeOperator, appLogger: appLogger}
}

// ExpandVolumeClaim は、pending の大きさで PVC を拡張し、容量が増えるのを待つ。
//
// PVC がまだ無い(マウントされていない)ときは、何もせず成功する(最初のマウントのとき、新しい大きさで作られる)。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 拡張する volume の ID。
//
// 戻り値:
//   - volume を読めなかったとき、適用する大きさが無いとき(リトライしない)、k8s への依頼に失敗したとき、時間切れのとき(リトライしない)のエラー。
func (volumeActivities *VolumeActivities) ExpandVolumeClaim(requestContext context.Context, input workflow.ExpandVolumeClaimInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	volumeActivities.appLogger.Info("pending の大きさで PVC を拡張する", "volume_id", input.VolumeID, "activity", workflow.ExpandVolumeClaimActivityName)
	// DB から volume を読む。
	volume, findError := volumeActivities.volumeStore.FindForWorker(requestContext, input.VolumeID)
	if findError != nil {
		return findError
	}
	// 適用する大きさが無いのは、何度やっても同じなので、リトライしない。
	if volume.PendingSizeMB == nil {
		return temporal.NewNonRetryableApplicationError("適用する大きさがありません", "NoPendingSize", nil)
	}
	// 拡張と、容量が増えるまでの待機を、k8s を操作するものに任せる(待っている間は、生きていることを伝える)。
	definition := VolumeClaimDefinition{Namespace: volume.ProjectID, Name: volume.ID, SizeMB: *volume.PendingSizeMB}
	expandError := volumeActivities.volumeOperator.ExpandVolumeClaim(requestContext, definition, func() {
		sdkactivity.RecordHeartbeat(requestContext)
	})
	return markNonRetryable(expandError)
}

// CommitVolumeSizeResult は、拡張の結果を DB に確定する。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 結果を確定する volume の ID と、成功したか。
//
// 戻り値:
//   - 確定できなかったときのデータベースのエラー。
func (volumeActivities *VolumeActivities) CommitVolumeSizeResult(requestContext context.Context, input workflow.CommitVolumeSizeResultInput) error {
	// 結果をログに残す(失敗は、運用で気づけるよう警告にする)。
	if input.Succeeded {
		volumeActivities.appLogger.Info("volume の拡張に成功した", "volume_id", input.VolumeID, "activity", workflow.CommitVolumeSizeResultActivityName)
	} else {
		volumeActivities.appLogger.Warn("volume の拡張に失敗した", "volume_id", input.VolumeID, "activity", workflow.CommitVolumeSizeResultActivityName)
	}
	// 確定を、保存先に任せる(確定済みなら何もしない)。
	_, commitError := volumeActivities.volumeStore.CommitSizeApply(requestContext, input.VolumeID, input.Succeeded)
	return commitError
}

// DeleteVolumeClaim は、volume に対応する PVC を削除する(無くてもエラーにしない)。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除する volume の ID。
//
// 戻り値:
//   - volume を読めなかったとき、k8s への依頼に失敗したときのエラー。
func (volumeActivities *VolumeActivities) DeleteVolumeClaim(requestContext context.Context, input workflow.DeleteVolumeClaimInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	volumeActivities.appLogger.Info("PVC を削除する", "volume_id", input.VolumeID, "activity", workflow.DeleteVolumeClaimActivityName)
	// DB から volume を読む(namespace の名前を知るため)。
	volume, findError := volumeActivities.volumeStore.FindForWorker(requestContext, input.VolumeID)
	if findError != nil {
		return fmt.Errorf("volume を読めませんでした: %w", findError)
	}
	// k8s を操作するものに任せる。
	return markNonRetryable(volumeActivities.volumeOperator.DeleteVolumeClaim(requestContext, volume.ProjectID, volume.ID))
}

// DeleteVolumeRecord は、volume のレコードを物理削除する。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除する volume の ID。
//
// 戻り値:
//   - データベースのエラー。
func (volumeActivities *VolumeActivities) DeleteVolumeRecord(requestContext context.Context, input workflow.DeleteVolumeRecordInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	volumeActivities.appLogger.Info("volume のレコードを削除する", "volume_id", input.VolumeID, "activity", workflow.DeleteVolumeRecordActivityName)
	// 保存先に任せる(削除中のものだけが消える)。
	return volumeActivities.volumeStore.DeleteRecord(requestContext, input.VolumeID)
}

// MarkVolumeDeleteFailed は、削除に失敗した volume を failed にする。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 対象の volume の ID。
//
// 戻り値:
//   - データベースのエラー。
func (volumeActivities *VolumeActivities) MarkVolumeDeleteFailed(requestContext context.Context, input workflow.MarkVolumeDeleteFailedInput) error {
	// 運用で気づけるよう、警告で残す。
	volumeActivities.appLogger.Warn("volume の削除に失敗した", "volume_id", input.VolumeID, "activity", workflow.MarkVolumeDeleteFailedActivityName)
	// 保存先に任せる(削除中のものだけが failed になる)。
	return volumeActivities.volumeStore.MarkDeleteFailed(requestContext, input.VolumeID)
}
