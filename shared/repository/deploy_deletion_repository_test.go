package repository

import (
	"context"
	"launchs/shared/apperror"
	"testing"
)

// TestDeployRepositoryDeletionFlow は、削除の受け付け(状態ごとの可否)、巻き戻し、失敗の記録、
// レコードの物理削除(削除中のものだけ)を確認する。
func TestDeployRepositoryDeletionFlow(testingT *testing.T) {
	// 受け付けの可否。
	acceptCases := []struct {
		status       string
		expectedKind apperror.Kind
	}{
		{status: "active"}, {status: "scaled_zero"}, {status: "failed"}, {status: "delete_failed"},
		{status: "pending", expectedKind: apperror.KindConflict},
		{status: "applying", expectedKind: apperror.KindConflict},
		{status: "deleting", expectedKind: apperror.KindConflict},
	}
	for _, testCase := range acceptCases {
		testingT.Run("受け付け: "+testCase.status, func(subTest *testing.T) {
			database, deployRepository := newDeployTestEnvironment(subTest)
			database.Create(newTestDeploy(1, "project-00000001", "user-a"))
			database.Exec("UPDATE deploys SET status = ? WHERE id = 'deploy-00000001'", testCase.status)

			previousStatus, markError := deployRepository.MarkDeleting(context.Background(), "user-a", "deploy-00000001")

			deploy, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
			if apperror.KindOf(markError) != testCase.expectedKind {
				subTest.Fatalf("エラーの種類が違う: %v", markError)
			}
			if testCase.expectedKind != "" {
				// 拒否されたときは、状態が変わらない。
				if deploy.Status != testCase.status {
					subTest.Errorf("状態は変わらないはず: %q", deploy.Status)
				}
				return
			}
			// 受け付けられたときは、deleting になり、元の状態が返る。
			if deploy.Status != "deleting" || previousStatus != testCase.status {
				subTest.Errorf("deleting になり、元の状態が返るはず: %q %q", deploy.Status, previousStatus)
			}
			// 巻き戻すと、元の状態になる。
			if revertError := deployRepository.RevertDeleting(context.Background(), "deploy-00000001", previousStatus); revertError != nil {
				subTest.Fatalf("戻せるはず: %v", revertError)
			}
			reverted, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
			if reverted.Status != testCase.status {
				subTest.Errorf("元の状態に戻るはず: %q", reverted.Status)
			}
		})
	}
	// 他人の deploy は not_found。
	testingT.Run("他人の deploy は not_found", func(subTest *testing.T) {
		database, deployRepository := newDeployTestEnvironment(subTest)
		database.Create(newTestDeploy(1, "project-00000001", "user-a"))
		if _, markError := deployRepository.MarkDeleting(context.Background(), "user-b", "deploy-00000001"); apperror.KindOf(markError) != apperror.KindNotFound {
			subTest.Errorf("not_found のはず: %v", markError)
		}
	})
	// 失敗の記録とレコードの削除は、削除中のものだけが対象。
	testingT.Run("失敗の記録とレコードの削除は、削除中の deploy だけが対象で、繰り返しても壊れない", func(subTest *testing.T) {
		database, deployRepository := newDeployTestEnvironment(subTest)
		database.Create(newTestDeploy(1, "project-00000001", "user-a"))
		database.Create(newTestDeploy(2, "project-00000001", "user-a"))
		database.Exec("UPDATE deploys SET status = 'deleting' WHERE id = 'deploy-00000001'")
		database.Exec("UPDATE deploys SET status = 'active' WHERE id = 'deploy-00000002'")

		// active のものには、効かない。
		if markError := deployRepository.MarkDeleteFailed(context.Background(), "deploy-00000002"); markError != nil {
			subTest.Fatalf("エラーにならないはず: %v", markError)
		}
		if deleteError := deployRepository.DeleteRecord(context.Background(), "deploy-00000002"); deleteError != nil {
			subTest.Fatalf("エラーにならないはず: %v", deleteError)
		}
		if untouched, findError := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000002"); findError != nil || untouched.Status != "active" {
			subTest.Fatalf("active のものは変わらず残るはず: %v %q", findError, untouched.Status)
		}
		// 削除中のものは、delete_failed になり、その状態では消えない。
		if markError := deployRepository.MarkDeleteFailed(context.Background(), "deploy-00000001"); markError != nil {
			subTest.Fatalf("記録できるはず: %v", markError)
		}
		failed, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
		if failed.Status != "delete_failed" {
			subTest.Fatalf("delete_failed になるはず: %q", failed.Status)
		}
		_ = deployRepository.DeleteRecord(context.Background(), "deploy-00000001")
		if _, findError := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001"); findError != nil {
			subTest.Fatalf("delete_failed のものは消えないはず: %v", findError)
		}
		// 削除中に戻して消すと、消える。繰り返しても壊れない。
		database.Exec("UPDATE deploys SET status = 'deleting' WHERE id = 'deploy-00000001'")
		for attempt := 0; attempt < 2; attempt++ {
			if deleteError := deployRepository.DeleteRecord(context.Background(), "deploy-00000001"); deleteError != nil {
				subTest.Fatalf("消せるはず(%d回目): %v", attempt+1, deleteError)
			}
		}
		if _, findError := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001"); apperror.KindOf(findError) != apperror.KindNotFound {
			subTest.Errorf("消えているはず: %v", findError)
		}
	})
}
