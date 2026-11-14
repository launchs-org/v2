package repository

import (
	"fmt"

	"launchs/shared/model"
)

// ignoredWaitingReasons は、正常な起動の途中で出る、待っている理由。イベントにはしない。
var ignoredWaitingReasons = map[string]bool{
	"":                  true,
	"ContainerCreating": true,
	"PodInitializing":   true,
}

// DiffPodEvents は、Pod の前の状態と今の状態を比べて、起きた状態変化のイベントを返す。
//
// 前の状態は、DB に保存してある行。watcher が止まっていた間の変化も、再開したときの、この比較で拾える。
// 検出するもの:
//   - 前の状態が無い: created
//   - Ready になった: ready / Ready でなくなった: not_ready
//   - 再起動の回数が増えた: restarted(最後に終了した理由と終了コードを付ける)
//   - 異常な理由(CrashLoopBackOff、ImagePullBackOff など)で待ち始めた: waiting
//   - 終了の処理に入った: terminating
//
// 引数:
//   - previous: DB にある前の状態。無ければ nil。
//   - next: 観測した今の状態。
//
// 戻り値:
//   - 起きた順のイベント(DeployID、PodUID、PodName、OccurredAt を設定済み。ID は未設定)。変化が無ければ空。
func DiffPodEvents(previous *model.DeployPod, next model.DeployPod) []model.DeployPodEvent {
	// イベントを作る道具。共通の項目を設定する。
	var events []model.DeployPodEvent
	appendEvent := func(eventType string, reason string, message string) {
		events = append(events, model.DeployPodEvent{
			DeployID: next.DeployID, PodUID: next.PodUID, PodName: next.PodName,
			Type: eventType, Reason: reason, Message: message, OccurredAt: next.ObservedAt,
		})
	}

	// 前の状態が無いときは、作られたことだけを記録する(以降の差分は、次の観測から)。
	if previous == nil {
		appendEvent(model.PodEventCreated, "", "")
		// 作られた時点で、すでに異常な理由で待っているなら、それも記録する。
		if !ignoredWaitingReasons[next.WaitingReason] {
			appendEvent(model.PodEventWaiting, next.WaitingReason, "")
		}
		return events
	}

	// Ready の変化。
	if !previous.Ready && next.Ready {
		appendEvent(model.PodEventReady, "", "")
	}
	if previous.Ready && !next.Ready {
		appendEvent(model.PodEventNotReady, "", "")
	}
	// 再起動の回数の増加(クラッシュなど)。何回増えたかと、最後に終了した理由を残す。
	if next.RestartCount > previous.RestartCount {
		message := fmt.Sprintf("再起動 %d 回", next.RestartCount-previous.RestartCount)
		if next.LastExitCode != nil {
			message += fmt.Sprintf("、終了コード %d", *next.LastExitCode)
		}
		appendEvent(model.PodEventRestarted, next.LastTerminationReason, message)
	}
	// 異常な理由で待ち始めた(理由が変わったときだけ)。
	if next.WaitingReason != previous.WaitingReason && !ignoredWaitingReasons[next.WaitingReason] {
		appendEvent(model.PodEventWaiting, next.WaitingReason, "")
	}
	// 終了の処理に入った。
	if previous.Status != model.PodStatusTerminating && next.Status == model.PodStatusTerminating {
		appendEvent(model.PodEventTerminating, "", "")
	}
	return events
}
