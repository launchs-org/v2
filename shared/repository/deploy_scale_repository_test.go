package repository

import (
	"context"
	"launchs/shared/apperror"
	"testing"
)

// TestDeployRepositoryBeginScaleAndRevert は、スケーリングの開始で replicas と状態が変わり(0 にもできる)、
// 同じ数なら変わらず、受け付けない状態は拒否され、元に戻せることを確認する。
func TestDeployRepositoryBeginScaleAndRevert(testingT *testing.T) {
	// 開始の入力ごとの期待。
	testCases := []struct {
		name             string
		status           string
		replicas         int
		newReplicas      int
		expectedKind     apperror.Kind
		expectedChanged  bool
		expectedReplicas int
		expectedStatus   string
	}{
		{name: "active で数を変えれば、applying になる", status: "active", replicas: 1, newReplicas: 3, expectedChanged: true, expectedReplicas: 3, expectedStatus: "applying"},
		{name: "0 にできる", status: "active", replicas: 2, newReplicas: 0, expectedChanged: true, expectedReplicas: 0, expectedStatus: "applying"},
		{name: "scaled_zero から増やせる", status: "scaled_zero", replicas: 0, newReplicas: 1, expectedChanged: true, expectedReplicas: 1, expectedStatus: "applying"},
		{name: "同じ数なら変えない", status: "active", replicas: 2, newReplicas: 2, expectedChanged: false, expectedReplicas: 2, expectedStatus: "active"},
		{name: "failed なら、同じ数でもやり直す", status: "failed", replicas: 2, newReplicas: 2, expectedChanged: true, expectedReplicas: 2, expectedStatus: "applying"},
		{name: "applying なら conflict", status: "applying", replicas: 1, newReplicas: 2, expectedKind: apperror.KindConflict, expectedReplicas: 1, expectedStatus: "applying"},
		{name: "起動中(pending)なら conflict", status: "pending", replicas: 1, newReplicas: 2, expectedKind: apperror.KindConflict, expectedReplicas: 1, expectedStatus: "pending"},
		{name: "deleting なら conflict", status: "deleting", replicas: 1, newReplicas: 2, expectedKind: apperror.KindConflict, expectedReplicas: 1, expectedStatus: "deleting"},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			database, deployRepository := newDeployTestEnvironment(subTest)
			database.Create(newTestDeploy(1, "project-00000001", "user-a"))
			database.Exec("UPDATE deploys SET status = ?, replicas = ? WHERE id = 'deploy-00000001'", testCase.status, testCase.replicas)

			start, beginError := deployRepository.BeginScale(context.Background(), "user-a", "deploy-00000001", testCase.newReplicas)

			deploy, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
			if apperror.KindOf(beginError) != testCase.expectedKind || deploy.Replicas != testCase.expectedReplicas || deploy.Status != testCase.expectedStatus {
				subTest.Fatalf("結果が違う: %v replicas=%d status=%q", beginError, deploy.Replicas, deploy.Status)
			}
			if beginError != nil {
				return
			}
			if start.Changed != testCase.expectedChanged || start.PreviousReplicas != testCase.replicas || start.PreviousStatus != testCase.status {
				subTest.Errorf("変える前の値が違う: %+v", start)
			}
			// 変えたときは、元に戻せる。
			if testCase.expectedChanged {
				if revertError := deployRepository.RevertScale(context.Background(), "deploy-00000001", start); revertError != nil {
					subTest.Fatalf("戻せるはず: %v", revertError)
				}
				reverted, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
				if reverted.Replicas != testCase.replicas || reverted.Status != testCase.status {
					subTest.Errorf("元に戻るはず: replicas=%d status=%q", reverted.Replicas, reverted.Status)
				}
			}
		})
	}
}
