package model

import (
	"testing"

	"gorm.io/datatypes"
)

// TestDeployCommitConfig は、起動設定の結果の確定が、仕様どおりであることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestDeployCommitConfig(testing *testing.T) {
	// 成功: pending が正式な値になり、pending が空になり、落ち着いた状態になる。
	command := "run"
	deploy := Deploy{Status: DeployStatusApplying, Replicas: 0, Env: datatypes.JSON(`{"A":"1"}`),
		PendingEnv: datatypes.JSON(`{"A":"2"}`), PendingCommand: &command}
	if !deploy.CommitConfig(true) {
		testing.Fatal("確定できるはず")
	}
	if string(deploy.Env) != `{"A":"2"}` || deploy.PendingEnv != nil || deploy.PendingCommand != nil || deploy.Command == nil {
		testing.Errorf("pending が昇格していない: %+v", deploy)
	}
	if deploy.Status != DeployStatusScaledZero {
		testing.Errorf("replicas 0 は scaled_zero のはず: %s", deploy.Status)
	}
	// 失敗: 値は変わらず、pending が残り、failed になる。
	failing := Deploy{Status: DeployStatusApplying, Replicas: 1, Env: datatypes.JSON(`{"A":"1"}`), PendingEnv: datatypes.JSON(`{"A":"2"}`)}
	if !failing.CommitConfig(false) || failing.Status != DeployStatusFailed || string(failing.Env) != `{"A":"1"}` || failing.PendingEnv == nil {
		testing.Errorf("失敗の確定が違う: %+v", failing)
	}
	// applying でないなら、何も変えない。
	idle := Deploy{Status: DeployStatusActive, PendingEnv: datatypes.JSON(`{}`)}
	if idle.CommitConfig(true) || idle.Status != DeployStatusActive {
		testing.Error("applying でないときは何も変えないはず")
	}
}

// TestDeployBeginOperations は、反映・スケーリング・削除の開始が、状態の規則を守ることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestDeployBeginOperations(testing *testing.T) {
	// 適用する値が無い反映は conflict。
	active := Deploy{Status: DeployStatusActive, Replicas: 1}
	if active.BeginConfigApply() == nil {
		testing.Error("pending が無ければ conflict のはず")
	}
	// 同じ replicas は、変えない。
	changed, scaleError := active.BeginScale(1)
	if scaleError != nil || changed || active.Status != DeployStatusActive {
		testing.Error("同じ数は何も変えないはず")
	}
	// 違う replicas は、applying になる。
	changed, scaleError = active.BeginScale(3)
	if scaleError != nil || !changed || active.Replicas != 3 || active.Status != DeployStatusApplying {
		testing.Errorf("スケーリングの開始が違う: %+v", active)
	}
	// 反映中は、どの操作も受け付けない。
	if _, scaleError = active.BeginScale(2); scaleError == nil {
		testing.Error("applying 中は conflict のはず")
	}
	if _, deleteError := active.BeginDelete(); deleteError == nil {
		testing.Error("applying 中の削除は conflict のはず")
	}
	// 失敗した deploy は、削除でき、前の状態が返る。
	failed := Deploy{Status: DeployStatusFailed}
	previous, deleteError := failed.BeginDelete()
	if deleteError != nil || previous != DeployStatusFailed || failed.Status != DeployStatusDeleting {
		testing.Error("削除の受け付けが違う")
	}
}
