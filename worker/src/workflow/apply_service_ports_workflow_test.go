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

// newApplyServicePortsEnvironment は、ポートの反映の Workflow のテスト環境を作り、3つの Activity を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。Activity は、呼び出し側が OnActivity で振る舞いを決める。
func newApplyServicePortsEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る(待ち時間は、時計を進めて飛ばす)。
	testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	// Activity は、名前だけを登録する(中身はテストが差し替える)。
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.ApplyServicePortsActivityInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.ApplyServicePortsActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.ApplyServicePortsActivityInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.RollbackServicePortsActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.CommitServicePortsResultInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.CommitServicePortsResultActivityName})
	return testEnvironment
}

// TestApplyServicePortsWorkflow は、更新、確定の順に進み、失敗したら、k8s を元のポートに戻してから、失敗を確定して、
// 失敗で終わることを確認する。
func TestApplyServicePortsWorkflow(testingT *testing.T) {
	activityInput := sharedworkflow.ApplyServicePortsActivityInput{ServiceID: "service-1"}
	succeededInput := sharedworkflow.CommitServicePortsResultInput{ServiceID: "service-1", Succeeded: true}
	failedInput := sharedworkflow.CommitServicePortsResultInput{ServiceID: "service-1", Succeeded: false}

	testingT.Run("成功すると、成功を確定して、成功で終わる(巻き戻しはしない)", func(subTest *testing.T) {
		testEnvironment := newApplyServicePortsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyServicePortsActivityName, mock.Anything, activityInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitServicePortsResultActivityName, mock.Anything, succeededInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyServicePortsWorkflow, sharedworkflow.ApplyServicePortsInput{ServiceID: "service-1"})

		if !testEnvironment.IsWorkflowCompleted() || testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.RollbackServicePortsActivityName, mock.Anything, activityInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.CommitServicePortsResultActivityName, mock.Anything, failedInput)
	})
	testingT.Run("更新が一時的に失敗しても、リトライで回復すれば、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyServicePortsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyServicePortsActivityName, mock.Anything, activityInput).Return(errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.ApplyServicePortsActivityName, mock.Anything, activityInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitServicePortsResultActivityName, mock.Anything, succeededInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyServicePortsWorkflow, sharedworkflow.ApplyServicePortsInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("リトライで回復して成功するはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.ApplyServicePortsActivityName, 3)
	})
	testingT.Run("更新が期限まで失敗し続けると、Service を元に戻し、失敗を確定して、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyServicePortsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyServicePortsActivityName, mock.Anything, activityInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.RollbackServicePortsActivityName, mock.Anything, activityInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitServicePortsResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyServicePortsWorkflow, sharedworkflow.ApplyServicePortsInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.RollbackServicePortsActivityName, mock.Anything, activityInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitServicePortsResultActivityName, mock.Anything, failedInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.CommitServicePortsResultActivityName, mock.Anything, succeededInput)
	})
	testingT.Run("リトライしないエラーなら、すぐ Service を元に戻して、失敗を確定する", func(subTest *testing.T) {
		testEnvironment := newApplyServicePortsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyServicePortsActivityName, mock.Anything, activityInput).
			Return(temporal.NewNonRetryableApplicationError("bad", "NoPendingPorts", nil))
		testEnvironment.OnActivity(sharedworkflow.RollbackServicePortsActivityName, mock.Anything, activityInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitServicePortsResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyServicePortsWorkflow, sharedworkflow.ApplyServicePortsInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.ApplyServicePortsActivityName, 1)
	})
	testingT.Run("元に戻せなくても、失敗を確定して、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyServicePortsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyServicePortsActivityName, mock.Anything, activityInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.RollbackServicePortsActivityName, mock.Anything, activityInput).Return(errors.New("k8s still down"))
		testEnvironment.OnActivity(sharedworkflow.CommitServicePortsResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(ApplyServicePortsWorkflow, sharedworkflow.ApplyServicePortsInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitServicePortsResultActivityName, mock.Anything, failedInput)
	})
	testingT.Run("確定に失敗し続けたら、成功した反映でも、失敗で終わる(deploy は applying のまま残る)", func(subTest *testing.T) {
		testEnvironment := newApplyServicePortsEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyServicePortsActivityName, mock.Anything, activityInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitServicePortsResultActivityName, mock.Anything, succeededInput).Return(errors.New("database down"))

		testEnvironment.ExecuteWorkflow(ApplyServicePortsWorkflow, sharedworkflow.ApplyServicePortsInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
	})
}
