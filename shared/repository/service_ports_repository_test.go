package repository

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// currentPortsJSON と newPortsJSON は、テストで使う、現在のポートと、新しいポート。
const (
	currentPortsJSON = `[{"name":"web","port":8080,"protocol":"tcp"}]`
	newPortsJSON     = `[{"name":"web","port":8080,"protocol":"tcp"},{"name":"game","port":27015,"protocol":"udp"}]`
)

// newActiveServiceEnvironment は、deploy(active)と、active の service(ポートは currentPortsJSON)を用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - データベース。
//   - その上の ServiceRepository。
func newActiveServiceEnvironment(testingT *testing.T) (*gorm.DB, *ServiceRepository) {
	// deploy と service がある状態にして、service を active にする。
	database, serviceRepository := newServiceTestEnvironment(testingT)
	database.Exec("UPDATE deploys SET status = 'active' WHERE id = 'deploy-00000001'")
	_ = serviceRepository.Create(context.Background(), newTestService(1, "deploy-00000001", "user-a"))
	database.Exec("UPDATE services SET status = 'active'")
	return database, serviceRepository
}

// TestServiceRepositorySavePendingPorts は、ポートを pending として保存でき、現在と同じなら保存されず、
// 保存できない状態(deploy・service の状態、持ち主)では、何も変わらないことを確認する。
func TestServiceRepositorySavePendingPorts(testingT *testing.T) {
	ctx := context.Background()
	testingT.Run("pending に保存され、現在のポートは変わらない。もう一度保存すると、上書きされる", func(subTest *testing.T) {
		database, serviceRepository := newActiveServiceEnvironment(subTest)
		if saveError := serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(newPortsJSON)); saveError != nil {
			subTest.Fatalf("保存できるはず: %v", saveError)
		}
		var stored model.Service
		database.Where("id = 'service-00000001'").Take(&stored)
		if !sameJSON(stored.PendingPorts, []byte(newPortsJSON)) || !sameJSON(stored.Ports, []byte(currentPortsJSON)) {
			subTest.Fatalf("pending だけが入るはず: ports=%s pending=%s", stored.Ports, stored.PendingPorts)
		}
		other := `[{"name":"metrics","port":9090,"protocol":"tcp"}]`
		_ = serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(other))
		database.Where("id = 'service-00000001'").Take(&stored)
		if !sameJSON(stored.PendingPorts, []byte(other)) {
			subTest.Errorf("上書きされるはず: %s", stored.PendingPorts)
		}
	})
	testingT.Run("現在と同じ内容(書式が違うだけでも)なら、pending は保存されず、以前の pending も消える", func(subTest *testing.T) {
		database, serviceRepository := newActiveServiceEnvironment(subTest)
		_ = serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(newPortsJSON))
		spaced := `[ { "name" : "web", "port" : 8080, "protocol" : "tcp" } ]`
		if saveError := serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(spaced)); saveError != nil {
			subTest.Fatalf("成功するはず: %v", saveError)
		}
		var stored model.Service
		database.Where("id = 'service-00000001'").Take(&stored)
		if len(stored.PendingPorts) != 0 {
			subTest.Errorf("pending は NULL のはず: %s", stored.PendingPorts)
		}
	})
	for _, allowed := range []string{"pending", "active", "failed", "scaled_zero"} {
		testingT.Run("deploy が "+allowed+" でも保存できる(DB の保存だけで、deploy の実行には関わらないため)", func(subTest *testing.T) {
			database, serviceRepository := newActiveServiceEnvironment(subTest)
			database.Exec("UPDATE deploys SET status = ?", allowed)
			if saveError := serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(newPortsJSON)); saveError != nil {
				subTest.Errorf("保存できるはず: %v", saveError)
			}
		})
	}
	for _, blocked := range []string{"applying", "deleting", "delete_failed"} {
		testingT.Run("deploy が "+blocked+" なら conflict で、保存されない", func(subTest *testing.T) {
			database, serviceRepository := newActiveServiceEnvironment(subTest)
			database.Exec("UPDATE deploys SET status = ?", blocked)
			saveError := serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(newPortsJSON))
			var stored model.Service
			database.Where("id = 'service-00000001'").Take(&stored)
			if apperror.KindOf(saveError) != apperror.KindConflict || len(stored.PendingPorts) != 0 {
				subTest.Errorf("conflict で、保存されないはず: %v %s", saveError, stored.PendingPorts)
			}
		})
	}
	for _, blocked := range []string{"pending", "failed", "deleting"} {
		testingT.Run("service が "+blocked+" なら conflict", func(subTest *testing.T) {
			database, serviceRepository := newActiveServiceEnvironment(subTest)
			database.Exec("UPDATE services SET status = ?", blocked)
			if saveError := serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(newPortsJSON)); apperror.KindOf(saveError) != apperror.KindConflict {
				subTest.Errorf("conflict のはず: %v", saveError)
			}
		})
	}
	testingT.Run("他のユーザーの deploy・無い deploy・service が無い deploy は not_found。空の ID は拒否する", func(subTest *testing.T) {
		database, serviceRepository := newActiveServiceEnvironment(subTest)
		database.Create(newTestDeploy(2, "project-00000001", "user-a"))
		for _, testCase := range []struct{ user, deploy string }{{"user-b", "deploy-00000001"}, {"user-a", "deploy-99999999"}, {"user-a", "deploy-00000002"}, {"", "deploy-00000001"}, {"user-a", ""}} {
			if saveError := serviceRepository.SavePendingPorts(ctx, testCase.user, testCase.deploy, []byte(newPortsJSON)); apperror.KindOf(saveError) != apperror.KindNotFound {
				subTest.Errorf("%+v: not_found のはず: %v", testCase, saveError)
			}
		}
	})
}

// TestServiceRepositoryDiscardPendingPorts は、pending のポートを破棄でき、無いとき・保存できない状態では、conflict などになることを確認する。
func TestServiceRepositoryDiscardPendingPorts(testingT *testing.T) {
	ctx := context.Background()
	testingT.Run("破棄すると pending が NULL に戻り、現在のポートは変わらない。もう一度は conflict", func(subTest *testing.T) {
		database, serviceRepository := newActiveServiceEnvironment(subTest)
		_ = serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(newPortsJSON))
		if discardError := serviceRepository.DiscardPendingPorts(ctx, "user-a", "deploy-00000001"); discardError != nil {
			subTest.Fatalf("破棄できるはず: %v", discardError)
		}
		var stored model.Service
		database.Where("id = 'service-00000001'").Take(&stored)
		if len(stored.PendingPorts) != 0 || !sameJSON(stored.Ports, []byte(currentPortsJSON)) {
			subTest.Errorf("pending は NULL で、ports は変わらないはず: %s %s", stored.Ports, stored.PendingPorts)
		}
		if secondError := serviceRepository.DiscardPendingPorts(ctx, "user-a", "deploy-00000001"); apperror.KindOf(secondError) != apperror.KindConflict {
			subTest.Errorf("2回目は conflict のはず: %v", secondError)
		}
	})
	for _, blocked := range []string{"applying", "deleting", "delete_failed"} {
		testingT.Run("deploy が "+blocked+" なら conflict で、破棄されない", func(subTest *testing.T) {
			database, serviceRepository := newActiveServiceEnvironment(subTest)
			_ = serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(newPortsJSON))
			database.Exec("UPDATE deploys SET status = ?", blocked)
			discardError := serviceRepository.DiscardPendingPorts(ctx, "user-a", "deploy-00000001")
			var stored model.Service
			database.Where("id = 'service-00000001'").Take(&stored)
			if apperror.KindOf(discardError) != apperror.KindConflict || len(stored.PendingPorts) == 0 {
				subTest.Errorf("conflict で、pending は残るはず: %v %s", discardError, stored.PendingPorts)
			}
		})
	}
	testingT.Run("他人・無い・service が無い deploy は not_found", func(subTest *testing.T) {
		_, serviceRepository := newActiveServiceEnvironment(subTest)
		if discardError := serviceRepository.DiscardPendingPorts(ctx, "user-b", "deploy-00000001"); apperror.KindOf(discardError) != apperror.KindNotFound {
			subTest.Errorf("他人は not_found のはず: %v", discardError)
		}
		if discardError := serviceRepository.DiscardPendingPorts(ctx, "", ""); apperror.KindOf(discardError) != apperror.KindNotFound {
			subTest.Errorf("空は not_found のはず: %v", discardError)
		}
	})
}

// TestServiceRepositoryBeginPortsApply は、反映の開始で deploy が applying になること、開始できない条件、巻き戻しを確認する。
func TestServiceRepositoryBeginPortsApply(testingT *testing.T) {
	ctx := context.Background()
	for _, allowed := range []string{"active", "failed", "scaled_zero"} {
		testingT.Run("deploy が "+allowed+" なら開始でき、applying になり、元の状態が返り、巻き戻せる", func(subTest *testing.T) {
			database, serviceRepository := newActiveServiceEnvironment(subTest)
			database.Exec("UPDATE deploys SET status = ?", allowed)
			_ = serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(newPortsJSON))

			serviceID, previousStatus, beginError := serviceRepository.BeginPortsApply(ctx, "user-a", "deploy-00000001")

			var status string
			database.Table("deploys").Where("id = 'deploy-00000001'").Pluck("status", &status)
			if beginError != nil || serviceID != "service-00000001" || previousStatus != allowed || status != "applying" {
				subTest.Fatalf("applying になり、ID と元の状態が返るはず: %v %q %q %q", beginError, serviceID, previousStatus, status)
			}
			var stored model.Service
			database.Where("id = 'service-00000001'").Take(&stored)
			if !sameJSON(stored.PendingPorts, []byte(newPortsJSON)) || stored.Status != "active" {
				subTest.Errorf("pending は保持され、service の status は変わらないはず: %s %s", stored.PendingPorts, stored.Status)
			}
			if revertError := serviceRepository.RevertPortsApply(ctx, "deploy-00000001", previousStatus); revertError != nil {
				subTest.Fatalf("戻せるはず: %v", revertError)
			}
			database.Table("deploys").Where("id = 'deploy-00000001'").Pluck("status", &status)
			if status != allowed {
				subTest.Errorf("元の状態に戻るはず: %q", status)
			}
		})
	}
	for _, blocked := range []string{"pending", "applying", "deleting", "delete_failed"} {
		testingT.Run("deploy が "+blocked+" なら conflict で、状態は変わらない", func(subTest *testing.T) {
			database, serviceRepository := newActiveServiceEnvironment(subTest)
			_ = serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(newPortsJSON))
			database.Exec("UPDATE deploys SET status = ?", blocked)
			_, _, beginError := serviceRepository.BeginPortsApply(ctx, "user-a", "deploy-00000001")
			var status string
			database.Table("deploys").Where("id = 'deploy-00000001'").Pluck("status", &status)
			if apperror.KindOf(beginError) != apperror.KindConflict || status != blocked {
				subTest.Errorf("conflict で、状態は変わらないはず: %v %q", beginError, status)
			}
		})
	}
	testingT.Run("適用するポートが無ければ conflict(状態は変わらない)", func(subTest *testing.T) {
		database, serviceRepository := newActiveServiceEnvironment(subTest)
		_, _, beginError := serviceRepository.BeginPortsApply(ctx, "user-a", "deploy-00000001")
		var status string
		database.Table("deploys").Where("id = 'deploy-00000001'").Pluck("status", &status)
		if apperror.KindOf(beginError) != apperror.KindConflict || status != "active" {
			subTest.Errorf("conflict のはず: %v %q", beginError, status)
		}
	})
	for _, blocked := range []string{"pending", "failed", "deleting"} {
		testingT.Run("service が "+blocked+" なら conflict", func(subTest *testing.T) {
			database, serviceRepository := newActiveServiceEnvironment(subTest)
			database.Exec("UPDATE services SET pending_ports = ?::jsonb, status = ?", newPortsJSON, blocked)
			if _, _, beginError := serviceRepository.BeginPortsApply(ctx, "user-a", "deploy-00000001"); apperror.KindOf(beginError) != apperror.KindConflict {
				subTest.Errorf("conflict のはず: %v", beginError)
			}
		})
	}
	testingT.Run("他人・無い・service が無い deploy は not_found。巻き戻しは、空の条件を拒否する", func(subTest *testing.T) {
		_, serviceRepository := newActiveServiceEnvironment(subTest)
		if _, _, beginError := serviceRepository.BeginPortsApply(ctx, "user-b", "deploy-00000001"); apperror.KindOf(beginError) != apperror.KindNotFound {
			subTest.Errorf("他人は not_found のはず: %v", beginError)
		}
		if revertError := serviceRepository.RevertPortsApply(ctx, "", "active"); revertError == nil {
			subTest.Errorf("空の ID は拒否されるはず")
		}
	})
}

// TestServiceRepositoryCommitPortsApply は、成功ならポートが pending の内容になって deploy が戻り、
// 失敗ならポートも pending も変わらず deploy が failed になり、applying でなければ何も変えないことを確認する。
func TestServiceRepositoryCommitPortsApply(testingT *testing.T) {
	ctx := context.Background()
	// 開始まで済ませた状態(deploy は applying、pending のポートあり)を作る。
	prepare := func(subTest *testing.T, replicas int) (*gorm.DB, *ServiceRepository) {
		database, serviceRepository := newActiveServiceEnvironment(subTest)
		database.Exec("UPDATE deploys SET replicas = ?", replicas)
		_ = serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(newPortsJSON))
		if _, _, beginError := serviceRepository.BeginPortsApply(ctx, "user-a", "deploy-00000001"); beginError != nil {
			subTest.Fatalf("開始できるはず: %v", beginError)
		}
		return database, serviceRepository
	}
	readState := func(database *gorm.DB) (model.Service, string) {
		var stored model.Service
		database.Where("id = 'service-00000001'").Take(&stored)
		var status string
		database.Table("deploys").Where("id = 'deploy-00000001'").Pluck("status", &status)
		return stored, status
	}

	for _, testCase := range []struct {
		name           string
		replicas       int
		expectedStatus string
	}{{"成功なら、ポートが pending の内容になり、pending は NULL で、deploy は active", 1, "active"}, {"成功で replicas が 0 なら、deploy は scaled_zero", 0, "scaled_zero"}} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			database, serviceRepository := prepare(subTest, testCase.replicas)
			committed, commitError := serviceRepository.CommitPortsApply(ctx, "service-00000001", true)
			stored, status := readState(database)
			if commitError != nil || !committed || !sameJSON(stored.Ports, []byte(newPortsJSON)) || len(stored.PendingPorts) != 0 || status != testCase.expectedStatus || stored.Status != "active" {
				subTest.Errorf("ports が新しく、pending は NULL、deploy は %s、service は active のはず: %v %v ports=%s pending=%s %s %s",
					testCase.expectedStatus, committed, commitError, stored.Ports, stored.PendingPorts, status, stored.Status)
			}
		})
	}
	testingT.Run("失敗なら、ポートも pending も変わらず、deploy は failed", func(subTest *testing.T) {
		database, serviceRepository := prepare(subTest, 1)
		committed, commitError := serviceRepository.CommitPortsApply(ctx, "service-00000001", false)
		stored, status := readState(database)
		if commitError != nil || !committed || !sameJSON(stored.Ports, []byte(currentPortsJSON)) || !sameJSON(stored.PendingPorts, []byte(newPortsJSON)) || status != "failed" {
			subTest.Errorf("ports と pending は変わらず、deploy は failed のはず: %v %v ports=%s pending=%s %s", committed, commitError, stored.Ports, stored.PendingPorts, status)
		}
	})
	testingT.Run("もう一度確定しても、何も変えない(冪等)", func(subTest *testing.T) {
		database, serviceRepository := prepare(subTest, 1)
		_, _ = serviceRepository.CommitPortsApply(ctx, "service-00000001", true)
		if again, _ := serviceRepository.CommitPortsApply(ctx, "service-00000001", false); again {
			subTest.Errorf("applying でないので、確定されないはず")
		}
		if _, status := readState(database); status != "active" {
			subTest.Errorf("active のままのはず: %q", status)
		}
	})
	testingT.Run("service が無い(削除された)ときは、何もせず、エラーにもしない。空の ID は拒否する", func(subTest *testing.T) {
		_, serviceRepository := newActiveServiceEnvironment(subTest)
		if committed, commitError := serviceRepository.CommitPortsApply(ctx, "service-99999999", true); commitError != nil || committed {
			subTest.Errorf("何もしないはず: %v %v", committed, commitError)
		}
		if _, commitError := serviceRepository.CommitPortsApply(ctx, "", true); commitError == nil {
			subTest.Errorf("空の ID は拒否されるはず")
		}
	})
	testingT.Run("deploy が applying でないときは、service の pending も変えない", func(subTest *testing.T) {
		database, serviceRepository := newActiveServiceEnvironment(subTest)
		_ = serviceRepository.SavePendingPorts(ctx, "user-a", "deploy-00000001", []byte(newPortsJSON))
		committed, _ := serviceRepository.CommitPortsApply(ctx, "service-00000001", true)
		stored, _ := readState(database)
		if committed || len(stored.PendingPorts) == 0 {
			subTest.Errorf("何も変わらないはず: %v %s", committed, stored.PendingPorts)
		}
	})
}
