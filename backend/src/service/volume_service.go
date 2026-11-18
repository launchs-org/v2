package service

import (
	"context"

	"github.com/google/uuid"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// VolumeStore は、volume のユースケースが使う、データベースの操作。
type VolumeStore interface {
	// Create は、volume を保存する。
	Create(requestContext context.Context, volume *model.Volume) error

	// ListByProject は、プロジェクトの volume を、新しい順に取得する。
	ListByProject(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.Volume, *pagination.Position, error)

	// FindByID は、volume を1件と、マウントしている deploy の ID を取得する。
	FindByID(requestContext context.Context, userID string, projectID string, volumeID string) (model.Volume, []string, error)

	// Rename は、名前を変える。
	Rename(requestContext context.Context, userID string, projectID string, volumeID string, name string) error

	// SavePendingSize は、拡張する大きさを、まだ適用しない値として保存する。
	SavePendingSize(requestContext context.Context, userID string, projectID string, volumeID string, sizeMB int, maxSizeMB int) error

	// DiscardPendingSize は、pending の大きさを破棄する。
	DiscardPendingSize(requestContext context.Context, userID string, projectID string, volumeID string) error

	// BeginSizeApply は、拡張の反映を始める(状態を applying にする)。変える前の状態を返す。
	BeginSizeApply(requestContext context.Context, userID string, projectID string, volumeID string) (string, error)

	// RevertApply は、反映の開始に失敗したとき、状態を元に戻す。
	RevertApply(requestContext context.Context, volumeID string, previousStatus string) error

	// MarkDeleting は、削除を受け付ける(状態を deleting にする)。変える前の状態を返す。
	MarkDeleting(requestContext context.Context, userID string, projectID string, volumeID string) (string, error)

	// RevertDeleting は、削除中にした volume を、元の状態に戻す。
	RevertDeleting(requestContext context.Context, volumeID string, previousStatus string) error
}

// VolumeWorkflowStarter は、volume の拡張の反映と削除の Workflow を開始するもの。
type VolumeWorkflowStarter interface {
	// StartSizeApply は、拡張の反映の Workflow を開始する。
	StartSizeApply(requestContext context.Context, volumeID string) error

	// StartVolumeDeletion は、volume の削除の Workflow を開始する。
	StartVolumeDeletion(requestContext context.Context, volumeID string) error
}

// VolumeService は、volume に関するユースケース。
type VolumeService struct {
	volumeStore           VolumeStore
	volumeWorkflowStarter VolumeWorkflowStarter
	maxSizeMB             int
}

// NewVolumeService は、VolumeService を作る。
//
// 引数:
//   - volumeStore: データベースの操作。
//   - volumeWorkflowStarter: Workflow を開始するもの。
//   - maxSizeMB: volume の大きさの上限(MB。環境変数 VOLUME_MAX_SIZE_MB)。
//
// 戻り値:
//   - 作成した VolumeService。
func NewVolumeService(volumeStore VolumeStore, volumeWorkflowStarter VolumeWorkflowStarter, maxSizeMB int) *VolumeService {
	// 依存するものを持たせて返す。
	return &VolumeService{volumeStore: volumeStore, volumeWorkflowStarter: volumeWorkflowStarter, maxSizeMB: maxSizeMB}
}

// Create は、プロジェクトに volume を作る(PVC は、最初にマウントを適用するときに作られる)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 属するプロジェクトの ID。
//   - name: volume の名前(英数字とハイフンだけの1〜64文字)。
//   - sizeMB: 大きさ(MB。1以上、上限以下)。
//
// 戻り値:
//   - 作成した volume。
//   - 名前が不正、または大きさが上限を超えるなら invalid_request。プロジェクトが無ければ not_found。
//     active でない、または同じ名前があれば conflict。
func (volumeService *VolumeService) Create(requestContext context.Context, userID string, projectID string, name string, sizeMB int) (model.Volume, error) {
	// 名前の形と、大きさの範囲を確かめる。
	if nameError := model.ValidateVolumeName(name); nameError != nil {
		return model.Volume{}, nameError
	}
	if sizeMB < 1 || sizeMB > volumeService.maxSizeMB {
		return model.Volume{}, apperror.New(apperror.KindInvalidRequest, "sizeMB は、1以上、上限以下でなければなりません")
	}
	// ID を作って、保存する。
	volume := model.Volume{ID: model.VolumeIDPrefix + uuid.NewString(), ProjectID: projectID, UserID: userID, Name: name, SizeMB: sizeMB}
	if createError := volumeService.volumeStore.Create(requestContext, &volume); createError != nil {
		return model.Volume{}, createError
	}
	return volume, nil
}

// List は、プロジェクトの volume を、新しい順に返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - limit: 1ページの件数。
//   - after: 前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - volume の一覧と、次のページの位置。
//   - プロジェクトが無ければ not_found。
func (volumeService *VolumeService) List(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.Volume, *pagination.Position, error) {
	// 保存先に任せる。
	return volumeService.volumeStore.ListByProject(requestContext, userID, projectID, limit, after)
}

// Get は、volume を1件と、マウントしている deploy の ID を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: volume の ID。
//
// 戻り値:
//   - volume と、マウントしている deploy の ID。無ければ not_found。
func (volumeService *VolumeService) Get(requestContext context.Context, userID string, projectID string, volumeID string) (model.Volume, []string, error) {
	// 保存先に任せる(他のユーザーのものは、見つからない扱いになる)。
	return volumeService.volumeStore.FindByID(requestContext, userID, projectID, volumeID)
}

// Rename は、volume の名前を変える(即時反映)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: volume の ID。
//   - name: 新しい名前(形は、確かめてから変える)。
//
// 戻り値:
//   - 名前が不正なら invalid_request。無ければ not_found。反映中・削除中、または同じ名前があれば conflict。
func (volumeService *VolumeService) Rename(requestContext context.Context, userID string, projectID string, volumeID string, name string) error {
	// 名前の形を確かめる。
	if nameError := model.ValidateVolumeName(name); nameError != nil {
		return nameError
	}
	// 保存先に任せる。
	return volumeService.volumeStore.Rename(requestContext, userID, projectID, volumeID, name)
}

// UpdateSize は、拡張する大きさを、まだ適用しない値として保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: volume の ID。
//   - sizeMB: 新しい大きさ(MB。現在以上、上限以下)。
//
// 戻り値:
//   - 縮小、または上限を超えるなら invalid_request。無ければ not_found。反映中・削除中なら conflict。
func (volumeService *VolumeService) UpdateSize(requestContext context.Context, userID string, projectID string, volumeID string, sizeMB int) error {
	// 上限を渡して、保存先(モデルの規則)に任せる。
	return volumeService.volumeStore.SavePendingSize(requestContext, userID, projectID, volumeID, sizeMB, volumeService.maxSizeMB)
}

// DiscardSize は、pending の大きさを破棄する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: volume の ID。
//
// 戻り値:
//   - 無ければ not_found。反映中・削除中、または pending が無ければ conflict。
func (volumeService *VolumeService) DiscardSize(requestContext context.Context, userID string, projectID string, volumeID string) error {
	// 保存先に任せる。
	return volumeService.volumeStore.DiscardPendingSize(requestContext, userID, projectID, volumeID)
}

// ApplySize は、pending の大きさの反映を始める。
//
// 流れ: 状態を applying にする → Workflow を開始する。Workflow を開始できなかったときは、状態を元に戻す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: 反映する volume の ID。
//
// 戻り値:
//   - 無ければ not_found。反映中・削除中、または pending が無ければ conflict。
//   - Workflow を開始できなかったときは internal_error。
func (volumeService *VolumeService) ApplySize(requestContext context.Context, userID string, projectID string, volumeID string) error {
	// 1. 状態を applying にする。
	previousStatus, beginError := volumeService.volumeStore.BeginSizeApply(requestContext, userID, projectID, volumeID)
	if beginError != nil {
		return beginError
	}
	// 2. Workflow を開始する。
	startError := volumeService.volumeWorkflowStarter.StartSizeApply(requestContext, volumeID)
	if startError == nil {
		return nil
	}
	// 3. 開始できなかったので、状態を元に戻す(リクエストが中断されていても戻せるよう、context を切り離す)。
	if revertError := volumeService.volumeStore.RevertApply(context.WithoutCancel(requestContext), volumeID, previousStatus); revertError != nil {
		return apperror.Wrap(apperror.KindInternal, "拡張の反映を開始できませんでした", joinErrors(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "拡張の反映を開始できませんでした", startError)
}

// Delete は、volume の削除を受け付け、実体の削除を開始する。
//
// 流れ: 状態を deleting にする → Workflow を開始する。Workflow を開始できなかったときは、状態を元に戻す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: 削除する volume の ID。
//
// 戻り値:
//   - 無ければ not_found。反映中・削除中、またはマウント中なら conflict。
//   - Workflow を開始できなかったときは internal_error。
func (volumeService *VolumeService) Delete(requestContext context.Context, userID string, projectID string, volumeID string) error {
	// 1. 状態を deleting にする。
	previousStatus, markError := volumeService.volumeStore.MarkDeleting(requestContext, userID, projectID, volumeID)
	if markError != nil {
		return markError
	}
	// 2. Workflow を開始する。
	startError := volumeService.volumeWorkflowStarter.StartVolumeDeletion(requestContext, volumeID)
	if startError == nil {
		return nil
	}
	// 3. 開始できなかったので、状態を元に戻す。
	if revertError := volumeService.volumeStore.RevertDeleting(context.WithoutCancel(requestContext), volumeID, previousStatus); revertError != nil {
		return apperror.Wrap(apperror.KindInternal, "volume の削除を開始できませんでした", joinErrors(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "volume の削除を開始できませんでした", startError)
}
