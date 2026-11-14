package repository

import (
	"context"
	"launchs/shared/model"
	"testing"
	"time"
)

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
