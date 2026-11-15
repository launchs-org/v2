package model

import "launchs/shared/apperror"

// このファイルは、deploy のボリュームのマウントの、状態を変える操作を持つ。
// メソッドは、メモリ上の値だけを変える(保存は repository が行う)。

// HasPendingMounts は、まだ適用していないマウントの構成があるかを返す(空の配列も、「全て外す」変更としてある扱い)。
//
// 戻り値:
//   - あれば true。
func (deploy Deploy) HasPendingMounts() bool {
	// pending の列に、値があるか。
	return len(deploy.PendingMounts) > 0
}

// BeginMountsApply は、マウントの反映を始める(状態を applying にする)。
//
// 戻り値:
//   - 受け付ける前の状態(開始に失敗したとき、元に戻すために使う)。
//   - 設定を操作できない状態(反映中、削除の途中)、起動中、または適用する値が無いときは conflict。
func (deploy *Deploy) BeginMountsApply() (string, error) {
	// 反映中や削除の途中は、操作できない。
	if !deploy.CanEditSettings() {
		return "", apperror.New(apperror.KindConflict, "deploy は "+deploy.Status+" のため、マウントを操作できません")
	}
	// 作成の起動中は、反映を始められない。
	if deploy.IsStarting() {
		return "", apperror.New(apperror.KindConflict, "deploy は起動中のため、マウントを適用できません")
	}
	// 適用する値が無いときは、反映する意味が無い。
	if !deploy.HasPendingMounts() {
		return "", apperror.New(apperror.KindConflict, "適用するマウントの変更がありません")
	}
	// 前の状態を覚えて、反映中にする。
	previousStatus := deploy.Status
	deploy.Status = DeployStatusApplying
	return previousStatus, nil
}

// CommitMounts は、マウントの反映の結果を確定する。
//
//   - 成功: マウントを pending の内容にし、pending を空に戻し、状態を落ち着いた状態にする。
//   - 失敗: マウントと pending は、そのまま。状態を failed にする。
//
// 引数:
//   - succeeded: 反映に成功したか。
//
// 戻り値:
//   - 確定したら true。applying でないときは、何も変えず false。
func (deploy *Deploy) CommitMounts(succeeded bool) bool {
	// 反映中でなければ、確定するものが無い。
	if !deploy.IsApplying() {
		return false
	}
	// 失敗のときは、状態だけを failed にする。
	if !succeeded {
		deploy.Status = DeployStatusFailed
		return true
	}
	// 成功のときは、pending があるときだけ、マウントにして pending を空に戻す。
	if deploy.HasPendingMounts() {
		deploy.Mounts = deploy.PendingMounts
		deploy.PendingMounts = nil
	}
	// 落ち着いた状態にする。
	deploy.Status = deploy.SettledStatus()
	return true
}
