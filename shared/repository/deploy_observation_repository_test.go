package repository

import (
	"context"
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
