package repository

import (
	"launchs/shared/model"
	"testing"
	"time"
)

// TestDiffPodEvents は、Pod の前後の状態から、状態変化のイベントが決まることを確認する。
func TestDiffPodEvents(testingT *testing.T) {
	// 基準となる、動いている Pod。
	now := time.Now()
	exitCode := 137
	running := model.DeployPod{PodUID: "uid", DeployID: "d", PodName: "p", Status: model.PodStatusRunning, Ready: true, ObservedAt: now}
	testCases := []struct {
		name     string
		previous *model.DeployPod
		next     model.DeployPod
		expected []string
	}{
		{name: "前が無ければ created", previous: nil, next: running, expected: []string{"created"}},
		{name: "前が無く、最初から ImagePullBackOff なら created と waiting", previous: nil,
			next: model.DeployPod{PodUID: "uid", WaitingReason: "ImagePullBackOff"}, expected: []string{"created", "waiting"}},
		{name: "前が無く、ContainerCreating なら created だけ", previous: nil,
			next: model.DeployPod{PodUID: "uid", WaitingReason: "ContainerCreating"}, expected: []string{"created"}},
		{name: "変化が無ければ何もない", previous: &running, next: running, expected: nil},
		{name: "Ready になれば ready", previous: &model.DeployPod{Status: model.PodStatusRunning}, next: running, expected: []string{"ready"}},
		{name: "Ready でなくなれば not_ready", previous: &running, next: model.DeployPod{PodUID: "uid", Status: model.PodStatusRunning}, expected: []string{"not_ready"}},
		{name: "再起動が増えれば restarted(理由と終了コード付き)", previous: &running,
			next:     model.DeployPod{PodUID: "uid", Status: model.PodStatusRunning, Ready: true, RestartCount: 2, LastTerminationReason: "OOMKilled", LastExitCode: &exitCode},
			expected: []string{"restarted"}},
		{name: "クラッシュして待ち始めれば、not_ready、restarted、waiting の順", previous: &running,
			next:     model.DeployPod{PodUID: "uid", Status: model.PodStatusRunning, RestartCount: 1, WaitingReason: "CrashLoopBackOff", LastTerminationReason: "Error", LastExitCode: &exitCode},
			expected: []string{"not_ready", "restarted", "waiting"}},
		{name: "同じ理由で待ち続けても waiting を重ねない", previous: &model.DeployPod{WaitingReason: "CrashLoopBackOff"},
			next: model.DeployPod{WaitingReason: "CrashLoopBackOff"}, expected: nil},
		{name: "終了の処理に入れば terminating", previous: &running,
			next: model.DeployPod{PodUID: "uid", Status: model.PodStatusTerminating, Ready: true}, expected: []string{"terminating"}},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			events := DiffPodEvents(testCase.previous, testCase.next)
			got := eventTypes(events)
			if len(got) != len(testCase.expected) {
				subTest.Fatalf("イベントが違う: 期待 %v、実際 %v", testCase.expected, got)
			}
			for index := range got {
				if got[index] != testCase.expected[index] {
					subTest.Fatalf("イベントが違う: 期待 %v、実際 %v", testCase.expected, got)
				}
			}
		})
	}
	// 再起動のイベントには、理由と終了コードが入る。
	testingT.Run("restarted には、理由と終了コードが入る", func(subTest *testing.T) {
		events := DiffPodEvents(&running, model.DeployPod{PodUID: "uid", Status: model.PodStatusRunning, Ready: true, RestartCount: 1, LastTerminationReason: "OOMKilled", LastExitCode: &exitCode})
		if len(events) != 1 || events[0].Reason != "OOMKilled" || events[0].Message == "" {
			subTest.Errorf("理由と終了コードが入るはず: %+v", events)
		}
	})
}
