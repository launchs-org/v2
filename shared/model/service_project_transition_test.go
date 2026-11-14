package model

import "testing"

// TestServiceTransitions は、service の作成の確定と削除の遷移が、仕様どおりであることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestServiceTransitions(testing *testing.T) {
	// 作成の成功: active になり、DNS 名と ClusterIP が入る(空の ClusterIP は NULL のまま)。
	created := Service{Status: ServiceStatusPending}
	if !created.CompleteCreation(true, "dns", "") || created.Status != ServiceStatusActive || *created.DNSName != "dns" || created.ClusterIP != nil {
		testing.Errorf("作成の成功が違う: %+v", created)
	}
	// 作成の失敗: failed。作成中でなければ何もしない。
	failed := Service{Status: ServiceStatusPending}
	if !failed.CompleteCreation(false, "", "") || failed.Status != ServiceStatusFailed || failed.CompleteCreation(true, "dns", "ip") {
		testing.Error("作成の失敗が違う")
	}
	// 削除: 受け付け(前の状態が返る)→ 戻す / 失敗にする。
	active := Service{Status: ServiceStatusActive}
	previous, deleteError := active.BeginDelete()
	if deleteError != nil || previous != ServiceStatusActive || active.Status != ServiceStatusDeleting {
		testing.Error("削除の受け付けが違う")
	}
	if _, secondError := active.BeginDelete(); secondError == nil {
		testing.Error("削除中は conflict のはず")
	}
	if !active.RevertDeleting(previous) || active.Status != ServiceStatusActive || active.MarkDeleteFailed() {
		testing.Error("削除の戻しが違う")
	}
}

// TestProjectTransitions は、project の削除の遷移が、仕様どおりであることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestProjectTransitions(testing *testing.T) {
	// active と delete_failed は受け付ける。deleting は受け付けない。
	project := Project{Status: ProjectStatusDeleteFailed}
	previous, deleteError := project.BeginDelete()
	if deleteError != nil || previous != ProjectStatusDeleteFailed || project.Status != ProjectStatusDeleting {
		testing.Error("削除の受け付けが違う")
	}
	if _, secondError := project.BeginDelete(); secondError == nil {
		testing.Error("削除中は conflict のはず")
	}
	// 削除失敗にでき、削除中でなければ何もしない。
	if !project.MarkDeleteFailed() || project.Status != ProjectStatusDeleteFailed || project.MarkDeleteFailed() || project.RevertDeleting(ProjectStatusActive) {
		testing.Error("削除失敗の遷移が違う")
	}
}
