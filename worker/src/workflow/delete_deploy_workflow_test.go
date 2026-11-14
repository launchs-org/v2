package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	sharedworkflow "launchs/shared/workflow"
)

// newDeleteDeployEnvironment は、deploy 削除の Workflow のテスト環境を作り、3つの Activity を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。Activity は、呼び出し側が OnActivity で振る舞いを決める。
func newDeleteDeployEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る(待ち時間は、時計を進めて飛ばす)。
	testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	// Activity は、名前だけを登録する(中身はテストが差し替える)。
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteDeployServiceInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.DeleteDeployServiceActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteDeploymentInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.DeleteDeploymentActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteDeployRecordInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.DeleteDeployRecordActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.MarkDeployDeleteFailedInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.MarkDeployDeleteFailedActivityName})
	return testEnvironment
}

// TestDeleteDeployWorkflow は、Deployment、レコードの順に削除し、失敗したら delete_failed にして失敗で終わることを確認する。
func TestDeleteDeployWorkflow(testingT *testing.T) {
	// 各 Activity の入力。
	serviceInput := sharedworkflow.DeleteDeployServiceInput{DeployID: "deploy-1"}
	deploymentInput := sharedworkflow.DeleteDeploymentInput{DeployID: "deploy-1"}
	recordInput := sharedworkflow.DeleteDeployRecordInput{DeployID: "deploy-1"}
	failedInput := sharedworkflow.MarkDeployDeleteFailedInput{DeployID: "deploy-1"}

	testingT.Run("成功すると、Deployment、レコードの順に削除して、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newDeleteDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployServiceActivityName, mock.Anything, serviceInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeploymentActivityName, mock.Anything, deploymentInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployRecordActivityName, mock.Anything, recordInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteDeployWorkflow, sharedworkflow.DeleteDeployInput{DeployID: "deploy-1"})

		if !testEnvironment.IsWorkflowCompleted() || testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.MarkDeployDeleteFailedActivityName, mock.Anything, failedInput)
	})

	testingT.Run("Deployment の削除が一時的に失敗しても、リトライで回復すれば成功で終わる", func(subTest *testing.T) {
		testEnvironment := newDeleteDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployServiceActivityName, mock.Anything, serviceInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeploymentActivityName, mock.Anything, deploymentInput).Return(errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.DeleteDeploymentActivityName, mock.Anything, deploymentInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployRecordActivityName, mock.Anything, recordInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteDeployWorkflow, sharedworkflow.DeleteDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("リトライで回復して成功するはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.DeleteDeploymentActivityName, 3)
	})

	testingT.Run("Deployment の削除が期限まで失敗し続けると、レコードは消さず、delete_failed にして失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newDeleteDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployServiceActivityName, mock.Anything, serviceInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeploymentActivityName, mock.Anything, deploymentInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkDeployDeleteFailedActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteDeployWorkflow, sharedworkflow.DeleteDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.DeleteDeployRecordActivityName, mock.Anything, recordInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkDeployDeleteFailedActivityName, mock.Anything, failedInput)
	})

	testingT.Run("リトライしないエラーなら、すぐ delete_failed にして失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newDeleteDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployServiceActivityName, mock.Anything, serviceInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeploymentActivityName, mock.Anything, deploymentInput).
			Return(temporal.NewNonRetryableApplicationError("bad name", "InvalidDeploymentName", nil))
		testEnvironment.OnActivity(sharedworkflow.MarkDeployDeleteFailedActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteDeployWorkflow, sharedworkflow.DeleteDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.DeleteDeploymentActivityName, 1)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkDeployDeleteFailedActivityName, mock.Anything, failedInput)
	})

	testingT.Run("service の k8s の Service の削除が期限まで失敗し続けると、Deployment もレコードも消さず、delete_failed にして失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newDeleteDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployServiceActivityName, mock.Anything, serviceInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkDeployDeleteFailedActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteDeployWorkflow, sharedworkflow.DeleteDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.DeleteDeploymentActivityName, mock.Anything, deploymentInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.DeleteDeployRecordActivityName, mock.Anything, recordInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkDeployDeleteFailedActivityName, mock.Anything, failedInput)
	})

	testingT.Run("service の削除が一時的に失敗しても、リトライで回復すれば、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newDeleteDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployServiceActivityName, mock.Anything, serviceInput).Return(errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployServiceActivityName, mock.Anything, serviceInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeploymentActivityName, mock.Anything, deploymentInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployRecordActivityName, mock.Anything, recordInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteDeployWorkflow, sharedworkflow.DeleteDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("リトライで回復して成功するはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.DeleteDeployServiceActivityName, 3)
	})

	testingT.Run("レコードの削除が失敗し続けても、delete_failed にして失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newDeleteDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployServiceActivityName, mock.Anything, serviceInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeploymentActivityName, mock.Anything, deploymentInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteDeployRecordActivityName, mock.Anything, recordInput).Return(errors.New("database down"))
		testEnvironment.OnActivity(sharedworkflow.MarkDeployDeleteFailedActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteDeployWorkflow, sharedworkflow.DeleteDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkDeployDeleteFailedActivityName, mock.Anything, failedInput)
	})
}
