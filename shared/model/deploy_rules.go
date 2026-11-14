package model

// このファイルは、deploy の「状態による操作の可否」の規則を集める。
// 状態で分岐する規則は、ここだけに置く(repository などに、状態の比較を散らさない)。

// IsBeingDeleted は、deploy が削除の途中(削除中、または削除に失敗して止まっている)かどうかを返す。
//
// 削除の途中の deploy には、service などの付属物を作ったり消したりできない(deploy の削除が片付ける)。
//
// 戻り値:
//   - 削除の途中なら true。
func (deploy Deploy) IsBeingDeleted() bool {
	// deleting と delete_failed が、削除の途中。
	return deploy.Status == DeployStatusDeleting || deploy.Status == DeployStatusDeleteFailed
}

// IsStarting は、deploy が作成の起動中(pending)かどうかを返す。
//
// 起動中は、作成の処理が状態を決めるので、反映やスケーリングなどの操作は始められない。
//
// 戻り値:
//   - 起動中なら true。
func (deploy Deploy) IsStarting() bool {
	// pending が、作成の起動中。
	return deploy.Status == DeployStatusPending
}

// IsApplying は、deploy が反映中(applying)かどうかを返す。
//
// 戻り値:
//   - 反映中なら true。
func (deploy Deploy) IsApplying() bool {
	// applying が、反映中。
	return deploy.Status == DeployStatusApplying
}

// IsActive は、deploy が稼働中(active)かどうかを返す。
//
// 戻り値:
//   - 稼働中なら true。
func (deploy Deploy) IsActive() bool {
	// active が、稼働中。
	return deploy.Status == DeployStatusActive
}

// IsFailed は、deploy が失敗(failed)の状態かどうかを返す。
//
// 戻り値:
//   - 失敗の状態なら true。
func (deploy Deploy) IsFailed() bool {
	// failed が、起動または反映に失敗した状態。
	return deploy.Status == DeployStatusFailed
}

// CanEditSettings は、起動設定・スケール・service のポートなど、deploy の設定を操作できるかどうかを返す。
//
// 反映中と、削除の途中は、操作できない(排他制御)。
//
// 戻り値:
//   - 操作できるなら true。
func (deploy Deploy) CanEditSettings() bool {
	// 反映中でも、削除の途中でもなければ、操作できる。
	return !deploy.IsApplying() && !deploy.IsBeingDeleted()
}

// CanStartApply は、設定の反映(apply、スケーリング)を始められるかどうかを返す。
//
// 設定を操作できて、かつ、作成の起動中でないときに、始められる。
//
// 戻り値:
//   - 始められるなら true。
func (deploy Deploy) CanStartApply() bool {
	// 操作できる状態で、起動中でなければ始められる。
	return deploy.CanEditSettings() && !deploy.IsStarting()
}

// CanDelete は、deploy の削除を受け付けられるかどうかを返す。
//
// 反映中、起動中、削除中は受け付けない。削除に失敗して止まっている(delete_failed)ものは、もう一度受け付ける。
//
// 戻り値:
//   - 受け付けられるなら true。
func (deploy Deploy) CanDelete() bool {
	// 反映中、起動中、削除中のどれでもなければ受け付ける。
	return !deploy.IsApplying() && !deploy.IsStarting() && deploy.Status != DeployStatusDeleting
}

// CanAttachService は、deploy に service を作れる(付けられる)かどうかを返す。
//
// 戻り値:
//   - 作れるなら true。
func (deploy Deploy) CanAttachService() bool {
	// 削除の途中でなければ作れる。
	return !deploy.IsBeingDeleted()
}

// CanStopForCrashLoop は、crash-loop のために watcher が止められる状態かどうかを返す。
//
// 稼働中(active)のものだけを止める。
//
// 戻り値:
//   - 止められるなら true。
func (deploy Deploy) CanStopForCrashLoop() bool {
	// 稼働中のものだけ。
	return deploy.IsActive()
}
