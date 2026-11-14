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

// newApplyDeployConfigEnvironment は、起動設定の反映の Workflow のテスト環境を作り、Activity を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。Activity は、呼び出し側が OnActivity で振る舞いを決める。
func newApplyDeployConfigEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る(待ち時間は、時計を進めて飛ばす)。
	testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	// Activity は、名前だけを登録する(中身はテストが差し替える)。
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.ApplyPendingConfigInput) (int64, error) { return 0, nil },
		activity.RegisterOptions{Name: sharedworkflow.ApplyPendingConfigActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.WaitDeploymentReadyInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.WaitDeploymentReadyActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.RollbackDeploymentConfigInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.RollbackDeploymentConfigActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.CommitConfigResultInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.CommitConfigResultActivityName})
	return testEnvironment
}

// TestApplyDeployConfigWorkflow は、更新、起動待ち、確定の順に進み、成功なら成功を、どこかで失敗したら
// (以降は進まずに)失敗を確定して、失敗で終わることを確認する。
func TestApplyDeployConfigWorkflow(testingT *testing.T) {
	// 各 Activity の入力と、確定する結果。
	applyInput := sharedworkflow.ApplyPendingConfigInput{DeployID: "deploy-1"}
	waitInput := sharedworkflow.WaitDeploymentReadyInput{DeployID: "deploy-1", MinGeneration: 1}
	succeededInput := sharedworkflow.CommitConfigResultInput{DeployID: "deploy-1", Succeeded: true}
	failedInput := sharedworkflow.CommitConfigResultInput{DeployID: "deploy-1", Succeeded: false}
	rollbackInput := sharedworkflow.RollbackDeploymentConfigInput{DeployID: "deploy-1"}

	testingT.Run("全て成功すると、成功を確定して、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigActivityName, mock.Anything, applyInput).Return(int64(1), nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitConfigResultActivityName, mock.Anything, succeededInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigWorkflow, sharedworkflow.ApplyDeployConfigInput{DeployID: "deploy-1"})

		if !testEnvironment.IsWorkflowCompleted() || testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitConfigResultActivityName, mock.Anything, succeededInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.CommitConfigResultActivityName, mock.Anything, failedInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.RollbackDeploymentConfigActivityName, mock.Anything, rollbackInput)
	})

	testingT.Run("Deployment を更新できなければ、起動は待たず、失敗を確定して失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigActivityName, mock.Anything, applyInput).Return(int64(0), errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.RollbackDeploymentConfigActivityName, mock.Anything, rollbackInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitConfigResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigWorkflow, sharedworkflow.ApplyDeployConfigInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitConfigResultActivityName, mock.Anything, failedInput)
	})

	testingT.Run("更新が一時的に失敗しても、リトライで回復すれば成功で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigActivityName, mock.Anything, applyInput).Return(int64(0), errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigActivityName, mock.Anything, applyInput).Return(int64(1), nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitConfigResultActivityName, mock.Anything, succeededInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigWorkflow, sharedworkflow.ApplyDeployConfigInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("リトライで回復して成功するはず: %v", testEnvironment.GetWorkflowError())
		}
	})

	testingT.Run("起動が期限まで確認できなければ、失敗を確定して失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigActivityName, mock.Anything, applyInput).Return(int64(1), nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(errors.New("not ready in time"))
		testEnvironment.OnActivity(sharedworkflow.RollbackDeploymentConfigActivityName, mock.Anything, rollbackInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitConfigResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigWorkflow, sharedworkflow.ApplyDeployConfigInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.RollbackDeploymentConfigActivityName, mock.Anything, rollbackInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitConfigResultActivityName, mock.Anything, failedInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.CommitConfigResultActivityName, mock.Anything, succeededInput)
	})

	testingT.Run("元の値に戻せなくても、失敗を確定して失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigActivityName, mock.Anything, applyInput).Return(int64(0), errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.RollbackDeploymentConfigActivityName, mock.Anything, rollbackInput).Return(errors.New("k8s still down"))
		testEnvironment.OnActivity(sharedworkflow.CommitConfigResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigWorkflow, sharedworkflow.ApplyDeployConfigInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitConfigResultActivityName, mock.Anything, failedInput)
	})

	testingT.Run("確定に失敗したときは、反映の結果が成功でも、失敗で終わる(applying は残る)", func(subTest *testing.T) {
		testEnvironment := newApplyDeployConfigEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigActivityName, mock.Anything, applyInput).Return(int64(1), nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitConfigResultActivityName, mock.Anything, succeededInput).Return(errors.New("database down"))

		testEnvironment.ExecuteWorkflow(ApplyDeployConfigWorkflow, sharedworkflow.ApplyDeployConfigInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
	})
}

// TestApplyDeployConfigWorkflowPassesGeneration は、更新(作成)した Deployment の世代が、そのまま起動待ちの下限として渡されることを確認する。
func TestApplyDeployConfigWorkflowPassesGeneration(testingT *testing.T) {
	// 更新が世代 7 を返すようにして、起動待ちが、世代 7 を下限として呼ばれることを、入力の一致で確かめる。
	testEnvironment := newApplyDeployConfigEnvironment(testingT)
	testEnvironment.OnActivity(sharedworkflow.ApplyPendingConfigActivityName, mock.Anything, sharedworkflow.ApplyPendingConfigInput{DeployID: "deploy-1"}).Return(int64(7), nil)
	testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, sharedworkflow.WaitDeploymentReadyInput{DeployID: "deploy-1", MinGeneration: 7}).Return(nil)
	testEnvironment.OnActivity(sharedworkflow.CommitConfigResultActivityName, mock.Anything, sharedworkflow.CommitConfigResultInput{DeployID: "deploy-1", Succeeded: true}).Return(nil)
	testEnvironment.ExecuteWorkflow(ApplyDeployConfigWorkflow, sharedworkflow.ApplyDeployConfigInput{DeployID: "deploy-1"})

	if testEnvironment.GetWorkflowError() != nil {
		testingT.Fatalf("世代 7 を下限として起動待ちが呼ばれ、成功で終わるはず: %v", testEnvironment.GetWorkflowError())
	}
	testEnvironment.AssertCalled(testingT, sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, sharedworkflow.WaitDeploymentReadyInput{DeployID: "deploy-1", MinGeneration: 7})
}
