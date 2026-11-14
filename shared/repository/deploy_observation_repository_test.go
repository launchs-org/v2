package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// newObservationTestEnvironment は、使い捨てのデータベースと、deploy が1件ある状態を用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - データベース。
//   - その上の DeployObservationRepository。
func newObservationTestEnvironment(testingT *testing.T) (*gorm.DB, *DeployObservationRepository) {
	// deploy が1件ある状態にする。
	database, _ := newDeployTestEnvironment(testingT)
	database.Create(newTestDeploy(1, "project-00000001", "user-a"))
	return database, NewDeployObservationRepository(database)
}

// newTestPod は、テスト用の Pod の状態を作る。
//
// 引数:
//   - observedAt: 観測した時刻。
//
// 戻り値:
//   - deploy-00000001 の、起動待ちの Pod。
func newTestPod(observedAt time.Time) model.DeployPod {
	return model.DeployPod{
		PodUID: "uid-1", DeployID: "deploy-00000001", ProjectID: "project-00000001", PodName: "pod-1",
		Status: model.PodStatusPending, Phase: "Pending", PodCreatedAt: observedAt, ObservedAt: observedAt,
	}
}

// eventTypes は、イベントの種類だけを、順に並べる。
//
// 引数:
//   - events: イベントの一覧。
//
// 戻り値:
//   - 種類の一覧。
func eventTypes(events []model.DeployPodEvent) []string {
	// 種類だけを取り出す。
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

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

// TestDeployObservationRepositoryDeployment は、Deployment の観測値を、保存・上書き・削除でき、
// deploy の行が無いときは保存されないことを確認する。
func TestDeployObservationRepositoryDeployment(testingT *testing.T) {
	database, observationRepository := newObservationTestEnvironment(testingT)
	observation := model.DeployObservation{DeployID: "deploy-00000001", Generation: 2, ObservedGeneration: 1, ReadyReplicas: 0, ObservedAt: time.Now()}

	testingT.Run("保存でき、上書きできる(0 の値も書ける)", func(subTest *testing.T) {
		if saved, saveError := observationRepository.UpsertDeployment(context.Background(), observation); saveError != nil || !saved {
			subTest.Fatalf("保存できるはず: %v %v", saved, saveError)
		}
		observation.ObservedGeneration, observation.Replicas, observation.ReadyReplicas = 2, 3, 3
		if saved, saveError := observationRepository.UpsertDeployment(context.Background(), observation); saveError != nil || !saved {
			subTest.Fatalf("上書きできるはず: %v %v", saved, saveError)
		}
		var stored model.DeployObservation
		database.Where("deploy_id = ?", "deploy-00000001").Take(&stored)
		if stored.ObservedGeneration != 2 || stored.ReadyReplicas != 3 {
			subTest.Errorf("上書きされているはず: %+v", stored)
		}
		// 0 に戻した値も、書ける。
		observation.ReadyReplicas = 0
		_, _ = observationRepository.UpsertDeployment(context.Background(), observation)
		database.Where("deploy_id = ?", "deploy-00000001").Take(&stored)
		if stored.ReadyReplicas != 0 {
			subTest.Errorf("0 も書けるはず: %+v", stored)
		}
	})
	testingT.Run("一覧に出て、削除できる", func(subTest *testing.T) {
		deployIDs, _ := observationRepository.ListObservedDeployIDs(context.Background())
		if len(deployIDs) != 1 || deployIDs[0] != "deploy-00000001" {
			subTest.Fatalf("1件あるはず: %v", deployIDs)
		}
		if deleteError := observationRepository.DeleteDeployment(context.Background(), "deploy-00000001"); deleteError != nil {
			subTest.Fatalf("削除できるはず: %v", deleteError)
		}
		if remaining, _ := observationRepository.ListObservedDeployIDs(context.Background()); len(remaining) != 0 {
			subTest.Errorf("無くなるはず: %v", remaining)
		}
	})
	testingT.Run("deploy の行が無ければ、保存せず、エラーにもしない", func(subTest *testing.T) {
		saved, saveError := observationRepository.UpsertDeployment(context.Background(), model.DeployObservation{DeployID: "deploy-99999999", ObservedAt: time.Now()})
		if saveError != nil || saved {
			subTest.Errorf("保存されず、エラーにもならないはず: %v %v", saved, saveError)
		}
	})
	testingT.Run("空の ID は拒否する", func(subTest *testing.T) {
		if _, saveError := observationRepository.UpsertDeployment(context.Background(), model.DeployObservation{}); saveError == nil {
			subTest.Errorf("拒否されるはず")
		}
		if deleteError := observationRepository.DeleteDeployment(context.Background(), ""); deleteError == nil {
			subTest.Errorf("拒否されるはず")
		}
	})
}

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

// TestDeployObservationRepositoryPruneEvents は、保存期間を過ぎたイベントと、1つの deploy の件数の上限を
// 超えたイベントだけが、古いものから削除され、他の deploy と新しいイベントは残ることを確認する。
func TestDeployObservationRepositoryPruneEvents(testingT *testing.T) {
	// deploy を2件用意する(deploy-00000001 と deploy-00000002)。
	database, observationRepository := newObservationTestEnvironment(testingT)
	database.Create(newTestDeploy(2, "project-00000001", "user-a"))
	now := time.Now()
	// insertEvents は、deploy に、指定した時刻のイベントを、古い順に入れる。
	insertEvents := func(deployID string, occurredAt ...time.Time) {
		for _, at := range occurredAt {
			database.Create(&model.DeployPodEvent{DeployID: deployID, PodUID: "uid", PodName: "pod", Type: model.PodEventRestarted, OccurredAt: at})
		}
	}
	countEvents := func(deployID string) int64 {
		var count int64
		database.Table("deploy_pod_events").Where("deploy_id = ?", deployID).Count(&count)
		return count
	}
	cutoff := now.Add(-30 * 24 * time.Hour)

	testingT.Run("保存期間を過ぎたものだけが削除される(境界の前後)", func(subTest *testing.T) {
		insertEvents("deploy-00000001", cutoff.Add(-time.Hour), cutoff.Add(-time.Second), cutoff.Add(time.Second), now)
		deleted, pruneError := observationRepository.PruneEvents(context.Background(), cutoff, 1000)
		if pruneError != nil || deleted != 2 || countEvents("deploy-00000001") != 2 {
			subTest.Errorf("期限切れの2件だけ消えるはず: 削除 %d、残り %d、%v", deleted, countEvents("deploy-00000001"), pruneError)
		}
	})
	testingT.Run("繰り返しても、何も消えない(冪等)", func(subTest *testing.T) {
		deleted, pruneError := observationRepository.PruneEvents(context.Background(), cutoff, 1000)
		if pruneError != nil || deleted != 0 || countEvents("deploy-00000001") != 2 {
			subTest.Errorf("何も消えないはず: %d %v", deleted, pruneError)
		}
	})
	testingT.Run("件数の上限を超えた分が、古いものから削除され、他の deploy には及ばない", func(subTest *testing.T) {
		// deploy-1 に5件(合計7件)、deploy-2 に3件を入れる。上限は3件。
		insertEvents("deploy-00000001", now, now, now, now, now)
		insertEvents("deploy-00000002", now, now, now)
		var newestBefore []int64
		database.Table("deploy_pod_events").Where("deploy_id = ?", "deploy-00000001").Order("id DESC").Limit(3).Pluck("id", &newestBefore)

		deleted, pruneError := observationRepository.PruneEvents(context.Background(), cutoff, 3)

		var remaining []int64
		database.Table("deploy_pod_events").Where("deploy_id = ?", "deploy-00000001").Order("id DESC").Pluck("id", &remaining)
		if pruneError != nil || deleted != 4 || len(remaining) != 3 || countEvents("deploy-00000002") != 3 {
			subTest.Fatalf("deploy-1 は3件に、deploy-2 は3件のままのはず: 削除 %d、%v、deploy-2 %d、%v", deleted, remaining, countEvents("deploy-00000002"), pruneError)
		}
		for index := range remaining {
			if remaining[index] != newestBefore[index] {
				subTest.Errorf("新しい3件が残るはず: %v と %v", remaining, newestBefore)
			}
		}
	})
	testingT.Run("上限ちょうどなら、何も消えない", func(subTest *testing.T) {
		deleted, pruneError := observationRepository.PruneEvents(context.Background(), cutoff, 3)
		if pruneError != nil || deleted != 0 {
			subTest.Errorf("何も消えないはず: %d %v", deleted, pruneError)
		}
	})
	testingT.Run("不正な引数は拒否し、何も消えない", func(subTest *testing.T) {
		before := countEvents("deploy-00000001") + countEvents("deploy-00000002")
		if _, pruneError := observationRepository.PruneEvents(context.Background(), time.Time{}, 3); pruneError == nil {
			subTest.Errorf("空の時刻は拒否されるはず")
		}
		if _, pruneError := observationRepository.PruneEvents(context.Background(), now, 0); pruneError == nil {
			subTest.Errorf("件数 0 は拒否されるはず(全て消えてしまう)")
		}
		if after := countEvents("deploy-00000001") + countEvents("deploy-00000002"); after != before {
			subTest.Errorf("何も消えないはず: %d → %d", before, after)
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

// TestDeployObservationRepositoryFindDeployment は、deploy の観測値を1件取得でき、無いときは「無い」だけを返し、
// 空の ID は拒否することを確認する。
func TestDeployObservationRepositoryFindDeployment(testingT *testing.T) {
	database, observationRepository := newObservationTestEnvironment(testingT)
	database.Create(newTestDeploy(2, "project-00000001", "user-a"))
	_, _ = observationRepository.UpsertDeployment(context.Background(), model.DeployObservation{DeployID: "deploy-00000001", Generation: 4, ObservedGeneration: 4, Replicas: 2, ReadyReplicas: 2, ObservedAt: time.Now()})

	testingT.Run("その deploy の観測値を返す(他の deploy のものは返さない)", func(subTest *testing.T) {
		observation, found, findError := observationRepository.FindDeployment(context.Background(), "deploy-00000001")
		if findError != nil || !found || observation.Generation != 4 || observation.ReadyReplicas != 2 {
			subTest.Errorf("観測値が返るはず: %v %v %+v", findError, found, observation)
		}
	})
	testingT.Run("観測値が無い deploy は、エラーにせず、無いことを返す", func(subTest *testing.T) {
		_, found, findError := observationRepository.FindDeployment(context.Background(), "deploy-00000002")
		if findError != nil || found {
			subTest.Errorf("無いだけのはず: %v %v", findError, found)
		}
	})
	testingT.Run("空の ID は拒否する(先頭の1件を返さない)", func(subTest *testing.T) {
		if _, _, findError := observationRepository.FindDeployment(context.Background(), ""); findError == nil {
			subTest.Errorf("拒否されるはず")
		}
	})
}

// TestDeployObservationRepositoryHeartbeat は、心拍が、書くまでは「無い」、書けば経過時間が小さく、
// 時間が経てば経過時間が大きくなり、繰り返し書いても1行のままであることを確認する。
func TestDeployObservationRepositoryHeartbeat(testingT *testing.T) {
	database, observationRepository := newObservationTestEnvironment(testingT)

	testingT.Run("一度も書いていなければ、無い", func(subTest *testing.T) {
		_, found, ageError := observationRepository.HeartbeatAge(context.Background())
		if ageError != nil || found {
			subTest.Errorf("無いはず: %v %v", ageError, found)
		}
	})
	testingT.Run("書いた直後は、経過時間が短い", func(subTest *testing.T) {
		if touchError := observationRepository.TouchHeartbeat(context.Background()); touchError != nil {
			subTest.Fatalf("書けるはず: %v", touchError)
		}
		age, found, ageError := observationRepository.HeartbeatAge(context.Background())
		if ageError != nil || !found || age < 0 || age > 5*time.Second {
			subTest.Errorf("経過時間が短いはず: %v %v %v", age, found, ageError)
		}
	})
	testingT.Run("時間が経つと、経過時間が大きくなり、しきい値を超える", func(subTest *testing.T) {
		database.Exec("UPDATE watcher_heartbeats SET beat_at = now() - interval '2 minutes'")
		age, found, _ := observationRepository.HeartbeatAge(context.Background())
		if !found || age < 119*time.Second || age < model.WatcherHeartbeatStaleAfter {
			subTest.Errorf("2分ほどで、しきい値を超えるはず: %v", age)
		}
	})
	testingT.Run("繰り返し書くと、経過時間が戻り、行は1つのまま", func(subTest *testing.T) {
		for attempt := 0; attempt < 3; attempt++ {
			if touchError := observationRepository.TouchHeartbeat(context.Background()); touchError != nil {
				subTest.Fatalf("書けるはず: %v", touchError)
			}
		}
		var rowCount int64
		database.Table("watcher_heartbeats").Count(&rowCount)
		age, _, _ := observationRepository.HeartbeatAge(context.Background())
		if rowCount != 1 || age > 5*time.Second {
			subTest.Errorf("1行で、経過時間が短いはず: %d %v", rowCount, age)
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

// TestDeployRepositoryCrashLoopStop は、止める準備が active の deploy だけを applying にしてイベントを記録し、
// 止め終えると applying の deploy だけを failed にし、replicas は変えないことを確認する。
func TestDeployRepositoryCrashLoopStop(testingT *testing.T) {
	testingT.Run("active の deploy は applying になり、理由のイベントが記録され、続けて failed にできる(replicas は変わらない)", func(subTest *testing.T) {
		database, deployRepository := newDeployTestEnvironment(subTest)
		database.Create(newTestDeploy(1, "project-00000001", "user-a"))
		database.Exec("UPDATE deploys SET status = 'active', replicas = 3 WHERE id = 'deploy-00000001'")

		began, beginError := deployRepository.BeginCrashLoopStop(context.Background(), "deploy-00000001", "uid-1", "pod-1", "CrashLoopBackOff", "再起動 9 回", time.Now())
		if beginError != nil || !began {
			subTest.Fatalf("始められるはず: %v %v", began, beginError)
		}
		var status string
		database.Table("deploys").Where("id = ?", "deploy-00000001").Pluck("status", &status)
		var stopEvents []model.DeployPodEvent
		database.Where("deploy_id = ? AND type = ?", "deploy-00000001", model.PodEventCrashLoopLimit).Find(&stopEvents)
		if status != "applying" || len(stopEvents) != 1 || stopEvents[0].Reason != "CrashLoopBackOff" || stopEvents[0].Message != "再起動 9 回" || stopEvents[0].PodName != "pod-1" {
			subTest.Errorf("applying で、理由のイベントが1件のはず: %q %+v", status, stopEvents)
		}
		// 2回目は、もう active ではないので、何もしない(イベントも増えない)。
		if again, _ := deployRepository.BeginCrashLoopStop(context.Background(), "deploy-00000001", "uid-1", "pod-1", "x", "x", time.Now()); again {
			subTest.Errorf("2回目は始められないはず")
		}
		// 止め終えると failed になり、replicas は 3 のまま。
		if finishError := deployRepository.FinishCrashLoopStop(context.Background(), "deploy-00000001"); finishError != nil {
			subTest.Fatalf("終えられるはず: %v", finishError)
		}
		var deploy model.Deploy
		database.Where("id = ?", "deploy-00000001").Take(&deploy)
		var eventCount int64
		database.Table("deploy_pod_events").Where("type = ?", model.PodEventCrashLoopLimit).Count(&eventCount)
		if deploy.Status != "failed" || deploy.Replicas != 3 || eventCount != 1 {
			subTest.Errorf("failed で replicas は 3 のままで、イベントは1件のはず: %q %d %d", deploy.Status, deploy.Replicas, eventCount)
		}
	})
	for _, status := range []string{"pending", "applying", "failed", "scaled_zero", "deleting", "delete_failed"} {
		testingT.Run(status+" の deploy には、何もしない", func(subTest *testing.T) {
			database, deployRepository := newDeployTestEnvironment(subTest)
			database.Create(newTestDeploy(1, "project-00000001", "user-a"))
			database.Exec("UPDATE deploys SET status = ? WHERE id = 'deploy-00000001'", status)
			began, beginError := deployRepository.BeginCrashLoopStop(context.Background(), "deploy-00000001", "uid-1", "pod-1", "x", "x", time.Now())
			var after string
			database.Table("deploys").Where("id = ?", "deploy-00000001").Pluck("status", &after)
			var eventCount int64
			database.Table("deploy_pod_events").Count(&eventCount)
			if beginError != nil || began || after != status || eventCount != 0 {
				subTest.Errorf("何も変わらないはず: %v %v %q %d", beginError, began, after, eventCount)
			}
		})
	}
	testingT.Run("無い deploy には何もしない。空の ID は拒否する", func(subTest *testing.T) {
		_, deployRepository := newDeployTestEnvironment(subTest)
		if began, beginError := deployRepository.BeginCrashLoopStop(context.Background(), "deploy-99999999", "uid", "pod", "x", "x", time.Now()); beginError != nil || began {
			subTest.Errorf("何もしないはず: %v %v", began, beginError)
		}
		if _, beginError := deployRepository.BeginCrashLoopStop(context.Background(), "", "uid", "pod", "x", "x", time.Now()); beginError == nil {
			subTest.Errorf("空の ID は拒否されるはず")
		}
		if finishError := deployRepository.FinishCrashLoopStop(context.Background(), ""); finishError == nil {
			subTest.Errorf("空の ID は拒否されるはず")
		}
	})
	testingT.Run("applying でない deploy は、failed にしない", func(subTest *testing.T) {
		database, deployRepository := newDeployTestEnvironment(subTest)
		database.Create(newTestDeploy(1, "project-00000001", "user-a"))
		database.Exec("UPDATE deploys SET status = 'active' WHERE id = 'deploy-00000001'")
		_ = deployRepository.FinishCrashLoopStop(context.Background(), "deploy-00000001")
		var after string
		database.Table("deploys").Where("id = ?", "deploy-00000001").Pluck("status", &after)
		if after != "active" {
			subTest.Errorf("active のままのはず: %q", after)
		}
	})
}
