package model

import "launchs/shared/apperror"

// このファイルは、deploy の「状態を変える操作」を、モデルのメソッドとして持つ。
// メソッドは、メモリ上の deploy の値だけを変える(データベースへの保存は、repository が行う)。
// 行をロックして読んだ deploy に対して呼び、成功したら、repository が保存する。

// SettledStatus は、反映が終わったあとの、落ち着いた状態を返す。
//
// 戻り値:
//   - replicas が 0 なら scaled_zero、そうでなければ active。
func (deploy Deploy) SettledStatus() string {
	// レプリカが 0 のときは、scaled_zero。
	if deploy.Replicas == 0 {
		return DeployStatusScaledZero
	}
	// それ以外は稼働中。
	return DeployStatusActive
}

// HasPendingConfig は、まだ適用していない起動設定(環境変数、コマンド、引数)があるかを返す。
//
// 戻り値:
//   - どれか1つでもあれば true。
func (deploy Deploy) HasPendingConfig() bool {
	// 3つのどれかに、pending の値があるか。
	return len(deploy.PendingEnv) > 0 || deploy.PendingCommand != nil || len(deploy.PendingArgs) > 0
}

// BeginConfigApply は、起動設定の反映を始める(状態を applying にする)。
//
// 戻り値:
//   - 設定を操作できない状態(反映中、削除の途中)、起動中、または適用する値が無いときは conflict。
func (deploy *Deploy) BeginConfigApply() error {
	// 反映中や削除の途中は、操作できない。
	if !deploy.CanEditSettings() {
		return apperror.New(apperror.KindConflict, "deploy は "+deploy.Status+" のため、起動設定を操作できません")
	}
	// 作成の起動中は、反映を始められない。
	if deploy.IsStarting() {
		return apperror.New(apperror.KindConflict, "deploy は起動中のため、起動設定を適用できません")
	}
	// 適用する値が無いときは、反映する意味が無い。
	if !deploy.HasPendingConfig() {
		return apperror.New(apperror.KindConflict, "適用する起動設定の変更がありません")
	}
	// 反映中にする。
	deploy.Status = DeployStatusApplying
	return nil
}

// CommitConfig は、起動設定の反映の結果を確定する。
//
//   - 成功: 正式な値(env、command、args)を pending の内容にし、pending を空に戻し、状態を落ち着いた状態にする。
//     pending の環境変数が無いとき(通常は起きない)は、正式な値を壊さないよう、状態だけを変える。
//   - 失敗: 正式な値と pending は、そのまま。状態を failed にする。
//
// 引数:
//   - succeeded: 反映に成功したか。
//
// 戻り値:
//   - 確定したら true。applying でないとき(別の処理が先に確定した、など)は、何も変えず false。
func (deploy *Deploy) CommitConfig(succeeded bool) bool {
	// 反映中でなければ、確定するものが無い。
	if !deploy.IsApplying() {
		return false
	}
	// 失敗のときは、状態だけを failed にする。
	if !succeeded {
		deploy.Status = DeployStatusFailed
		return true
	}
	// 適用する値があるときだけ、正式な値を pending の内容にして、pending を空に戻す。
	if len(deploy.PendingEnv) > 0 {
		deploy.Env = deploy.PendingEnv
		deploy.Command = deploy.PendingCommand
		deploy.Args = deploy.PendingArgs
		deploy.PendingEnv = nil
		deploy.PendingCommand = nil
		deploy.PendingArgs = nil
	}
	// 落ち着いた状態にする。
	deploy.Status = deploy.SettledStatus()
	return true
}

// BeginScale は、スケーリングを始める(replicas を変え、状態を applying にする)。
//
// 同じ数で、失敗の状態でもないときは、何も変えない。
//
// 引数:
//   - replicas: 新しいレプリカ数。
//
// 戻り値:
//   - 変えたら true。何も変えなかったら false。
//   - 設定を操作できない状態、または起動中なら conflict。
func (deploy *Deploy) BeginScale(replicas int) (bool, error) {
	// 反映中や削除の途中は、操作できない。
	if !deploy.CanEditSettings() {
		return false, apperror.New(apperror.KindConflict, "deploy は "+deploy.Status+" のため、起動設定を操作できません")
	}
	// 作成の起動中は、スケーリングを始められない。
	if deploy.IsStarting() {
		return false, apperror.New(apperror.KindConflict, "deploy は起動中のため、スケーリングできません")
	}
	// 同じ数で、失敗してもいないなら、何も変えない。
	if deploy.Replicas == replicas && !deploy.IsFailed() {
		return false, nil
	}
	// replicas を変えて、反映中にする。
	deploy.Replicas = replicas
	deploy.Status = DeployStatusApplying
	return true, nil
}

// BeginDelete は、削除を受け付ける(状態を deleting にする)。
//
// 戻り値:
//   - 受け付ける前の状態(削除を始められなかったとき、元に戻すために使う)。
//   - 反映中、起動中、削除中なら conflict。
func (deploy *Deploy) BeginDelete() (string, error) {
	// 反映中、起動中、削除中は、受け付けられない。
	if !deploy.CanDelete() {
		return "", apperror.New(apperror.KindConflict, "deploy は "+deploy.Status+" のため、削除できません")
	}
	// 前の状態を覚えて、削除中にする。
	previousStatus := deploy.Status
	deploy.Status = DeployStatusDeleting
	return previousStatus, nil
}

// BeginServicePortsApply は、service のポートの反映を始める(状態を applying にする)。
//
// 戻り値:
//   - 受け付ける前の状態。
//   - 設定を操作できない状態、または起動中なら conflict。
func (deploy *Deploy) BeginServicePortsApply() (string, error) {
	// 反映中や削除の途中は、操作できない。
	if !deploy.CanEditSettings() {
		return "", apperror.New(apperror.KindConflict, "deploy は "+deploy.Status+" のため、service のポートを操作できません")
	}
	// 作成の起動中は、反映を始められない。
	if deploy.IsStarting() {
		return "", apperror.New(apperror.KindConflict, "deploy は起動中のため、ポートを適用できません")
	}
	// 前の状態を覚えて、反映中にする。
	previousStatus := deploy.Status
	deploy.Status = DeployStatusApplying
	return previousStatus, nil
}

// CommitServicePorts は、service のポートの反映の結果を、deploy の状態に確定する。
//
// 引数:
//   - succeeded: 反映に成功したか。成功なら落ち着いた状態、失敗なら failed にする。
//
// 戻り値:
//   - 確定したら true。applying でないときは、何も変えず false。
func (deploy *Deploy) CommitServicePorts(succeeded bool) bool {
	// 反映中でなければ、確定するものが無い。
	if !deploy.IsApplying() {
		return false
	}
	// 失敗のときは failed、成功のときは落ち着いた状態にする。
	if succeeded {
		deploy.Status = deploy.SettledStatus()
	} else {
		deploy.Status = DeployStatusFailed
	}
	return true
}
