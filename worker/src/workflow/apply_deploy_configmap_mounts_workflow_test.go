package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	sharedworkflow "launchs/shared/workflow"
)

// newApplyDeployConfigMapMountsEnvironment は、ConfigMap のマウントの反映の Workflow のテスト環境を作り、Activity を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。Activity は、呼び出し側が OnActivity で振る舞いを決める。
func newApplyDeployConfigMapMountsEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る(待ち時間は、時計を進めて飛ばす)。
	testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	// Activity は、名前だけを登録する(中身はテストが差し替える)。
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.ApplyPendingConfigMapMountsInput) (int64, error) { return 0, nil },
		activity.RegisterOptions{Name: sharedworkflow.ApplyPendingConfigMapMountsActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.WaitDeploymentReadyInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.WaitDeploymentReadyActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.RollbackDeploymentConfigMapMountsInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.RollbackDeploymentConfigMapMountsActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.CommitConfigMapMountsResultInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.CommitConfigMapMountsResultActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.CleanupConfigMapRevisionsInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.CleanupConfigMapRevisionsActivityName})
	return testEnvironment
}

// TestApplyDeployConfigMapMountsWorkflow は、更新、起動待ち、確定、後始末の順に進み、成功なら成功を確定して版を片付け、
// 失敗なら(元に戻して)失敗を確定し、片付けないことを確認する。
func TestApplyDeployConfigMapMountsWorkflow(testingT *testing.T) {
	// 各 Activity の入力と、確定する結果。
	applyInput := sharedworkflow.ApplyPendingConfigMapMountsInput{DeployID: "deploy-1"}
	waitInput := sharedworkflow.WaitDeploymentReadyInput{DeployID: "deploy-1", MinGeneration: 1}
	succeededInput := sharedworkflow.CommitConfigMapMountsResultInput{DeployID: "deploy-1", Succeeded: true}
	failedInput := sharedworkflow.CommitConfigMapMountsResultInput{DeployID: "deploy-1", Succeeded: false}
	rollbackInput := sharedworkflow.RollbackDeploymentConfigMapMountsInput{DeployID: "deploy-1"}
	cleanupInput := sharedworkflow.CleanupConfigMapRevisionsInput{DeployID: "deploy-1"}

	testingT.Run("全て成功すると、成功を確定し、使われなくなった版を片付けて、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigMapMountsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigMapMountsActivityName, mock.Anything, applyInput).Return(int64(1), nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitConfigMapMountsResultActivityName, mock.Anything, succeededInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CleanupConfigMapRevisionsActivityName, mock.Anything, cleanupInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigMapMountsWorkflow, sharedworkflow.ApplyDeployConfigMapMountsInput{DeployID: "deploy-1"})

		if !testEnvironment.IsWorkflowCompleted() || testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitConfigMapMountsResultActivityName, mock.Anything, succeededInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.CleanupConfigMapRevisionsActivityName, mock.Anything, cleanupInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.RollbackDeploymentConfigMapMountsActivityName, mock.Anything, rollbackInput)
	})

	testingT.Run("片付けに失敗しても、反映は成功で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigMapMountsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigMapMountsActivityName, mock.Anything, applyInput).Return(int64(1), nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitConfigMapMountsResultActivityName, mock.Anything, succeededInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CleanupConfigMapRevisionsActivityName, mock.Anything, cleanupInput).Return(errors.New("k8s down"))

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigMapMountsWorkflow, sharedworkflow.ApplyDeployConfigMapMountsInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("片付けの失敗は、結果に影響しないはず: %v", testEnvironment.GetWorkflowError())
		}
	})

	testingT.Run("Deployment を更新できなければ、起動は待たず、元に戻して、失敗を確定し、片付けずに失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigMapMountsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigMapMountsActivityName, mock.Anything, applyInput).Return(int64(0), errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.RollbackDeploymentConfigMapMountsActivityName, mock.Anything, rollbackInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitConfigMapMountsResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigMapMountsWorkflow, sharedworkflow.ApplyDeployConfigMapMountsInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.RollbackDeploymentConfigMapMountsActivityName, mock.Anything, rollbackInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitConfigMapMountsResultActivityName, mock.Anything, failedInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.CleanupConfigMapRevisionsActivityName, mock.Anything, cleanupInput)
	})

	testingT.Run("起動が期限まで確認できなければ、元に戻して、失敗を確定して失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigMapMountsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigMapMountsActivityName, mock.Anything, applyInput).Return(int64(1), nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(errors.New("not ready in time"))
		testEnvironment.OnActivity(sharedworkflow.RollbackDeploymentConfigMapMountsActivityName, mock.Anything, rollbackInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitConfigMapMountsResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigMapMountsWorkflow, sharedworkflow.ApplyDeployConfigMapMountsInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitConfigMapMountsResultActivityName, mock.Anything, failedInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.CommitConfigMapMountsResultActivityName, mock.Anything, succeededInput)
	})

	testingT.Run("元に戻せなくても、失敗を確定して失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigMapMountsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigMapMountsActivityName, mock.Anything, applyInput).Return(int64(0), errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.RollbackDeploymentConfigMapMountsActivityName, mock.Anything, rollbackInput).Return(errors.New("k8s still down"))
		testEnvironment.OnActivity(sharedworkflow.CommitConfigMapMountsResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigMapMountsWorkflow, sharedworkflow.ApplyDeployConfigMapMountsInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitConfigMapMountsResultActivityName, mock.Anything, failedInput)
	})

	testingT.Run("確定に失敗したときは、反映の結果が成功でも、失敗で終わる(片付けない)", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigMapMountsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigMapMountsActivityName, mock.Anything, applyInput).Return(int64(1), nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitConfigMapMountsResultActivityName, mock.Anything, succeededInput).Return(errors.New("database down"))

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigMapMountsWorkflow, sharedworkflow.ApplyDeployConfigMapMountsInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.CleanupConfigMapRevisionsActivityName, mock.Anything, cleanupInput)
	})
}
