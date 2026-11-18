package repository

import (
	"context"
	"launchs/shared/model"
	"testing"
	"time"

	"gorm.io/gorm"
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
