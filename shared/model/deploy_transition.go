package model

import (
	"fmt"
	"time"

	"launchs/shared/apperror"
)

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
	// 適用するイメージがあるときは、正式なイメージにして、pending を空に戻す。
	if deploy.PendingImageRef != nil {
		deploy.ImageRef = *deploy.PendingImageRef
		deploy.PendingImageRef = nil
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
//   - replicas が 0〜MaxReplicas の範囲外なら invalid_request(状態の確認より先に行う)。
//   - 設定を操作できない状態、または起動中なら conflict。
func (deploy *Deploy) BeginScale(replicas int) (bool, error) {
	// 範囲外の値は、状態に関わらず、入力の不正として断る(replicas の上限は、スペックやクォータとは関係なく、全体で決まっている)。
	if replicas < 0 || replicas > MaxReplicas {
		return false, apperror.New(apperror.KindInvalidRequest, fmt.Sprintf("replicas は 0〜%d でなければなりません", MaxReplicas))
	}
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

// RevertDeleting は、削除中(deleting)の deploy を、削除を受け付ける前の状態に戻す。
//
// 削除の Workflow を開始できなかったときに使う。
//
// 引数:
//   - previousStatus: 戻す先の状態(BeginDelete が返したもの)。
//
// 戻り値:
//   - 戻したら true。削除中でないときは、何も変えず false。
func (deploy *Deploy) RevertDeleting(previousStatus string) bool {
	// 削除中でなければ、戻すものが無い。
	if deploy.Status != DeployStatusDeleting {
		return false
	}
	// 前の状態に戻す。
	deploy.Status = previousStatus
	return true
}

// MarkDeleteFailed は、削除中(deleting)の deploy を、削除に失敗した状態(delete_failed)にする。
//
// 戻り値:
//   - 変えたら true。削除中でないときは、何も変えず false。
func (deploy *Deploy) MarkDeleteFailed() bool {
	// 削除中でなければ、変えるものが無い。
	if deploy.Status != DeployStatusDeleting {
		return false
	}
	// 削除に失敗した状態にする。
	deploy.Status = DeployStatusDeleteFailed
	return true
}

// RevertScale は、スケーリングの開始に失敗したとき、replicas と状態を元に戻す。
//
// 引数:
//   - replicas: 戻す先のレプリカ数。
//   - status: 戻す先の状態。
//
// 戻り値:
//   - 戻したら true。applying でないとき(別の処理が先に変えた)は、何も変えず false。
func (deploy *Deploy) RevertScale(replicas int, status string) bool {
	// 反映中でなければ、戻すものが無い。
	if !deploy.IsApplying() {
		return false
	}
	// 前の値に戻す。
	deploy.Replicas = replicas
	deploy.Status = status
	return true
}

// RevertApply は、反映の開始に失敗したとき、状態だけを元に戻す(起動設定・ポートの反映用)。
//
// 引数:
//   - status: 戻す先の状態。
//
// 戻り値:
//   - 戻したら true。applying でないときは、何も変えず false。
func (deploy *Deploy) RevertApply(status string) bool {
	// 反映中でなければ、戻すものが無い。
	if !deploy.IsApplying() {
		return false
	}
	// 前の状態に戻す。
	deploy.Status = status
	return true
}

// BeginCrashLoopStop は、crash-loop で止める準備として、稼働中(active)の deploy を applying にする。
//
// 戻り値:
//   - 変えたら true。active でないときは、何も変えず false。
func (deploy *Deploy) BeginCrashLoopStop() bool {
	// 止められる状態でなければ、何もしない。
	if !deploy.CanStopForCrashLoop() {
		return false
	}
	// 反映中にする。
	deploy.Status = DeployStatusApplying
	return true
}

// FinishCrashLoopStop は、止める処理が終わった deploy(applying)を、failed にする。
//
// 戻り値:
//   - 変えたら true。applying でないときは、何も変えず false。
func (deploy *Deploy) FinishCrashLoopStop() bool {
	// 反映中でなければ、変えるものが無い。
	if !deploy.IsApplying() {
		return false
	}
	// 失敗の状態にする。
	deploy.Status = DeployStatusFailed
	return true
}

// CrashLoopingPod は、deploy を crash-loop として止めるべきかを決め、止める理由になる Pod を返す(Pods を読み込んであるとき)。
//
// 次の全てを満たすときだけ、止める。
//   - deploy が稼働中(active)で、replicas が 1 以上。
//   - 終了中でない Pod の中に、安定して動いている Pod が1つも無い(1つでも健全なら、サービスは続いている)。
//   - 安定していない Pod のうち、不健全になってから threshold 以上たったものがある。
//
// 引数:
//   - at: 判定する時刻(データベースの時計)。
//   - threshold: 不健全が続いてよい時間。
//
// 戻り値:
//   - 止める理由になる Pod(不健全が最も長いもの)と、止めるべきなら true。
func (deploy Deploy) CrashLoopingPod(at time.Time, threshold time.Duration) (DeployPod, bool) {
	// 稼働中で、レプリカがあるものだけが対象。
	if !deploy.CanStopForCrashLoop() || deploy.Replicas <= 0 {
		return DeployPod{}, false
	}
	// 不健全が最も長い Pod を探す。
	var longest DeployPod
	found := false
	for _, pod := range deploy.Pods {
		// 終了中の Pod は、判定に含めない。
		if pod.Status == PodStatusTerminating {
			continue
		}
		// 安定している Pod が1つでもあれば、止めない。
		if pod.IsStable(at) {
			return DeployPod{}, false
		}
		// 不健全になった時刻が無い、または、まだ猶予の時間に達していないものは、候補にしない。
		if pod.UnhealthySince == nil || pod.UnhealthySince.After(at.Add(-threshold)) {
			continue
		}
		// より長く不健全な Pod を選ぶ。
		if !found || pod.UnhealthySince.Before(*longest.UnhealthySince) {
			longest = pod
			found = true
		}
	}
	return longest, found
}

// CompleteCreation は、作成の起動の結果を確定する。pending の deploy だけが対象。
//
// 引数:
//   - succeeded: 起動に成功したか。成功なら落ち着いた状態(active か scaled_zero)、失敗なら failed。
//
// 戻り値:
//   - 確定したら true。pending でないとき(別の処理が先に決めた)は、何も変えず false。
func (deploy *Deploy) CompleteCreation(succeeded bool) bool {
	// 起動中でなければ、確定するものが無い。
	if !deploy.IsStarting() {
		return false
	}
	// 成功なら落ち着いた状態、失敗なら failed にする。
	if succeeded {
		deploy.Status = deploy.SettledStatus()
	} else {
		deploy.Status = DeployStatusFailed
	}
	return true
}

// CompleteScale は、スケーリングの結果を確定する。applying の deploy だけが対象。
//
// 引数:
//   - succeeded: 成功したか。成功なら落ち着いた状態、失敗なら failed。
//
// 戻り値:
//   - 確定したら true。applying でないときは、何も変えず false。
func (deploy *Deploy) CompleteScale(succeeded bool) bool {
	// 反映中でなければ、確定するものが無い(結果の確定は、サービスのポートの反映と同じ規則)。
	return deploy.CommitServicePorts(succeeded)
}
