package model

import (
	"strings"
	"testing"

	"gorm.io/datatypes"
)

// TestValidateVolumeNameAndMounts は、volume の名前と、マウント構成の形の検査を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestValidateVolumeNameAndMounts(testing *testing.T) {
	// 名前: 英数字とハイフンの1〜64文字だけ。
	for _, valid := range []string{"a", "db-data", "A1-b2", strings.Repeat("a", 64)} {
		if ValidateVolumeName(valid) != nil {
			testing.Errorf("%q は通るはず", valid)
		}
	}
	for _, invalid := range []string{"", "a_b", "a b", "あ", "a/b", strings.Repeat("a", 65)} {
		if ValidateVolumeName(invalid) == nil {
			testing.Errorf("%q はエラーのはず", invalid)
		}
	}
	// マウント: 空と、正しい構成は通る。
	volumeID := VolumeIDPrefix + "00000000-0000-4000-8000-000000000000"
	otherID := VolumeIDPrefix + "00000000-0000-4000-8000-000000000001"
	if ValidateMounts(nil) != nil || ValidateMounts([]MountSpec{{VolumeID: volumeID, MountPath: "/data"}, {VolumeID: otherID, MountPath: "/var/lib/db-1.x_y"}}) != nil {
		testing.Error("正しい構成は通るはず")
	}
	invalid := map[string][]MountSpec{
		"volumeId の形":     {{VolumeID: "nope", MountPath: "/data"}},
		"volumeId が接頭辞だけ": {{VolumeID: VolumeIDPrefix, MountPath: "/data"}},
		"相対 path":         {{VolumeID: volumeID, MountPath: "data"}},
		"ルート":             {{VolumeID: volumeID, MountPath: "/"}},
		"..":              {{VolumeID: volumeID, MountPath: "/a/../b"}},
		"空白":              {{VolumeID: volumeID, MountPath: "/a b"}},
		"長すぎる":            {{VolumeID: volumeID, MountPath: "/" + strings.Repeat("a", MaxMountPathLength)}},
		"volume の重複":      {{VolumeID: volumeID, MountPath: "/a"}, {VolumeID: volumeID, MountPath: "/b"}},
		"path の重複":        {{VolumeID: volumeID, MountPath: "/a"}, {VolumeID: otherID, MountPath: "/a"}},
	}
	for name, mounts := range invalid {
		if ValidateMounts(mounts) == nil {
			testing.Errorf("%s: エラーになるはず", name)
		}
	}
	// 個数は、10件まで。
	many := []MountSpec{}
	for index := 0; index <= MaxMounts; index++ {
		many = append(many, MountSpec{VolumeID: VolumeIDPrefix + strings.Repeat("a", index+1), MountPath: "/p" + strings.Repeat("a", index+1)})
	}
	if ValidateMounts(many[:MaxMounts]) != nil || ValidateMounts(many) == nil {
		testing.Error("10件までのはず")
	}
	// 読み込み: 空と NULL は、マウント無し。壊れた JSON は、エラー。
	if parsed, parseError := ParseMounts(nil); parseError != nil || len(parsed) != 0 {
		testing.Error("NULL は、マウント無しのはず")
	}
	if _, parseError := ParseMounts(datatypes.JSON(`{`)); parseError == nil {
		testing.Error("壊れた JSON はエラーのはず")
	}
}

// TestVolumeTransitions は、volume の状態の規則と遷移(名前、拡張の保存・反映・確定、削除)を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestVolumeTransitions(testing *testing.T) {
	// 状態ごとの操作の可否。
	for _, testCase := range []struct {
		status    string
		canChange bool
		canDelete bool
		canMount  bool
	}{
		{VolumeStatusActive, true, true, true},
		{VolumeStatusFailed, true, true, true},
		{VolumeStatusApplying, false, false, true},
		{VolumeStatusDeleting, false, false, false},
	} {
		volume := Volume{Status: testCase.status}
		if volume.CanChange() != testCase.canChange || volume.CanDelete() != testCase.canDelete || volume.CanBeMounted() != testCase.canMount {
			testing.Errorf("%s: 可否が違う", testCase.status)
		}
	}
	// 名前の変更は、反映中・削除中は conflict。
	renaming := Volume{Status: VolumeStatusActive, Name: "a"}
	if renaming.Rename("b") != nil || renaming.Name != "b" || (&Volume{Status: VolumeStatusApplying}).Rename("x") == nil {
		testing.Error("名前の変更が違う")
	}
	// 拡張の保存: 拡大は pending に入り、同じ大きさは pending を空にし、縮小と上限超過はエラー。
	resizing := Volume{Status: VolumeStatusActive, SizeMB: 1000}
	if resizing.SetPendingSize(2000, 5120) != nil || resizing.PendingSizeMB == nil || *resizing.PendingSizeMB != 2000 {
		testing.Fatal("拡大は pending に入るはず")
	}
	if resizing.SetPendingSize(1000, 5120) != nil || resizing.PendingSizeMB != nil {
		testing.Error("同じ大きさは pending を空にするはず")
	}
	if resizing.SetPendingSize(999, 5120) == nil || resizing.SetPendingSize(5121, 5120) == nil || resizing.SetPendingSize(5120, 5120) != nil {
		testing.Error("縮小と上限超過はエラー、上限ちょうどは通るはず")
	}
	// 破棄: pending が無いと conflict。
	discarding := Volume{Status: VolumeStatusActive, SizeMB: 10}
	if discarding.DiscardPendingSize() == nil {
		testing.Error("pending が無ければ conflict のはず")
	}
	// 反映の開始と確定。
	applying := Volume{Status: VolumeStatusFailed, SizeMB: 1000}
	if _, beginError := applying.BeginSizeApply(); beginError == nil {
		testing.Error("pending が無ければ conflict のはず")
	}
	pending := 2000
	applying.PendingSizeMB = &pending
	previous, beginError := applying.BeginSizeApply()
	if beginError != nil || previous != VolumeStatusFailed || applying.Status != VolumeStatusApplying {
		testing.Fatalf("反映の開始が違う: %v %+v", beginError, applying)
	}
	if !applying.CommitSize(true) || applying.SizeMB != 2000 || applying.PendingSizeMB != nil || applying.Status != VolumeStatusActive || applying.CommitSize(true) {
		testing.Errorf("成功の確定が違う: %+v", applying)
	}
	// 失敗の確定: 大きさと pending は、そのまま。
	failing := Volume{Status: VolumeStatusApplying, SizeMB: 1000, PendingSizeMB: &pending}
	if !failing.CommitSize(false) || failing.Status != VolumeStatusFailed || failing.SizeMB != 1000 || failing.PendingSizeMB == nil {
		testing.Errorf("失敗の確定が違う: %+v", failing)
	}
	// 開始に失敗したときの戻し。
	reverting := Volume{Status: VolumeStatusApplying}
	if !reverting.RevertApply(VolumeStatusActive) || reverting.Status != VolumeStatusActive || reverting.RevertApply(VolumeStatusFailed) {
		testing.Error("戻しが違う")
	}
	// 削除: 受け付け → 戻す / failed にする。
	deleting := Volume{Status: VolumeStatusFailed}
	previous, deleteError := deleting.BeginDelete()
	if deleteError != nil || previous != VolumeStatusFailed || deleting.Status != VolumeStatusDeleting {
		testing.Error("削除の受け付けが違う")
	}
	if _, secondError := deleting.BeginDelete(); secondError == nil {
		testing.Error("削除中は conflict のはず")
	}
	if !deleting.MarkDeleteFailed() || deleting.Status != VolumeStatusFailed || deleting.MarkDeleteFailed() || deleting.RevertDeleting(VolumeStatusActive) {
		testing.Error("削除失敗の遷移が違う")
	}
}

// TestDeployMountsTransitions は、deploy のマウントの反映の開始・確定が、仕様どおりであることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestDeployMountsTransitions(testing *testing.T) {
	// pending が無いと、反映は始められない。
	idle := Deploy{Status: DeployStatusActive}
	if _, beginError := idle.BeginMountsApply(); beginError == nil {
		testing.Error("pending が無ければ conflict のはず")
	}
	// 起動中、反映中、削除の途中は、始められない。
	for _, status := range []string{DeployStatusPending, DeployStatusApplying, DeployStatusDeleting, DeployStatusDeleteFailed} {
		blocked := Deploy{Status: status, PendingMounts: datatypes.JSON(`[]`)}
		if _, beginError := blocked.BeginMountsApply(); beginError == nil {
			testing.Errorf("%s は conflict のはず", status)
		}
	}
	// 空の配列(全て外す)も、適用する値としてある扱い。
	removing := Deploy{Status: DeployStatusScaledZero, Replicas: 0, Mounts: datatypes.JSON(`[{"volumeId":"v"}]`), PendingMounts: datatypes.JSON(`[]`)}
	previous, beginError := removing.BeginMountsApply()
	if beginError != nil || previous != DeployStatusScaledZero || removing.Status != DeployStatusApplying {
		testing.Fatalf("反映を始められるはず: %v", beginError)
	}
	// 成功: マウントが pending になり、pending が空になり、replicas 0 は scaled_zero に戻る。
	if !removing.CommitMounts(true) || string(removing.Mounts) != `[]` || removing.PendingMounts != nil || removing.Status != DeployStatusScaledZero || removing.CommitMounts(true) {
		testing.Errorf("成功の確定が違う: %+v", removing)
	}
	// 失敗: マウントと pending は、そのままで、failed になる。
	failing := Deploy{Status: DeployStatusApplying, Replicas: 1, Mounts: datatypes.JSON(`[]`), PendingMounts: datatypes.JSON(`[{"volumeId":"v"}]`)}
	if !failing.CommitMounts(false) || failing.Status != DeployStatusFailed || string(failing.Mounts) != `[]` || failing.PendingMounts == nil {
		testing.Errorf("失敗の確定が違う: %+v", failing)
	}
}
