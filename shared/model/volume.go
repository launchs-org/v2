package model

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"launchs/shared/apperror"

	"gorm.io/datatypes"
)

const (
	// VolumeIDPrefix は、volume の ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。PVC の名前にも使う。
	VolumeIDPrefix = "volume-"

	// VolumeStatusActive は、使える volume。
	VolumeStatusActive = "active"

	// VolumeStatusApplying は、拡張を反映している最中の volume。
	VolumeStatusApplying = "applying"

	// VolumeStatusFailed は、拡張または削除に失敗した volume。
	VolumeStatusFailed = "failed"

	// VolumeStatusDeleting は、削除を受け付けて、実体を削除している最中の volume。
	VolumeStatusDeleting = "deleting"

	// MaxVolumeNameLength は、volume の名前の長さの上限。
	MaxVolumeNameLength = 64

	// MaxMounts は、1つの deploy にマウントできるボリュームの数の上限。
	MaxMounts = 10

	// MaxMountPathLength は、マウント先の path の長さの上限。
	MaxMountPathLength = 200
)

// volumeNamePattern は、volume の名前として受け付ける形(英数字とハイフンだけ)。
var volumeNamePattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// mountPathPattern は、マウント先として受け付ける形(/ で始まり、英数字と . _ - / だけ)。
var mountPathPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

// Volume は、volumes テーブルの1行。k8s の PVC 1つに対応する。
type Volume struct {
	// ID は、volume の ID("volume-" + UUID v4)。
	ID string `gorm:"column:id;primaryKey"`

	// ProjectID は、属するプロジェクトの ID(k8s の namespace の名前でもある)。
	ProjectID string `gorm:"column:project_id"`

	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id"`

	// Name は、プロジェクトの中で一意の名前。
	Name string `gorm:"column:name"`

	// SizeMB は、現在有効な大きさ(MB)。
	SizeMB int `gorm:"column:size_mb"`

	// PendingSizeMB は、まだ適用していない大きさ(MB)。変更が無ければ nil。
	PendingSizeMB *int `gorm:"column:pending_size_mb"`

	// Status は、volume の状態(VolumeStatus... のどれか)。
	Status string `gorm:"column:status"`

	// CreatedAt は、作成日時。
	CreatedAt time.Time `gorm:"column:created_at"`

	// UpdatedAt は、更新日時。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("volumes")。
func (Volume) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "volumes"
}

// MountSpec は、deploy がマウントするボリューム1つ。deploys.mounts と deploys.pending_mounts の JSON の配列の要素。
type MountSpec struct {
	// VolumeID は、マウントする volume の ID(同じプロジェクトのもの)。
	VolumeID string `json:"volumeId"`

	// MountPath は、コンテナの中のマウント先の path。
	MountPath string `json:"mountPath"`
}

// ValidateVolumeName は、volume の名前の形(英数字とハイフンだけ、1〜64文字)を確かめる。
//
// 引数:
//   - name: 確かめる名前。
//
// 戻り値:
//   - 不正なら invalid_request。
func ValidateVolumeName(name string) error {
	// 長さと文字の種類を確かめる。
	if len(name) < 1 || len(name) > MaxVolumeNameLength || !volumeNamePattern.MatchString(name) {
		return apperror.New(apperror.KindInvalidRequest, "name は、英数字とハイフンだけの1〜64文字でなければなりません")
	}
	return nil
}

// ValidateMounts は、マウント構成の形(個数、volumeId の形、path の書式、重複)を確かめる。
// volume が実在するか、などは、データベースを見る必要があるので、ここでは確かめない。
//
// 引数:
//   - mounts: 確かめる構成(空でもよい。空は、全て外す)。
//
// 戻り値:
//   - 不正なら invalid_request。
func ValidateMounts(mounts []MountSpec) error {
	// 個数の上限を確かめる。
	if len(mounts) > MaxMounts {
		return apperror.New(apperror.KindInvalidRequest, "mounts は10件までです")
	}
	// 重複を調べるための入れ物。
	usedVolumes := map[string]bool{}
	usedPaths := map[string]bool{}
	for _, mount := range mounts {
		// volume の ID の形を確かめる。
		if len(mount.VolumeID) <= len(VolumeIDPrefix) || !strings.HasPrefix(mount.VolumeID, VolumeIDPrefix) {
			return apperror.New(apperror.KindInvalidRequest, "volumeId の形が正しくありません: "+mount.VolumeID)
		}
		// path の書式を確かめる(".." を含むものは、拒否する)。
		if len(mount.MountPath) > MaxMountPathLength || !mountPathPattern.MatchString(mount.MountPath) || strings.Contains(mount.MountPath, "..") {
			return apperror.New(apperror.KindInvalidRequest, "mountPath は、/ で始まり、英数字と . _ - / だけの200文字以内で、/ だけや .. を含まないものでなければなりません: "+mount.MountPath)
		}
		// 同じ volume を2回、同じ path に2つ、は置けない。
		if usedVolumes[mount.VolumeID] {
			return apperror.New(apperror.KindInvalidRequest, "mounts の volumeId が重複しています: "+mount.VolumeID)
		}
		if usedPaths[mount.MountPath] {
			return apperror.New(apperror.KindInvalidRequest, "mounts の mountPath が重複しています: "+mount.MountPath)
		}
		usedVolumes[mount.VolumeID], usedPaths[mount.MountPath] = true, true
	}
	return nil
}

// ParseMounts は、JSON のマウント構成を読み込む。
//
// 引数:
//   - raw: JSON の配列(空なら、マウント無し)。
//
// 戻り値:
//   - マウント構成。読めなければエラー。
func ParseMounts(raw datatypes.JSON) ([]MountSpec, error) {
	// 空(NULL)は、マウント無し。
	if len(raw) == 0 {
		return []MountSpec{}, nil
	}
	// JSON を読み込む。
	mounts := []MountSpec{}
	if parseError := json.Unmarshal(raw, &mounts); parseError != nil {
		return nil, parseError
	}
	return mounts, nil
}

// --- 状態による操作の可否 ---

// IsApplying は、volume が拡張の反映中かを返す。
//
// 戻り値:
//   - 反映中なら true。
func (volume Volume) IsApplying() bool {
	// applying が、反映中。
	return volume.Status == VolumeStatusApplying
}

// CanChange は、名前の変更・拡張の保存・適用・破棄ができるかを返す。
//
// 戻り値:
//   - active、または failed(直して再度適用できる)なら true。反映中と削除中は false。
func (volume Volume) CanChange() bool {
	// 反映中と削除中以外なら、操作できる。
	return volume.Status == VolumeStatusActive || volume.Status == VolumeStatusFailed
}

// CanDelete は、削除を受け付けられるかを返す(マウント中でないことは、別に確かめる)。
//
// 戻り値:
//   - 反映中と削除中以外なら true(failed は、もう一度削除できる)。
func (volume Volume) CanDelete() bool {
	// 反映中と削除中は、受け付けない。
	return !volume.IsApplying() && volume.Status != VolumeStatusDeleting
}

// CanBeMounted は、deploy にマウントできるかを返す。
//
// 戻り値:
//   - 削除中でなければ true。
func (volume Volume) CanBeMounted() bool {
	// 削除中の volume には、新たにマウントできない。
	return volume.Status != VolumeStatusDeleting
}

// --- 状態を変える操作 ---

// Rename は、名前を変える(メタデータだけの変更で、pending を使わない)。
//
// 引数:
//   - name: 新しい名前(形は、呼び出し側で確かめてある)。
//
// 戻り値:
//   - 操作できない状態(反映中、削除中)なら conflict。
func (volume *Volume) Rename(name string) error {
	// 反映中と削除中は、操作できない。
	if !volume.CanChange() {
		return apperror.New(apperror.KindConflict, "volume は "+volume.Status+" のため、操作できません")
	}
	// 名前を変える。
	volume.Name = name
	return nil
}

// ReservedMB は、クォータで、この volume が使っているとして数える大きさ(MB)。
//
// 拡張の pending も、適用の前から予約として数えるので、現在の大きさと pending の大きい方になる。
//
// 戻り値:
//   - 数える大きさ(MB)。
func (volume *Volume) ReservedMB() int {
	// pending があり、現在より大きければ、pending を数える。
	if volume.PendingSizeMB != nil && *volume.PendingSizeMB > volume.SizeMB {
		return *volume.PendingSizeMB
	}
	return volume.SizeMB
}

// SetPendingSize は、拡張する大きさを、まだ適用しない値として保存する。
//
// 現在と同じ大きさなら、変更が無いものとして、pending を空にする(以前の pending も消える)。
//
// 引数:
//   - sizeMB: 新しい大きさ(MB)。
//   - maxSizeMB: 大きさの上限(MB)。
//
// 戻り値:
//   - 操作できない状態なら conflict。縮小、または上限を超えるなら invalid_request。
func (volume *Volume) SetPendingSize(sizeMB int, maxSizeMB int) error {
	// 反映中と削除中は、操作できない。
	if !volume.CanChange() {
		return apperror.New(apperror.KindConflict, "volume は "+volume.Status+" のため、操作できません")
	}
	// 縮小は、できない。
	if sizeMB < volume.SizeMB {
		return apperror.New(apperror.KindInvalidRequest, "sizeMB は、現在の大きさ以上でなければなりません(縮小はできません)")
	}
	// 上限を超えてはいけない。
	if sizeMB > maxSizeMB {
		return apperror.New(apperror.KindInvalidRequest, "sizeMB は、上限を超えられません")
	}
	// 現在と同じなら、pending を空にする。そうでなければ保存する。
	if sizeMB == volume.SizeMB {
		volume.PendingSizeMB = nil
		return nil
	}
	volume.PendingSizeMB = &sizeMB
	return nil
}

// DiscardPendingSize は、pending の大きさを破棄する。
//
// 戻り値:
//   - 操作できない状態、または pending が無いなら conflict。
func (volume *Volume) DiscardPendingSize() error {
	// 反映中と削除中は、操作できない。
	if !volume.CanChange() {
		return apperror.New(apperror.KindConflict, "volume は "+volume.Status+" のため、操作できません")
	}
	// 破棄する値が無いときは、conflict。
	if volume.PendingSizeMB == nil {
		return apperror.New(apperror.KindConflict, "破棄する大きさの変更がありません")
	}
	// pending を空にする。
	volume.PendingSizeMB = nil
	return nil
}

// BeginSizeApply は、拡張の反映を始める(状態を applying にする)。
//
// 戻り値:
//   - 受け付ける前の状態(開始に失敗したとき、元に戻すために使う)。
//   - 操作できない状態、または適用する値が無いときは conflict。
func (volume *Volume) BeginSizeApply() (string, error) {
	// 反映中や削除中は、操作できない。
	if !volume.CanChange() {
		return "", apperror.New(apperror.KindConflict, "volume は "+volume.Status+" のため、操作できません")
	}
	// 適用する値が無いときは、反映する意味が無い。
	if volume.PendingSizeMB == nil {
		return "", apperror.New(apperror.KindConflict, "適用する大きさの変更がありません")
	}
	// 前の状態を覚えて、反映中にする。
	previousStatus := volume.Status
	volume.Status = VolumeStatusApplying
	return previousStatus, nil
}

// CommitSize は、拡張の反映の結果を確定する。
//
//   - 成功: 大きさを pending の値にし、pending を空に戻し、active にする。
//   - 失敗: 大きさと pending は、そのまま。failed にする(PVC は縮められないが、同じ値での再適用は問題ない)。
//
// 引数:
//   - succeeded: 反映に成功したか。
//
// 戻り値:
//   - 確定したら true。applying でないときは、何も変えず false。
func (volume *Volume) CommitSize(succeeded bool) bool {
	// 反映中でなければ、確定するものが無い。
	if !volume.IsApplying() {
		return false
	}
	// 失敗のときは、failed にするだけ。
	if !succeeded {
		volume.Status = VolumeStatusFailed
		return true
	}
	// 成功のときは、pending があるときだけ、大きさにして pending を空に戻す。
	if volume.PendingSizeMB != nil {
		volume.SizeMB = *volume.PendingSizeMB
		volume.PendingSizeMB = nil
	}
	volume.Status = VolumeStatusActive
	return true
}

// RevertApply は、反映の開始に失敗したとき、状態を元に戻す。
//
// 引数:
//   - status: 戻す先の状態。
//
// 戻り値:
//   - 戻したら true。applying でないときは、何も変えず false。
func (volume *Volume) RevertApply(status string) bool {
	// 反映中でなければ、戻すものが無い。
	if !volume.IsApplying() {
		return false
	}
	// 前の状態に戻す。
	volume.Status = status
	return true
}

// BeginDelete は、削除を受け付ける(状態を deleting にする)。マウント中でないことは、呼び出し側が確かめる。
//
// 戻り値:
//   - 受け付ける前の状態。
//   - 反映中、または削除中なら conflict。
func (volume *Volume) BeginDelete() (string, error) {
	// 反映中と削除中は、受け付けられない。
	if !volume.CanDelete() {
		return "", apperror.New(apperror.KindConflict, "volume は "+volume.Status+" のため、削除できません")
	}
	// 前の状態を覚えて、削除中にする。
	previousStatus := volume.Status
	volume.Status = VolumeStatusDeleting
	return previousStatus, nil
}

// RevertDeleting は、削除中の volume を、削除を受け付ける前の状態に戻す。
//
// 引数:
//   - previousStatus: 戻す先の状態。
//
// 戻り値:
//   - 戻したら true。削除中でないときは、何も変えず false。
func (volume *Volume) RevertDeleting(previousStatus string) bool {
	// 削除中でなければ、戻すものが無い。
	if volume.Status != VolumeStatusDeleting {
		return false
	}
	// 前の状態に戻す。
	volume.Status = previousStatus
	return true
}

// MarkDeleteFailed は、削除中の volume を、failed にする(削除は、もう一度受け付ける)。
//
// 戻り値:
//   - 変えたら true。削除中でないときは、何も変えず false。
func (volume *Volume) MarkDeleteFailed() bool {
	// 削除中でなければ、変えるものが無い。
	if volume.Status != VolumeStatusDeleting {
		return false
	}
	// 失敗の状態にする。
	volume.Status = VolumeStatusFailed
	return true
}
