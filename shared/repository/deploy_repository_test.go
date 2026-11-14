package repository

import (
	"context"
	"fmt"
	"launchs/shared/apperror"
	"launchs/shared/database/testdatabase"
	"launchs/shared/model"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// newTestDeploy は、テスト用の deploy を作る。
//
// 引数:
//   - number: ID を区別するための番号。
//   - projectID: 属するプロジェクトの ID。
//   - userID: 持ち主のユーザー ID。
//
// 戻り値:
//   - pending 状態の deploy。
func newTestDeploy(number int, projectID string, userID string) *model.Deploy {
	return &model.Deploy{
		ID:        fmt.Sprintf("deploy-%08d", number),
		ProjectID: projectID,
		UserID:    userID,
		ImageRef:  "registry.example/app:1",
		Spec:      "small",
		Replicas:  1,
		Status:    model.DeployStatusPending,
		Env:       datatypes.JSON(`{}`),
	}
}

// newDeployTestEnvironment は、使い捨てのデータベースと、持ち主 user-a の active なプロジェクトを用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - データベース。
//   - その上の DeployRepository。
func newDeployTestEnvironment(testingT *testing.T) (*gorm.DB, *DeployRepository) {
	// 使い捨てのデータベースに、プロジェクトを1件作る。
	database := testdatabase.New(testingT)
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))
	return database, NewDeployRepository(database)
}

// TestDeployRepositoryFindSpec は、マスターデータのスペックを取得でき、無い名前は invalid_request になることを確認する。
func TestDeployRepositoryFindSpec(testingT *testing.T) {
	// マイグレーションで入るマスターデータを使う。
	_, deployRepository := newDeployTestEnvironment(testingT)

	// 3つのスペックの大きさ(仮の初期値)と、無い名前の場合。
	testCases := []struct {
		name           string
		specName       string
		expectedCPU    int
		expectedMemory int
		expectedKind   apperror.Kind
	}{
		{name: "small は 0.25 CPU / 256MiB", specName: "small", expectedCPU: 250, expectedMemory: 256},
		{name: "medium は 0.5 CPU / 512MiB", specName: "medium", expectedCPU: 500, expectedMemory: 512},
		{name: "large は 1 CPU / 1GiB", specName: "large", expectedCPU: 1000, expectedMemory: 1024},
		{name: "無い名前は invalid_request", specName: "huge", expectedKind: apperror.KindInvalidRequest},
		{name: "空の名前は invalid_request", specName: "", expectedKind: apperror.KindInvalidRequest},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			spec, findError := deployRepository.FindSpec(context.Background(), testCase.specName)
			if apperror.KindOf(findError) != testCase.expectedKind {
				subTest.Fatalf("エラーの種類が違う: 期待 %q、実際 %v", testCase.expectedKind, findError)
			}
			if findError == nil && (spec.CPUMillicores != testCase.expectedCPU || spec.MemoryMiB != testCase.expectedMemory) {
				subTest.Errorf("大きさが違う: %+v", spec)
			}
		})
	}
}

// TestDeployRepositoryCreate は、プロジェクトが active のときだけ作れ、無い・他人のものは not_found、
// 削除中などは conflict になることを確認する。
func TestDeployRepositoryCreate(testingT *testing.T) {
	// user-a の active なプロジェクトに加えて、削除中と、delete_failed のプロジェクトも作る。
	database, deployRepository := newDeployTestEnvironment(testingT)
	deletingProject := newTestProject(2, "user-a", time.Now())
	deletingProject.Status = model.ProjectStatusDeleting
	mustCreate(testingT, database, deletingProject)
	failedProject := newTestProject(3, "user-a", time.Now())
	failedProject.Status = model.ProjectStatusDeleteFailed
	mustCreate(testingT, database, failedProject)

	// プロジェクトと持ち主の組み合わせごとの期待。
	testCases := []struct {
		name         string
		projectID    string
		userID       string
		expectedKind apperror.Kind
	}{
		{name: "active なプロジェクトには作れる", projectID: "project-00000001", userID: "user-a"},
		{name: "他のユーザーのプロジェクトは not_found", projectID: "project-00000001", userID: "user-b", expectedKind: apperror.KindNotFound},
		{name: "存在しないプロジェクトは not_found", projectID: "project-99999999", userID: "user-a", expectedKind: apperror.KindNotFound},
		{name: "削除中のプロジェクトは conflict", projectID: "project-00000002", userID: "user-a", expectedKind: apperror.KindConflict},
		{name: "delete_failed のプロジェクトも conflict", projectID: "project-00000003", userID: "user-a", expectedKind: apperror.KindConflict},
	}
	for index, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			createError := deployRepository.Create(context.Background(), newTestDeploy(index+1, testCase.projectID, testCase.userID))
			if apperror.KindOf(createError) != testCase.expectedKind {
				subTest.Errorf("エラーの種類が違う: 期待 %q、実際 %v", testCase.expectedKind, createError)
			}
		})
	}
}

// TestDeployRepositoryFindByID は、持ち主は取得でき、他のユーザーと存在しない ID は not_found になることを確認する。
func TestDeployRepositoryFindByID(testingT *testing.T) {
	// deploy を1件作る。
	database, deployRepository := newDeployTestEnvironment(testingT)
	if createError := deployRepository.Create(context.Background(), newTestDeploy(1, "project-00000001", "user-a")); createError != nil {
		testingT.Fatalf("作成できませんでした: %v", createError)
	}
	_ = database

	// 持ち主・他のユーザー・存在しない ID の場合。
	testCases := []struct {
		name         string
		userID       string
		deployID     string
		expectedKind apperror.Kind
	}{
		{name: "持ち主は取得できる", userID: "user-a", deployID: "deploy-00000001"},
		{name: "他のユーザーは not_found", userID: "user-b", deployID: "deploy-00000001", expectedKind: apperror.KindNotFound},
		{name: "存在しない ID は not_found", userID: "user-a", deployID: "deploy-99999999", expectedKind: apperror.KindNotFound},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			deploy, findError := deployRepository.FindByID(context.Background(), testCase.userID, testCase.deployID)
			if apperror.KindOf(findError) != testCase.expectedKind {
				subTest.Fatalf("エラーの種類が違う: 期待 %q、実際 %v", testCase.expectedKind, findError)
			}
			if findError == nil && (deploy.ImageRef != "registry.example/app:1" || deploy.Status != model.DeployStatusPending || deploy.Replicas != 1) {
				subTest.Errorf("内容が違う: %+v", deploy)
			}
		})
	}
}

// TestDeployRepositoryUpdateStatus は、期待する状態のときだけ状態が変わり、
// 別の処理が先に変えていたときは、何も変えずに false を返すことを確認する。
func TestDeployRepositoryUpdateStatus(testingT *testing.T) {
	// pending の deploy を1件作る。
	_, deployRepository := newDeployTestEnvironment(testingT)
	if createError := deployRepository.Create(context.Background(), newTestDeploy(1, "project-00000001", "user-a")); createError != nil {
		testingT.Fatalf("作成できませんでした: %v", createError)
	}

	// 期待が合えば変わる。
	testingT.Run("期待する状態なら変わる", func(subTest *testing.T) {
		changed, updateError := deployRepository.UpdateStatus(context.Background(), "deploy-00000001", model.DeployStatusPending, model.DeployStatusActive)
		if updateError != nil || !changed {
			subTest.Fatalf("変わるはず: %v %v", changed, updateError)
		}
		deploy, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
		if deploy.Status != model.DeployStatusActive {
			subTest.Errorf("active になるはず: %q", deploy.Status)
		}
	})
	// 期待が合わなければ変わらない(古い結果で上書きしない)。
	testingT.Run("期待する状態でなければ、変えずに false", func(subTest *testing.T) {
		changed, updateError := deployRepository.UpdateStatus(context.Background(), "deploy-00000001", model.DeployStatusPending, model.DeployStatusFailed)
		if updateError != nil || changed {
			subTest.Fatalf("変わらないはず: %v %v", changed, updateError)
		}
		deploy, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
		if deploy.Status != model.DeployStatusActive {
			subTest.Errorf("active のままのはず: %q", deploy.Status)
		}
	})
	// 無い deploy は、false になる。
	testingT.Run("無い deploy は false", func(subTest *testing.T) {
		changed, updateError := deployRepository.UpdateStatus(context.Background(), "deploy-99999999", model.DeployStatusPending, model.DeployStatusActive)
		if updateError != nil || changed {
			subTest.Errorf("false のはず: %v %v", changed, updateError)
		}
	})
}

// TestDeployRepositoryDeleteUnstarted は、pending の deploy だけが物理削除され、
// 起動済み(active)のものは消えず、繰り返しても壊れないことを確認する。
func TestDeployRepositoryDeleteUnstarted(testingT *testing.T) {
	// pending と active の deploy を1件ずつ作る。
	_, deployRepository := newDeployTestEnvironment(testingT)
	for number := 1; number <= 2; number++ {
		if createError := deployRepository.Create(context.Background(), newTestDeploy(number, "project-00000001", "user-a")); createError != nil {
			testingT.Fatalf("作成できませんでした: %v", createError)
		}
	}
	if _, updateError := deployRepository.UpdateStatus(context.Background(), "deploy-00000002", model.DeployStatusPending, model.DeployStatusActive); updateError != nil {
		testingT.Fatalf("状態を変えられませんでした: %v", updateError)
	}

	// active のものは消えない。
	testingT.Run("active の deploy は消えない", func(subTest *testing.T) {
		if deleteError := deployRepository.DeleteUnstarted(context.Background(), "deploy-00000002"); deleteError != nil {
			subTest.Fatalf("エラーにならないはず: %v", deleteError)
		}
		if _, findError := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000002"); findError != nil {
			subTest.Errorf("消えていないはず: %v", findError)
		}
	})
	// pending のものは消え、2回目もエラーにならない。
	testingT.Run("pending の deploy は消え、2回目もエラーにならない", func(subTest *testing.T) {
		for range 2 {
			if deleteError := deployRepository.DeleteUnstarted(context.Background(), "deploy-00000001"); deleteError != nil {
				subTest.Fatalf("エラーにならないはず: %v", deleteError)
			}
		}
		_, findError := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
		if apperror.KindOf(findError) != apperror.KindNotFound {
			subTest.Errorf("not_found のはず: %v", findError)
		}
	})
}
