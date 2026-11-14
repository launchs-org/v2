package repository

import (
	"context"
	"fmt"
	"launchs/shared/model"
	"testing"
	"time"
)

// TestDeployObservationRepositoryListCrashLoopCandidates は、止めるべき deploy だけが候補になること
// (時間・再起動の回数・状態・他に正常な Pod があるか)を、境界を含めて確認する。
func TestDeployObservationRepositoryListCrashLoopCandidates(testingT *testing.T) {
	limits := CrashLoopLimits{UnhealthyFor: 10 * time.Minute}
	now := time.Now()
	// 1つの deploy に、Pod を1つ(または複数)入れて、候補になるかを確かめる。
	type podSpec struct {
		restarts      int
		unhealthyAgo  time.Duration // 0 なら unhealthy_since は NULL
		ready         bool
		startedAgo    time.Duration // 0 なら起動していない
		podStatus     string
		waitingReason string
	}
	testCases := []struct {
		name         string
		deployStatus string
		replicas     int
		pods         []podSpec
		expected     bool
	}{
		{name: "10分以上、安定していなければ候補", deployStatus: "active", replicas: 1, pods: []podSpec{{unhealthyAgo: 11 * time.Minute, waitingReason: "CrashLoopBackOff"}}, expected: true},
		{name: "9分なら、まだ候補ではない(一時的な障害は、k8s のバックオフに乗り越えさせる)", deployStatus: "active", replicas: 1, pods: []podSpec{{unhealthyAgo: 9 * time.Minute}}, expected: false},
		{name: "再起動が100回でも、不健全が3分なら候補ではない(時間だけで判断する。一度のクラッシュで止めない)", deployStatus: "active", replicas: 1, pods: []podSpec{{restarts: 100, unhealthyAgo: 3 * time.Minute}}, expected: false},
		{name: "再起動が多くても、安定して動いている(不健全の印が NULL)なら候補ではない(再起動の回数では止めない)", deployStatus: "active", replicas: 1, pods: []podSpec{{restarts: 50, ready: true, startedAgo: time.Hour}}, expected: false},
		{name: "不健全の印が古く残っていても、いま安定していれば候補ではない", deployStatus: "active", replicas: 1, pods: []podSpec{{restarts: 50, unhealthyAgo: 3 * time.Hour, ready: true, startedAgo: 10 * time.Minute}}, expected: false},
		{name: "一瞬 Ready でも、起動から日が浅ければ(クラッシュの繰り返し)候補", deployStatus: "active", replicas: 1, pods: []podSpec{{unhealthyAgo: 15 * time.Minute, ready: true, startedAgo: 3 * time.Second}}, expected: true},
		{name: "他に、安定して動いている Pod があれば、不調な Pod があっても候補ではない", deployStatus: "active", replicas: 2,
			pods: []podSpec{{unhealthyAgo: 15 * time.Minute, waitingReason: "Unschedulable"}, {ready: true, startedAgo: time.Hour}}, expected: false},
		{name: "終了処理中の Pod は数えない(ローリング更新で消える古い Pod)", deployStatus: "active", replicas: 1, pods: []podSpec{{unhealthyAgo: 15 * time.Minute, podStatus: "terminating"}}, expected: false},
		{name: "終了処理中の Pod が安定していても、それでは「正常な Pod がある」とはみなさない", deployStatus: "active", replicas: 1,
			pods: []podSpec{{unhealthyAgo: 15 * time.Minute}, {ready: true, startedAgo: time.Hour, podStatus: "terminating"}}, expected: true},
		{name: "active でなければ候補ではない(反映中)", deployStatus: "applying", replicas: 1, pods: []podSpec{{unhealthyAgo: 15 * time.Minute}}, expected: false},
		{name: "active でなければ候補ではない(起動中)", deployStatus: "pending", replicas: 1, pods: []podSpec{{unhealthyAgo: 15 * time.Minute}}, expected: false},
		{name: "active でなければ候補ではない(すでに failed)", deployStatus: "failed", replicas: 1, pods: []podSpec{{unhealthyAgo: 15 * time.Minute}}, expected: false},
		{name: "active でなければ候補ではない(削除中)", deployStatus: "deleting", replicas: 1, pods: []podSpec{{unhealthyAgo: 15 * time.Minute}}, expected: false},
		{name: "希望するレプリカが 0 なら候補ではない", deployStatus: "active", replicas: 0, pods: []podSpec{{unhealthyAgo: 15 * time.Minute}}, expected: false},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			database, observationRepository := newObservationTestEnvironment(subTest)
			database.Exec("UPDATE deploys SET status = ?, replicas = ? WHERE id = 'deploy-00000001'", testCase.deployStatus, testCase.replicas)
			for index, spec := range testCase.pods {
				pod := newTestPod(now)
				pod.PodUID, pod.PodName = fmt.Sprintf("uid-%d", index), fmt.Sprintf("pod-%d", index)
				pod.Status, pod.Ready, pod.RestartCount, pod.WaitingReason = model.PodStatusRunning, spec.ready, spec.restarts, spec.waitingReason
				if spec.podStatus != "" {
					pod.Status = spec.podStatus
				}
				if spec.startedAgo > 0 {
					startedAt := now.Add(-spec.startedAgo)
					pod.StartedAt = &startedAt
				}
				if spec.unhealthyAgo > 0 {
					unhealthySince := now.Add(-spec.unhealthyAgo)
					pod.UnhealthySince = &unhealthySince
				}
				// ApplyPod は不健全の時刻を計算し直すので、行を直接入れる。
				if createError := database.Create(&pod).Error; createError != nil {
					subTest.Fatalf("入れられるはず: %v", createError)
				}
			}

			candidates, listError := observationRepository.ListCrashLoopCandidates(context.Background(), limits)

			if listError != nil || (len(candidates) == 1) != testCase.expected || len(candidates) > 1 {
				subTest.Fatalf("候補の有無が違う(期待 %v): %v %+v", testCase.expected, listError, candidates)
			}
			if testCase.expected && candidates[0].DeployID != "deploy-00000001" {
				subTest.Errorf("deploy が違う: %+v", candidates[0])
			}
		})
	}

	testingT.Run("複数の Pod が不健全なら、最も長く不健全な Pod を根拠に、deploy ごとに1件だけ返す", func(subTest *testing.T) {
		database, observationRepository := newObservationTestEnvironment(subTest)
		for index, minutes := range []int{15, 90, 25} {
			pod := newTestPod(now)
			pod.PodUID, pod.PodName, pod.Status = fmt.Sprintf("uid-%d", index), fmt.Sprintf("pod-%d", index), model.PodStatusRunning
			unhealthySince := now.Add(-time.Duration(minutes) * time.Minute)
			pod.UnhealthySince, pod.WaitingReason, pod.RestartCount = &unhealthySince, "CrashLoopBackOff", 7
			database.Create(&pod)
		}
		database.Exec("UPDATE deploys SET status = 'active' WHERE id = 'deploy-00000001'")
		candidates, listError := observationRepository.ListCrashLoopCandidates(context.Background(), limits)
		if listError != nil || len(candidates) != 1 || candidates[0].PodName != "pod-1" || candidates[0].UnhealthyFor < 89*time.Minute ||
			candidates[0].WaitingReason != "CrashLoopBackOff" || candidates[0].RestartCount != 7 {
			subTest.Errorf("pod-1(90分)を根拠に1件のはず: %v %+v", listError, candidates)
		}
	})
	testingT.Run("不正な上限は拒否する(全ての deploy を止めてしまうため)", func(subTest *testing.T) {
		_, observationRepository := newObservationTestEnvironment(subTest)
		for _, invalid := range []CrashLoopLimits{{}, {UnhealthyFor: 0}, {UnhealthyFor: -time.Minute}} {
			if _, listError := observationRepository.ListCrashLoopCandidates(context.Background(), invalid); listError == nil {
				subTest.Errorf("拒否されるはず: %+v", invalid)
			}
		}
	})
}
