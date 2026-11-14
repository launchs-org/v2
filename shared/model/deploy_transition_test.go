package model

import (
	"testing"
	"time"

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

// TestDeployRevertAndFinish は、失敗の戻しと crash-loop の停止が、期待する状態のときだけ働くことを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestDeployRevertAndFinish(testing *testing.T) {
	// 削除中だけが、戻せて、delete_failed にできる。
	deleting := Deploy{Status: DeployStatusDeleting}
	if !deleting.MarkDeleteFailed() || deleting.Status != DeployStatusDeleteFailed {
		testing.Error("deleting は delete_failed にできるはず")
	}
	if deleting.RevertDeleting(DeployStatusActive) || deleting.MarkDeleteFailed() {
		testing.Error("deleting でなければ何もしないはず")
	}
	// スケーリングの戻し: applying のときだけ、replicas と状態が戻る。
	applying := Deploy{Status: DeployStatusApplying, Replicas: 5}
	if !applying.RevertScale(2, DeployStatusActive) || applying.Replicas != 2 || applying.Status != DeployStatusActive {
		testing.Error("スケーリングの戻しが違う")
	}
	if applying.RevertScale(9, DeployStatusFailed) || applying.Replicas != 2 {
		testing.Error("applying でなければ戻さないはず")
	}
	// crash-loop の停止: active → applying → failed。
	active := Deploy{Status: DeployStatusActive}
	if !active.BeginCrashLoopStop() || !active.FinishCrashLoopStop() || active.Status != DeployStatusFailed {
		testing.Error("停止の流れが違う")
	}
	if active.BeginCrashLoopStop() {
		testing.Error("active でなければ止められないはず")
	}
}

// TestDeployCrashLoopingPod は、止めるべき deploy の判定が、仕様どおりであることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestDeployCrashLoopingPod(testing *testing.T) {
	// 基準の時刻と、猶予の時間。
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	threshold := 10 * time.Minute
	// 時刻を作る補助。
	ago := func(duration time.Duration) *time.Time {
		moment := now.Add(-duration)
		return &moment
	}
	// 不健全な Pod(猶予を超えている)。
	crashing := DeployPod{PodUID: "crash", UnhealthySince: ago(30 * time.Minute)}
	// 猶予に届かない Pod。
	recent := DeployPod{PodUID: "recent", UnhealthySince: ago(2 * time.Minute)}
	// 安定している Pod。
	stable := DeployPod{PodUID: "stable", Ready: true, StartedAt: ago(time.Hour)}
	// 終了中の Pod。
	terminating := DeployPod{PodUID: "terminating", Status: PodStatusTerminating, UnhealthySince: ago(time.Hour)}

	// 猶予を超えた不健全な Pod だけなら、止める。
	if pod, stop := (Deploy{Status: DeployStatusActive, Replicas: 2, Pods: []DeployPod{recent, crashing}}).CrashLoopingPod(now, threshold); !stop || pod.PodUID != "crash" {
		testing.Error("止めるはず")
	}
	// 安定した Pod が1つでもあれば、止めない。
	if _, stop := (Deploy{Status: DeployStatusActive, Replicas: 2, Pods: []DeployPod{crashing, stable}}).CrashLoopingPod(now, threshold); stop {
		testing.Error("安定した Pod があれば止めないはず")
	}
	// 猶予に届かない、または終了中だけなら、止めない。
	if _, stop := (Deploy{Status: DeployStatusActive, Replicas: 1, Pods: []DeployPod{recent, terminating}}).CrashLoopingPod(now, threshold); stop {
		testing.Error("猶予内と終了中だけなら止めないはず")
	}
	// active でない、または replicas が 0 なら、止めない。
	if _, stop := (Deploy{Status: DeployStatusFailed, Replicas: 1, Pods: []DeployPod{crashing}}).CrashLoopingPod(now, threshold); stop {
		testing.Error("active でなければ止めないはず")
	}
	if _, stop := (Deploy{Status: DeployStatusActive, Replicas: 0, Pods: []DeployPod{crashing}}).CrashLoopingPod(now, threshold); stop {
		testing.Error("replicas 0 は止めないはず")
	}
}
