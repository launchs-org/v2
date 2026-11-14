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

// newScaleDeployEnvironment は、スケーリングの Workflow のテスト環境を作り、Activity を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。Activity は、呼び出し側が OnActivity で振る舞いを決める。
func newScaleDeployEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る(待ち時間は、時計を進めて飛ばす)。
	testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	// Activity は、名前だけを登録する(中身はテストが差し替える)。
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.ApplyScaleInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.ApplyScaleActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.WaitDeploymentReadyInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.WaitDeploymentReadyActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.MarkScaleResultInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.MarkScaleResultActivityName})
	return testEnvironment
}

// TestScaleDeployWorkflow は、更新、起動待ち、記録の順に進み、成功なら成功を、失敗なら失敗を記録することを確認する。
func TestScaleDeployWorkflow(testingT *testing.T) {
	// 各 Activity の入力と、記録する結果。
	applyInput := sharedworkflow.ApplyScaleInput{DeployID: "deploy-1"}
	waitInput := sharedworkflow.WaitDeploymentReadyInput{DeployID: "deploy-1"}
	succeededInput := sharedworkflow.MarkScaleResultInput{DeployID: "deploy-1", Succeeded: true}
	failedInput := sharedworkflow.MarkScaleResultInput{DeployID: "deploy-1", Succeeded: false}

	testingT.Run("全て成功すると、成功を記録して、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newScaleDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyScaleActivityName, mock.Anything, applyInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.MarkScaleResultActivityName, mock.Anything, succeededInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ScaleDeployWorkflow, sharedworkflow.ScaleDeployInput{DeployID: "deploy-1"})

		if !testEnvironment.IsWorkflowCompleted() || testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkScaleResultActivityName, mock.Anything, succeededInput)
	})

	testingT.Run("Deployment を更新できなければ、起動は待たず、失敗を記録して失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newScaleDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyScaleActivityName, mock.Anything, applyInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkScaleResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ScaleDeployWorkflow, sharedworkflow.ScaleDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkScaleResultActivityName, mock.Anything, failedInput)
	})

	testingT.Run("更新が一時的に失敗しても、リトライで回復すれば成功で終わる", func(subTest *testing.T) {
		testEnvironment := newScaleDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyScaleActivityName, mock.Anything, applyInput).Return(errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.ApplyScaleActivityName, mock.Anything, applyInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.MarkScaleResultActivityName, mock.Anything, succeededInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ScaleDeployWorkflow, sharedworkflow.ScaleDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("リトライで回復して成功するはず: %v", testEnvironment.GetWorkflowError())
		}
	})

	testingT.Run("起動が期限まで確認できなければ、失敗を記録して失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newScaleDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyScaleActivityName, mock.Anything, applyInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(errors.New("not ready in time"))
		testEnvironment.OnActivity(sharedworkflow.MarkScaleResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ScaleDeployWorkflow, sharedworkflow.ScaleDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkScaleResultActivityName, mock.Anything, failedInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.MarkScaleResultActivityName, mock.Anything, succeededInput)
	})

	testingT.Run("記録に失敗したときは、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newScaleDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyScaleActivityName, mock.Anything, applyInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.MarkScaleResultActivityName, mock.Anything, succeededInput).Return(errors.New("database down"))

		testEnvironment.ExecuteWorkflow(ScaleDeployWorkflow, sharedworkflow.ScaleDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
	})
}
