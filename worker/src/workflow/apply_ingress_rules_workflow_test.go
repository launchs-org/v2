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

// newApplyIngressRulesEnvironment は、ルールの反映の Workflow のテスト環境を作り、3つの Activity を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。Activity は、呼び出し側が OnActivity で振る舞いを決める。
func newApplyIngressRulesEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る(待ち時間は、時計を進めて飛ばす)。
	testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	// Activity は、名前だけを登録する(中身はテストが差し替える)。
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.ApplyIngressRulesActivityInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.ApplyIngressRulesActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.ApplyIngressRulesActivityInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.RollbackIngressRulesActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.CommitIngressRulesResultInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.CommitIngressRulesResultActivityName})
	return testEnvironment
}

// TestApplyIngressRulesWorkflow は、更新、確定の順に進み、失敗したら、IngressRoute を元のルールに戻してから、失敗を確定して、
// 失敗で終わることを確認する。
func TestApplyIngressRulesWorkflow(testingT *testing.T) {
	activityInput := sharedworkflow.ApplyIngressRulesActivityInput{IngressID: "ingress-1"}
	succeededInput := sharedworkflow.CommitIngressRulesResultInput{IngressID: "ingress-1", Succeeded: true}
	failedInput := sharedworkflow.CommitIngressRulesResultInput{IngressID: "ingress-1", Succeeded: false}
	workflowInput := sharedworkflow.ApplyIngressRulesInput{IngressID: "ingress-1"}

	testingT.Run("成功すると、成功を確定して、成功で終わる(巻き戻しはしない)", func(subTest *testing.T) {
		testEnvironment := newApplyIngressRulesEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyIngressRulesActivityName, mock.Anything, activityInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitIngressRulesResultActivityName, mock.Anything, succeededInput).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyIngressRulesWorkflow, workflowInput)
		if !testEnvironment.IsWorkflowCompleted() || testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.RollbackIngressRulesActivityName, mock.Anything, activityInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.CommitIngressRulesResultActivityName, mock.Anything, failedInput)
	})
	testingT.Run("更新が一時的に失敗しても、リトライで回復すれば、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyIngressRulesEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyIngressRulesActivityName, mock.Anything, activityInput).Return(errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.ApplyIngressRulesActivityName, mock.Anything, activityInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitIngressRulesResultActivityName, mock.Anything, succeededInput).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyIngressRulesWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("リトライで回復して成功するはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.ApplyIngressRulesActivityName, 3)
	})
	testingT.Run("更新が期限まで失敗し続けると、元のルールに戻し、失敗を確定して、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyIngressRulesEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyIngressRulesActivityName, mock.Anything, activityInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.RollbackIngressRulesActivityName, mock.Anything, activityInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitIngressRulesResultActivityName, mock.Anything, failedInput).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyIngressRulesWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.RollbackIngressRulesActivityName, mock.Anything, activityInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitIngressRulesResultActivityName, mock.Anything, failedInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.CommitIngressRulesResultActivityName, mock.Anything, succeededInput)
	})
	testingT.Run("リトライしないエラーなら、すぐ元のルールに戻して、失敗を確定する", func(subTest *testing.T) {
		testEnvironment := newApplyIngressRulesEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyIngressRulesActivityName, mock.Anything, activityInput).
			Return(temporal.NewNonRetryableApplicationError("bad", "NoPendingRules", nil))
		testEnvironment.OnActivity(sharedworkflow.RollbackIngressRulesActivityName, mock.Anything, activityInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitIngressRulesResultActivityName, mock.Anything, failedInput).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyIngressRulesWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.ApplyIngressRulesActivityName, 1)
	})
	testingT.Run("元に戻せなくても、失敗を確定して、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyIngressRulesEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyIngressRulesActivityName, mock.Anything, activityInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.RollbackIngressRulesActivityName, mock.Anything, activityInput).Return(errors.New("k8s still down"))
		testEnvironment.OnActivity(sharedworkflow.CommitIngressRulesResultActivityName, mock.Anything, failedInput).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyIngressRulesWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitIngressRulesResultActivityName, mock.Anything, failedInput)
	})
	testingT.Run("確定に失敗し続けたら、成功した反映でも、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newApplyIngressRulesEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.ApplyIngressRulesActivityName, mock.Anything, activityInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitIngressRulesResultActivityName, mock.Anything, succeededInput).Return(errors.New("database down"))
		testEnvironment.ExecuteWorkflow(ApplyIngressRulesWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
	})
}

// TestDeleteIngressWorkflow は、IngressRoute、レコードの順に削除し、期限まで失敗し続けたら、failed にして失敗で終わることを確認する。
func TestDeleteIngressWorkflow(testingT *testing.T) {
	// 3つの Activity を、名前で登録した環境を作る。
	newEnvironment := func() *testsuite.TestWorkflowEnvironment {
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteIngressObjectInput) error { return nil },
			activity.RegisterOptions{Name: sharedworkflow.DeleteIngressObjectActivityName})
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteIngressRecordInput) error { return nil },
			activity.RegisterOptions{Name: sharedworkflow.DeleteIngressRecordActivityName})
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.MarkIngressDeleteFailedInput) error { return nil },
			activity.RegisterOptions{Name: sharedworkflow.MarkIngressDeleteFailedActivityName})
		return testEnvironment
	}
	objectInput := sharedworkflow.DeleteIngressObjectInput{IngressID: "ingress-1"}
	recordInput := sharedworkflow.DeleteIngressRecordInput{IngressID: "ingress-1"}
	failedInput := sharedworkflow.MarkIngressDeleteFailedInput{IngressID: "ingress-1"}
	workflowInput := sharedworkflow.DeleteIngressInput{IngressID: "ingress-1"}

	testingT.Run("IngressRoute とレコードを削除して、成功で終わる(failed にはしない)", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.DeleteIngressObjectActivityName, mock.Anything, objectInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteIngressRecordActivityName, mock.Anything, recordInput).Return(nil)
		testEnvironment.ExecuteWorkflow(DeleteIngressWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.MarkIngressDeleteFailedActivityName, mock.Anything, failedInput)
	})
	testingT.Run("IngressRoute の削除が期限まで失敗し続けたら、レコードは消さずに failed にして、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.DeleteIngressObjectActivityName, mock.Anything, objectInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkIngressDeleteFailedActivityName, mock.Anything, failedInput).Return(nil)
		testEnvironment.ExecuteWorkflow(DeleteIngressWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.DeleteIngressRecordActivityName, mock.Anything, recordInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkIngressDeleteFailedActivityName, mock.Anything, failedInput)
	})
	testingT.Run("レコードの削除が失敗し続けたら、failed にして、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.DeleteIngressObjectActivityName, mock.Anything, objectInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteIngressRecordActivityName, mock.Anything, recordInput).Return(errors.New("database down"))
		testEnvironment.OnActivity(sharedworkflow.MarkIngressDeleteFailedActivityName, mock.Anything, failedInput).Return(nil)
		testEnvironment.ExecuteWorkflow(DeleteIngressWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkIngressDeleteFailedActivityName, mock.Anything, failedInput)
	})
}
