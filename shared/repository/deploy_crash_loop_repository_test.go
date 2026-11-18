package repository

import (
	"context"
	"launchs/shared/model"
	"testing"
	"time"
)

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
