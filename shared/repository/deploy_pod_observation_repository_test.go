package repository

import (
	"context"
	"launchs/shared/apperror"
	"launchs/shared/model"
	"testing"
	"time"
)

// TestDeployObservationRepositoryPods は、Pod の状態の保存と、差分によるイベントの追記、
// 消えた Pod の削除、deploy を消したときの連鎖削除を確認する。
func TestDeployObservationRepositoryPods(testingT *testing.T) {
	database, observationRepository := newObservationTestEnvironment(testingT)
	now := time.Now()
	pod := newTestPod(now)

	testingT.Run("最初の保存で created が追記される", func(subTest *testing.T) {
		events, applyError := observationRepository.ApplyPod(context.Background(), pod)
		if applyError != nil || len(events) != 1 || events[0].Type != model.PodEventCreated {
			subTest.Fatalf("created が追記されるはず: %v %+v", applyError, events)
		}
	})
	testingT.Run("同じ状態を保存しても、イベントは増えない", func(subTest *testing.T) {
		events, applyError := observationRepository.ApplyPod(context.Background(), pod)
		if applyError != nil || len(events) != 0 {
			subTest.Errorf("増えないはず: %v %+v", applyError, events)
		}
	})
	testingT.Run("Ready と再起動の変化がイベントになり、行が更新される", func(subTest *testing.T) {
		exitCode := 1
		pod.Status, pod.Ready, pod.RestartCount, pod.LastTerminationReason, pod.LastExitCode = model.PodStatusRunning, true, 1, "Error", &exitCode
		events, applyError := observationRepository.ApplyPod(context.Background(), pod)
		if applyError != nil || len(events) != 2 || events[0].Type != "ready" || events[1].Type != "restarted" {
			subTest.Fatalf("ready と restarted のはず: %v %v", applyError, eventTypes(events))
		}
		pods, _ := observationRepository.ListPods(context.Background())
		if len(pods) != 1 || !pods[0].Ready || pods[0].RestartCount != 1 || pods[0].LastExitCode == nil || *pods[0].LastExitCode != 1 {
			subTest.Errorf("行が更新されているはず: %+v", pods)
		}
	})
	testingT.Run("イベントを、新しい順に取得できる", func(subTest *testing.T) {
		events, listError := observationRepository.ListPodEvents(context.Background(), "deploy-00000001", 2)
		if listError != nil || len(events) != 2 || events[0].Type != "restarted" {
			subTest.Errorf("新しい順に2件のはず: %v %v", listError, eventTypes(events))
		}
	})
	testingT.Run("deploy の行が無ければ、保存せず、エラーにもしない", func(subTest *testing.T) {
		orphan := newTestPod(now)
		orphan.PodUID, orphan.DeployID, orphan.PodName = "uid-x", "deploy-99999999", "pod-x"
		events, applyError := observationRepository.ApplyPod(context.Background(), orphan)
		if applyError != nil || len(events) != 0 {
			subTest.Errorf("保存されず、エラーにもならないはず: %v %v", applyError, events)
		}
	})
	testingT.Run("消えた Pod を削除すると、deleted が追記され、繰り返しても壊れない", func(subTest *testing.T) {
		deleted, deleteError := observationRepository.DeletePod(context.Background(), "project-00000001", "pod-1", now)
		if deleteError != nil || !deleted {
			subTest.Fatalf("削除できるはず: %v %v", deleted, deleteError)
		}
		if again, _ := observationRepository.DeletePod(context.Background(), "project-00000001", "pod-1", now); again {
			subTest.Errorf("2回目は何も削除しないはず")
		}
		events, _ := observationRepository.ListPodEvents(context.Background(), "deploy-00000001", 10)
		if events[0].Type != model.PodEventDeleted {
			subTest.Errorf("最新が deleted のはず: %v", eventTypes(events))
		}
		if pods, _ := observationRepository.ListPods(context.Background()); len(pods) != 0 {
			subTest.Errorf("行が無くなるはず: %+v", pods)
		}
	})
	testingT.Run("deploy の行を消すと、観測値・Pod・イベントも消える", func(subTest *testing.T) {
		_, _ = observationRepository.UpsertDeployment(context.Background(), model.DeployObservation{DeployID: "deploy-00000001", ObservedAt: now})
		_, _ = observationRepository.ApplyPod(context.Background(), pod)
		if deleteError := database.Exec("DELETE FROM deploys WHERE id = 'deploy-00000001'").Error; deleteError != nil {
			subTest.Fatalf("消せるはず: %v", deleteError)
		}
		var observationCount, podCount, eventCount int64
		database.Table("deploy_observations").Count(&observationCount)
		database.Table("deploy_pods").Count(&podCount)
		database.Table("deploy_pod_events").Count(&eventCount)
		if observationCount != 0 || podCount != 0 || eventCount != 0 {
			subTest.Errorf("全て消えるはず: %d %d %d", observationCount, podCount, eventCount)
		}
	})
	testingT.Run("空の条件は拒否する", func(subTest *testing.T) {
		if _, applyError := observationRepository.ApplyPod(context.Background(), model.DeployPod{}); applyError == nil {
			subTest.Errorf("拒否されるはず")
		}
		if _, deleteError := observationRepository.DeletePod(context.Background(), "", "", time.Now()); deleteError == nil || apperror.KindOf(deleteError) == "" {
			subTest.Errorf("拒否されるはず")
		}
		if _, listError := observationRepository.ListPodEvents(context.Background(), "", 10); listError == nil {
			subTest.Errorf("拒否されるはず")
		}
	})
}

// TestDeployObservationRepositoryListPodsByDeploy は、deploy の Pod だけが、作られた順に返り、
// 他の deploy の Pod は含まれず、無ければ空になることを確認する。
func TestDeployObservationRepositoryListPodsByDeploy(testingT *testing.T) {
	// deploy を2件用意し、Pod を、作られた時刻が前後するように入れる。
	database, observationRepository := newObservationTestEnvironment(testingT)
	database.Create(newTestDeploy(2, "project-00000001", "user-a"))
	now := time.Now()
	podAt := func(uid string, deployID string, createdAt time.Time) model.DeployPod {
		pod := newTestPod(now)
		pod.PodUID, pod.PodName, pod.DeployID, pod.PodCreatedAt = uid, "name-"+uid, deployID, createdAt
		return pod
	}
	for _, pod := range []model.DeployPod{
		podAt("uid-b", "deploy-00000001", now.Add(-time.Minute)),
		podAt("uid-a", "deploy-00000001", now.Add(-2*time.Minute)),
		podAt("uid-c", "deploy-00000001", now.Add(-time.Minute)),
		podAt("uid-x", "deploy-00000002", now),
	} {
		if _, applyError := observationRepository.ApplyPod(context.Background(), pod); applyError != nil {
			testingT.Fatalf("保存できるはず: %v", applyError)
		}
	}

	testingT.Run("その deploy の Pod だけが、作られた順(同時刻は UID の順)に返る", func(subTest *testing.T) {
		pods, listError := observationRepository.ListPodsByDeploy(context.Background(), "deploy-00000001")
		if listError != nil || len(pods) != 3 || pods[0].PodUID != "uid-a" || pods[1].PodUID != "uid-b" || pods[2].PodUID != "uid-c" {
			subTest.Errorf("uid-a、uid-b、uid-c の順のはず: %v %+v", listError, pods)
		}
	})
	testingT.Run("Pod が無い deploy は、空になる", func(subTest *testing.T) {
		pods, listError := observationRepository.ListPodsByDeploy(context.Background(), "deploy-99999999")
		if listError != nil || len(pods) != 0 {
			subTest.Errorf("空のはず: %v %+v", listError, pods)
		}
	})
	testingT.Run("消えた Pod は、一覧に出ない", func(subTest *testing.T) {
		_, _ = observationRepository.DeletePod(context.Background(), "project-00000001", "name-uid-a", now)
		pods, _ := observationRepository.ListPodsByDeploy(context.Background(), "deploy-00000001")
		if len(pods) != 2 || pods[0].PodUID != "uid-b" {
			subTest.Errorf("2件になるはず: %+v", pods)
		}
	})
	testingT.Run("空の ID は拒否する", func(subTest *testing.T) {
		if _, listError := observationRepository.ListPodsByDeploy(context.Background(), ""); listError == nil {
			subTest.Errorf("拒否されるはず(全ての Pod を返さない)")
		}
	})
}

// TestDeployObservationRepositoryApplyPodTracksUnhealthySince は、Pod の保存のたびに、「不健全になった時刻」が、
// 不健全の間は保たれ、安定したら消えることを確認する。
func TestDeployObservationRepositoryApplyPodTracksUnhealthySince(testingT *testing.T) {
	database, observationRepository := newObservationTestEnvironment(testingT)
	base := time.Now().Add(-time.Hour)
	unhealthySinceOf := func() *time.Time {
		var pod model.DeployPod
		database.Where("pod_uid = ?", "uid-1").Take(&pod)
		return pod.UnhealthySince
	}
	apply := func(observedAt time.Time, ready bool, startedAgo time.Duration) {
		pod := newTestPod(observedAt)
		pod.Status, pod.Ready = model.PodStatusRunning, ready
		if startedAgo > 0 {
			startedAt := observedAt.Add(-startedAgo)
			pod.StartedAt = &startedAt
		}
		if _, applyError := observationRepository.ApplyPod(context.Background(), pod); applyError != nil {
			testingT.Fatalf("保存できるはず: %v", applyError)
		}
	}

	apply(base, false, 0)
	first := unhealthySinceOf()
	testingT.Run("最初の保存(起動待ち)で、観測した時刻から数え始める", func(subTest *testing.T) {
		if first == nil || first.Sub(base).Abs() > time.Second {
			subTest.Errorf("観測した時刻のはず: %v", first)
		}
	})
	testingT.Run("不健全が続く間は、時刻が保たれる(一瞬 Ready になっても)", func(subTest *testing.T) {
		// クラッシュの繰り返しでは、イベントが短い間隔で届く(一瞬 Ready、すぐ Ready でなくなる、を繰り返す)。
		apply(base.Add(10*time.Minute), true, 5*time.Second)
		apply(base.Add(10*time.Minute+30*time.Second), false, 0)
		apply(base.Add(20*time.Minute), false, 0)
		if kept := unhealthySinceOf(); kept == nil || kept.Sub(*first).Abs() > time.Second {
			subTest.Errorf("最初の時刻のままのはず: %v 対 %v", kept, first)
		}
	})
	testingT.Run("5分以上 Ready で動き続ければ、消える", func(subTest *testing.T) {
		apply(base.Add(30*time.Minute), true, 6*time.Minute)
		if cleared := unhealthySinceOf(); cleared != nil {
			subTest.Errorf("nil のはず: %v", cleared)
		}
	})
	testingT.Run("安定して動いていたあとで崩れたら、その時刻から数え直す", func(subTest *testing.T) {
		crashedAt := base.Add(50 * time.Minute)
		apply(crashedAt, false, 0)
		if restarted := unhealthySinceOf(); restarted == nil || restarted.Sub(crashedAt).Abs() > time.Second {
			subTest.Errorf("崩れた時刻のはず: %v", restarted)
		}
	})
}
