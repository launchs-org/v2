package repository

import (
	"context"
	"fmt"
	"testing"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// newTestService は、テスト用の service を作る。
//
// 引数:
//   - number: ID を区別するための番号。
//   - deployID: 属する deploy の ID。
//   - userID: 持ち主のユーザー ID。
//
// 戻り値:
//   - ポートが1つ(web、8080、tcp)の service。
func newTestService(number int, deployID string, userID string) *model.Service {
	return &model.Service{
		ID: fmt.Sprintf("service-%08d", number), DeployID: deployID, UserID: userID,
		Ports: datatypes.JSON(`[{"name":"web","port":8080,"protocol":"tcp"}]`),
	}
}

// newServiceTestEnvironment は、使い捨てのデータベースと、持ち主 user-a の deploy(deploy-00000001)を用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - データベース。
//   - その上の ServiceRepository。
func newServiceTestEnvironment(testingT *testing.T) (*gorm.DB, *ServiceRepository) {
	// deploy が1件ある状態にする。
	database, _ := newDeployTestEnvironment(testingT)
	database.Create(newTestDeploy(1, "project-00000001", "user-a"))
	return database, NewServiceRepository(database)
}

// TestServiceRepositoryCreate は、service を作れる条件(持ち主・deploy の状態・1 deploy に1つ)を確認する。
func TestServiceRepositoryCreate(testingT *testing.T) {
	testingT.Run("作れる。status は pending で、プロジェクトは deploy から決まり、DNS 名と ClusterIP は NULL", func(subTest *testing.T) {
		database, serviceRepository := newServiceTestEnvironment(subTest)
		service := newTestService(1, "deploy-00000001", "user-a")
		if createError := serviceRepository.Create(context.Background(), service); createError != nil {
			subTest.Fatalf("作れるはず: %v", createError)
		}
		var stored model.Service
		database.Where("id = ?", "service-00000001").Take(&stored)
		if stored.Status != "pending" || stored.ProjectID != "project-00000001" || stored.DNSName != nil || stored.ClusterIP != nil || stored.PendingPorts != nil {
			subTest.Errorf("pending で、プロジェクトが入り、他は NULL のはず: %+v", stored)
		}
	})
	for _, allowed := range []string{"pending", "active", "applying", "failed", "scaled_zero"} {
		testingT.Run("deploy が "+allowed+" でも作れる", func(subTest *testing.T) {
			database, serviceRepository := newServiceTestEnvironment(subTest)
			database.Exec("UPDATE deploys SET status = ? WHERE id = 'deploy-00000001'", allowed)
			if createError := serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a")); createError != nil {
				subTest.Errorf("作れるはず: %v", createError)
			}
		})
	}
	for _, blocked := range []string{"deleting", "delete_failed"} {
		testingT.Run("deploy が "+blocked+" なら conflict で、保存されない", func(subTest *testing.T) {
			database, serviceRepository := newServiceTestEnvironment(subTest)
			database.Exec("UPDATE deploys SET status = ? WHERE id = 'deploy-00000001'", blocked)
			createError := serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a"))
			var count int64
			database.Table("services").Count(&count)
			if apperror.KindOf(createError) != apperror.KindConflict || count != 0 {
				subTest.Errorf("conflict で、保存されないはず: %v %d", createError, count)
			}
		})
	}
	testingT.Run("すでに service があれば conflict(1 deploy = 1 service)", func(subTest *testing.T) {
		_, serviceRepository := newServiceTestEnvironment(subTest)
		_ = serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a"))
		if createError := serviceRepository.Create(context.Background(), newTestService(2, "deploy-00000001", "user-a")); apperror.KindOf(createError) != apperror.KindConflict {
			subTest.Errorf("conflict のはず: %v", createError)
		}
	})
	testingT.Run("無い deploy と、他のユーザーの deploy は not_found", func(subTest *testing.T) {
		_, serviceRepository := newServiceTestEnvironment(subTest)
		if createError := serviceRepository.Create(context.Background(), newTestService(1, "deploy-99999999", "user-a")); apperror.KindOf(createError) != apperror.KindNotFound {
			subTest.Errorf("無い deploy は not_found のはず: %v", createError)
		}
		if createError := serviceRepository.Create(context.Background(), newTestService(2, "deploy-00000001", "user-b")); apperror.KindOf(createError) != apperror.KindNotFound {
			subTest.Errorf("他人の deploy は not_found のはず: %v", createError)
		}
	})
}

// TestServiceRepositoryFind は、deploy の service を取得でき(deploy の状態も返る)、無いものと他人のものは not_found になり、
// ワーカー用の取得が、service の有無を返すことを確認する。
func TestServiceRepositoryFind(testingT *testing.T) {
	database, serviceRepository := newServiceTestEnvironment(testingT)
	database.Exec("UPDATE deploys SET status = 'active' WHERE id = 'deploy-00000001'")
	database.Create(newTestDeploy(2, "project-00000001", "user-a"))
	_ = serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a"))

	testingT.Run("service と、deploy の状態を返す", func(subTest *testing.T) {
		service, deployStatus, findError := serviceRepository.FindByDeployID(context.Background(), "user-a", "deploy-00000001")
		if findError != nil || service.ID != "service-00000001" || deployStatus != "active" {
			subTest.Errorf("service と active のはず: %v %+v %q", findError, service, deployStatus)
		}
	})
	testingT.Run("service が無い deploy は not_found", func(subTest *testing.T) {
		if _, _, findError := serviceRepository.FindByDeployID(context.Background(), "user-a", "deploy-00000002"); apperror.KindOf(findError) != apperror.KindNotFound {
			subTest.Errorf("not_found のはず: %v", findError)
		}
	})
	testingT.Run("他のユーザーの deploy と、無い deploy は not_found", func(subTest *testing.T) {
		if _, _, findError := serviceRepository.FindByDeployID(context.Background(), "user-b", "deploy-00000001"); apperror.KindOf(findError) != apperror.KindNotFound {
			subTest.Errorf("他人は not_found のはず: %v", findError)
		}
		if _, _, findError := serviceRepository.FindByDeployID(context.Background(), "user-a", "deploy-99999999"); apperror.KindOf(findError) != apperror.KindNotFound {
			subTest.Errorf("無い deploy は not_found のはず: %v", findError)
		}
	})
	testingT.Run("ワーカー用: ID で取得でき、deploy の ID では、有無を返す", func(subTest *testing.T) {
		if service, findError := serviceRepository.FindForWorker(context.Background(), "service-00000001"); findError != nil || service.DeployID != "deploy-00000001" {
			subTest.Errorf("取得できるはず: %v %+v", findError, service)
		}
		if _, findError := serviceRepository.FindForWorker(context.Background(), "service-99999999"); apperror.KindOf(findError) != apperror.KindNotFound {
			subTest.Errorf("無ければ not_found のはず: %v", findError)
		}
		if _, found, findError := serviceRepository.FindByDeployIDForWorker(context.Background(), "deploy-00000001"); findError != nil || !found {
			subTest.Errorf("あるはず: %v %v", found, findError)
		}
		if _, found, findError := serviceRepository.FindByDeployIDForWorker(context.Background(), "deploy-00000002"); findError != nil || found {
			subTest.Errorf("無いだけで、エラーにしないはず: %v %v", found, findError)
		}
	})
}

// TestServiceRepositoryMarkActive は、pending の service だけを active にして、DNS 名と ClusterIP を記録することを確認する。
func TestServiceRepositoryMarkActive(testingT *testing.T) {
	testingT.Run("pending なら active になり、DNS 名と ClusterIP が入る。2回目は何もしない", func(subTest *testing.T) {
		database, serviceRepository := newServiceTestEnvironment(subTest)
		_ = serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a"))
		changed, markError := serviceRepository.MarkActive(context.Background(), "service-00000001", "service-00000001.project-00000001.svc.test.local", "10.112.0.5")
		var stored model.Service
		database.Where("id = ?", "service-00000001").Take(&stored)
		if markError != nil || !changed || stored.Status != "active" || stored.DNSName == nil || *stored.DNSName != "service-00000001.project-00000001.svc.test.local" || stored.ClusterIP == nil || *stored.ClusterIP != "10.112.0.5" {
			subTest.Fatalf("active で、値が入るはず: %v %v %+v", changed, markError, stored)
		}
		if again, _ := serviceRepository.MarkActive(context.Background(), "service-00000001", "other", "10.0.0.1"); again {
			subTest.Errorf("2回目は何もしないはず")
		}
	})
	testingT.Run("ClusterIP が空(None など)なら、NULL のまま", func(subTest *testing.T) {
		database, serviceRepository := newServiceTestEnvironment(subTest)
		_ = serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a"))
		_, _ = serviceRepository.MarkActive(context.Background(), "service-00000001", "dns", "")
		var stored model.Service
		database.Where("id = ?", "service-00000001").Take(&stored)
		if stored.Status != "active" || stored.ClusterIP != nil {
			subTest.Errorf("active で、ClusterIP は NULL のはず: %+v", stored)
		}
	})
	testingT.Run("pending でなければ(削除中など)、変えない", func(subTest *testing.T) {
		database, serviceRepository := newServiceTestEnvironment(subTest)
		_ = serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a"))
		database.Exec("UPDATE services SET status = 'deleting'")
		if changed, _ := serviceRepository.MarkActive(context.Background(), "service-00000001", "dns", "10.0.0.1"); changed {
			subTest.Errorf("変えないはず")
		}
	})
}

// TestServiceRepositoryDeletion は、削除の受け付け(状態ごとの可否)、巻き戻し、失敗の記録、
// 物理削除(削除中のものだけ)、作成の取り消し(pending のものだけ)、deploy を消したときの連鎖を確認する。
func TestServiceRepositoryDeletion(testingT *testing.T) {
	acceptCases := []struct {
		serviceStatus string
		deployStatus  string
		expectedKind  apperror.Kind
	}{
		{serviceStatus: "active", deployStatus: "active"},
		{serviceStatus: "failed", deployStatus: "active"},
		{serviceStatus: "active", deployStatus: "failed"},
		{serviceStatus: "pending", deployStatus: "active", expectedKind: apperror.KindConflict},
		{serviceStatus: "deleting", deployStatus: "active", expectedKind: apperror.KindConflict},
		{serviceStatus: "active", deployStatus: "deleting", expectedKind: apperror.KindConflict},
		{serviceStatus: "active", deployStatus: "delete_failed", expectedKind: apperror.KindConflict},
	}
	for _, testCase := range acceptCases {
		testingT.Run(fmt.Sprintf("受け付け: service %s、deploy %s", testCase.serviceStatus, testCase.deployStatus), func(subTest *testing.T) {
			database, serviceRepository := newServiceTestEnvironment(subTest)
			_ = serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a"))
			database.Exec("UPDATE services SET status = ?", testCase.serviceStatus)
			database.Exec("UPDATE deploys SET status = ? WHERE id = 'deploy-00000001'", testCase.deployStatus)

			serviceID, previousStatus, markError := serviceRepository.MarkDeleting(context.Background(), "user-a", "deploy-00000001")

			var status string
			database.Table("services").Pluck("status", &status)
			if apperror.KindOf(markError) != testCase.expectedKind {
				subTest.Fatalf("エラーの種類が違う: %v", markError)
			}
			if testCase.expectedKind != "" {
				if status != testCase.serviceStatus {
					subTest.Errorf("状態は変わらないはず: %q", status)
				}
				return
			}
			if serviceID != "service-00000001" || previousStatus != testCase.serviceStatus || status != "deleting" {
				subTest.Errorf("deleting になり、ID と元の状態が返るはず: %q %q %q", serviceID, previousStatus, status)
			}
			if revertError := serviceRepository.RevertDeleting(context.Background(), serviceID, previousStatus); revertError != nil {
				subTest.Fatalf("戻せるはず: %v", revertError)
			}
			database.Table("services").Pluck("status", &status)
			if status != testCase.serviceStatus {
				subTest.Errorf("元の状態に戻るはず: %q", status)
			}
		})
	}
	testingT.Run("他人の deploy と、service が無い deploy は not_found", func(subTest *testing.T) {
		_, serviceRepository := newServiceTestEnvironment(subTest)
		if _, _, markError := serviceRepository.MarkDeleting(context.Background(), "user-b", "deploy-00000001"); apperror.KindOf(markError) != apperror.KindNotFound {
			subTest.Errorf("他人は not_found のはず: %v", markError)
		}
		if _, _, markError := serviceRepository.MarkDeleting(context.Background(), "user-a", "deploy-00000001"); apperror.KindOf(markError) != apperror.KindNotFound {
			subTest.Errorf("service が無ければ not_found のはず: %v", markError)
		}
	})
	testingT.Run("失敗の記録・物理削除は、削除中のものだけが対象で、繰り返しても壊れない", func(subTest *testing.T) {
		database, serviceRepository := newServiceTestEnvironment(subTest)
		_ = serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a"))
		database.Exec("UPDATE services SET status = 'active'")
		// active には、効かない。
		_ = serviceRepository.MarkDeleteFailed(context.Background(), "service-00000001")
		_ = serviceRepository.DeleteRecord(context.Background(), "service-00000001")
		var count int64
		database.Table("services").Where("status = 'active'").Count(&count)
		if count != 1 {
			subTest.Fatalf("active のものは変わらず残るはず: %d", count)
		}
		// 削除中のものは、failed になり、その状態では消えない。
		database.Exec("UPDATE services SET status = 'deleting'")
		if markError := serviceRepository.MarkDeleteFailed(context.Background(), "service-00000001"); markError != nil {
			subTest.Fatalf("記録できるはず: %v", markError)
		}
		_ = serviceRepository.DeleteRecord(context.Background(), "service-00000001")
		database.Table("services").Where("status = 'failed'").Count(&count)
		if count != 1 {
			subTest.Fatalf("failed になり、消えないはず: %d", count)
		}
		// 削除中に戻して消すと、消える。繰り返しても壊れない。
		database.Exec("UPDATE services SET status = 'deleting'")
		for attempt := 0; attempt < 2; attempt++ {
			if deleteError := serviceRepository.DeleteRecord(context.Background(), "service-00000001"); deleteError != nil {
				subTest.Fatalf("消せるはず(%d回目): %v", attempt+1, deleteError)
			}
		}
		database.Table("services").Count(&count)
		if count != 0 {
			subTest.Errorf("消えているはず: %d", count)
		}
	})
	testingT.Run("作成の取り消しは、pending のものだけを消す", func(subTest *testing.T) {
		database, serviceRepository := newServiceTestEnvironment(subTest)
		_ = serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a"))
		database.Exec("UPDATE services SET status = 'active'")
		_ = serviceRepository.DeleteUnstarted(context.Background(), "service-00000001")
		var count int64
		database.Table("services").Count(&count)
		if count != 1 {
			subTest.Fatalf("active は消えないはず: %d", count)
		}
		database.Exec("UPDATE services SET status = 'pending'")
		_ = serviceRepository.DeleteUnstarted(context.Background(), "service-00000001")
		database.Table("services").Count(&count)
		if count != 0 {
			subTest.Errorf("pending は消えるはず: %d", count)
		}
	})
	testingT.Run("deploy の行を消すと、service も一緒に消える(CASCADE)", func(subTest *testing.T) {
		database, serviceRepository := newServiceTestEnvironment(subTest)
		_ = serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a"))
		if deleteError := database.Exec("DELETE FROM deploys WHERE id = 'deploy-00000001'").Error; deleteError != nil {
			subTest.Fatalf("消せるはず: %v", deleteError)
		}
		var count int64
		database.Table("services").Count(&count)
		if count != 0 {
			subTest.Errorf("service も消えるはず: %d", count)
		}
	})
}
