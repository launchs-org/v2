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

// newServiceWorkflowEnvironment は、service の作成・削除の Workflow のテスト環境を作り、Activity を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。Activity は、呼び出し側が OnActivity で振る舞いを決める。
func newServiceWorkflowEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る(待ち時間は、時計を進めて飛ばす)。
	testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	// Activity は、名前だけを登録する(中身はテストが差し替える)。
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.EnsureServiceInput) (sharedworkflow.EnsureServiceResult, error) {
		return sharedworkflow.EnsureServiceResult{}, nil
	}, activity.RegisterOptions{Name: sharedworkflow.EnsureServiceActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.MarkServiceResultInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.MarkServiceResultActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteServiceObjectInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.DeleteServiceObjectActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteServiceRecordInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.DeleteServiceRecordActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.MarkServiceDeleteFailedInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.MarkServiceDeleteFailedActivityName})
	return testEnvironment
}

// TestCreateServiceWorkflow は、k8s の Service を作り、払い出された値を記録して active にし、
// 作れなければ failed を記録して失敗で終わることを確認する。
func TestCreateServiceWorkflow(testingT *testing.T) {
	ensureInput := sharedworkflow.EnsureServiceInput{ServiceID: "service-1"}
	created := sharedworkflow.EnsureServiceResult{DNSName: "service-1.project-1.svc.test.local", ClusterIP: "10.112.0.5"}
	succeededInput := sharedworkflow.MarkServiceResultInput{ServiceID: "service-1", Succeeded: true, DNSName: created.DNSName, ClusterIP: created.ClusterIP}
	failedInput := sharedworkflow.MarkServiceResultInput{ServiceID: "service-1", Succeeded: false}

	testingT.Run("作れたら、払い出された DNS 名と ClusterIP を、成功として記録して、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newServiceWorkflowEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.EnsureServiceActivityName, mock.Anything, ensureInput).Return(created, nil)
		testEnvironment.OnActivity(sharedworkflow.MarkServiceResultActivityName, mock.Anything, succeededInput).Return(nil)

		testEnvironment.ExecuteWorkflow(CreateServiceWorkflow, sharedworkflow.CreateServiceInput{ServiceID: "service-1"})

		if !testEnvironment.IsWorkflowCompleted() || testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkServiceResultActivityName, mock.Anything, succeededInput)
	})
	testingT.Run("一時的に失敗しても、リトライで回復すれば、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newServiceWorkflowEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.EnsureServiceActivityName, mock.Anything, ensureInput).Return(sharedworkflow.EnsureServiceResult{}, errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.EnsureServiceActivityName, mock.Anything, ensureInput).Return(created, nil)
		testEnvironment.OnActivity(sharedworkflow.MarkServiceResultActivityName, mock.Anything, succeededInput).Return(nil)

		testEnvironment.ExecuteWorkflow(CreateServiceWorkflow, sharedworkflow.CreateServiceInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("リトライで回復して成功するはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.EnsureServiceActivityName, 3)
	})
	testingT.Run("作れなければ(期限まで失敗)、failed を記録して、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newServiceWorkflowEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.EnsureServiceActivityName, mock.Anything, ensureInput).Return(sharedworkflow.EnsureServiceResult{}, errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkServiceResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(CreateServiceWorkflow, sharedworkflow.CreateServiceInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkServiceResultActivityName, mock.Anything, failedInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.MarkServiceResultActivityName, mock.Anything, succeededInput)
	})
	testingT.Run("リトライしないエラー(ポートの JSON が壊れているなど)なら、すぐ failed にして、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newServiceWorkflowEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.EnsureServiceActivityName, mock.Anything, ensureInput).
			Return(sharedworkflow.EnsureServiceResult{}, temporal.NewNonRetryableApplicationError("bad", "InvalidService", nil))
		testEnvironment.OnActivity(sharedworkflow.MarkServiceResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(CreateServiceWorkflow, sharedworkflow.CreateServiceInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.EnsureServiceActivityName, 1)
	})
	testingT.Run("結果の記録に失敗し続けたら、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newServiceWorkflowEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.EnsureServiceActivityName, mock.Anything, ensureInput).Return(created, nil)
		testEnvironment.OnActivity(sharedworkflow.MarkServiceResultActivityName, mock.Anything, succeededInput).Return(errors.New("database down"))

		testEnvironment.ExecuteWorkflow(CreateServiceWorkflow, sharedworkflow.CreateServiceInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
	})
}

// TestDeleteServiceWorkflow は、k8s の Service、レコードの順に削除し、失敗したら failed にして失敗で終わることを確認する。
func TestDeleteServiceWorkflow(testingT *testing.T) {
	objectInput := sharedworkflow.DeleteServiceObjectInput{ServiceID: "service-1"}
	recordInput := sharedworkflow.DeleteServiceRecordInput{ServiceID: "service-1"}
	failedInput := sharedworkflow.MarkServiceDeleteFailedInput{ServiceID: "service-1"}

	testingT.Run("成功すると、k8s の Service、レコードの順に削除して、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newServiceWorkflowEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteServiceObjectActivityName, mock.Anything, objectInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteServiceRecordActivityName, mock.Anything, recordInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteServiceWorkflow, sharedworkflow.DeleteServiceInput{ServiceID: "service-1"})

		if !testEnvironment.IsWorkflowCompleted() || testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.MarkServiceDeleteFailedActivityName, mock.Anything, failedInput)
	})
	testingT.Run("k8s の Service の削除が、一時的に失敗しても、リトライで回復すれば、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newServiceWorkflowEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteServiceObjectActivityName, mock.Anything, objectInput).Return(errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.DeleteServiceObjectActivityName, mock.Anything, objectInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteServiceRecordActivityName, mock.Anything, recordInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteServiceWorkflow, sharedworkflow.DeleteServiceInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("リトライで回復して成功するはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.DeleteServiceObjectActivityName, 3)
	})
	testingT.Run("k8s の Service の削除が、期限まで失敗し続けると、レコードは消さず、failed にして、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newServiceWorkflowEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteServiceObjectActivityName, mock.Anything, objectInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkServiceDeleteFailedActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteServiceWorkflow, sharedworkflow.DeleteServiceInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.DeleteServiceRecordActivityName, mock.Anything, recordInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkServiceDeleteFailedActivityName, mock.Anything, failedInput)
	})
	testingT.Run("リトライしないエラーなら、すぐ failed にして、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newServiceWorkflowEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteServiceObjectActivityName, mock.Anything, objectInput).
			Return(temporal.NewNonRetryableApplicationError("bad name", "InvalidServiceName", nil))
		testEnvironment.OnActivity(sharedworkflow.MarkServiceDeleteFailedActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteServiceWorkflow, sharedworkflow.DeleteServiceInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.DeleteServiceObjectActivityName, 1)
	})
	testingT.Run("レコードの削除が失敗し続けても、failed にして、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newServiceWorkflowEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteServiceObjectActivityName, mock.Anything, objectInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteServiceRecordActivityName, mock.Anything, recordInput).Return(errors.New("database down"))
		testEnvironment.OnActivity(sharedworkflow.MarkServiceDeleteFailedActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteServiceWorkflow, sharedworkflow.DeleteServiceInput{ServiceID: "service-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkServiceDeleteFailedActivityName, mock.Anything, failedInput)
	})
}
