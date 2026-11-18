package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	sharedworkflow "launchs/shared/workflow"
)

// newDeleteProjectEnvironment は、Workflow のテスト環境を作り、3つの Activity を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。Activity は、呼び出し側が OnActivity で振る舞いを決める。
func newDeleteProjectEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る(待ち時間は、実際には待たずに、時計を進めて飛ばす)。
	testSuite := &testsuite.WorkflowTestSuite{}
	testEnvironment := testSuite.NewTestWorkflowEnvironment()
	// Activity は、名前だけを登録する(中身はテストが差し替える)。
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteProjectNamespaceInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.DeleteProjectNamespaceActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteProjectRecordsInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.DeleteProjectRecordsActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.MarkProjectDeleteFailedInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.MarkProjectDeleteFailedActivityName})
	return testEnvironment
}

// TestDeleteProjectWorkflow は、namespace、レコードの順に Activity を呼び、成功すれば成功で終わり、
// 失敗が続いてリトライの期限を超えたときは、プロジェクトを delete_failed にして失敗で終わることを確認する。
func TestDeleteProjectWorkflow(testingT *testing.T) {
	// 期待する入力(全ての Activity に、同じ projectId を渡す)。
	namespaceInput := sharedworkflow.DeleteProjectNamespaceInput{ProjectID: "project-1"}
	recordsInput := sharedworkflow.DeleteProjectRecordsInput{ProjectID: "project-1"}
	markInput := sharedworkflow.MarkProjectDeleteFailedInput{ProjectID: "project-1"}

	// 両方成功する場合。
	testingT.Run("両方成功すると、成功で終わり、delete_failed にはしない", func(subTest *testing.T) {
		testEnvironment := newDeleteProjectEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteProjectNamespaceActivityName, mock.Anything, namespaceInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteProjectRecordsActivityName, mock.Anything, recordsInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteProjectWorkflow, sharedworkflow.DeleteProjectInput{ProjectID: "project-1"})

		if !testEnvironment.IsWorkflowCompleted() || testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.MarkProjectDeleteFailedActivityName, mock.Anything, markInput)
	})

	// 一時的な障害は、リトライで回復して、成功で終わる。
	testingT.Run("namespace の削除が一時的に失敗しても、リトライで回復すれば、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newDeleteProjectEnvironment(subTest)
		// 最初の2回は失敗し、3回目から成功する。
		testEnvironment.OnActivity(sharedworkflow.DeleteProjectNamespaceActivityName, mock.Anything, namespaceInput).Return(errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.DeleteProjectNamespaceActivityName, mock.Anything, namespaceInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteProjectRecordsActivityName, mock.Anything, recordsInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteProjectWorkflow, sharedworkflow.DeleteProjectInput{ProjectID: "project-1"})

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("リトライで回復して成功するはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.MarkProjectDeleteFailedActivityName, mock.Anything, markInput)
	})

	// namespace の削除が、期限まで失敗し続ける場合。
	testingT.Run("namespace の削除が期限まで失敗し続けると、レコードは消さず、delete_failed にして失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newDeleteProjectEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteProjectNamespaceActivityName, mock.Anything, namespaceInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkProjectDeleteFailedActivityName, mock.Anything, markInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteProjectWorkflow, sharedworkflow.DeleteProjectInput{ProjectID: "project-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.DeleteProjectRecordsActivityName, mock.Anything, recordsInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkProjectDeleteFailedActivityName, mock.Anything, markInput)
	})

	// レコードの削除が、期限まで失敗し続ける場合。
	testingT.Run("レコードの削除が期限まで失敗し続けると、delete_failed にして失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newDeleteProjectEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteProjectNamespaceActivityName, mock.Anything, namespaceInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteProjectRecordsActivityName, mock.Anything, recordsInput).Return(errors.New("db down"))
		testEnvironment.OnActivity(sharedworkflow.MarkProjectDeleteFailedActivityName, mock.Anything, markInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteProjectWorkflow, sharedworkflow.DeleteProjectInput{ProjectID: "project-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkProjectDeleteFailedActivityName, mock.Anything, markInput)
	})

	// リトライしないエラーは、すぐ失敗にして、delete_failed にする。
	testingT.Run("リトライしないエラーは、1回で失敗にして、delete_failed にする", func(subTest *testing.T) {
		testEnvironment := newDeleteProjectEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteProjectNamespaceActivityName, mock.Anything, namespaceInput).
			Return(temporal.NewNonRetryableApplicationError("名前の形が不正", "InvalidNamespaceName", nil)).Once()
		testEnvironment.OnActivity(sharedworkflow.MarkProjectDeleteFailedActivityName, mock.Anything, markInput).Return(nil)

		testEnvironment.ExecuteWorkflow(DeleteProjectWorkflow, sharedworkflow.DeleteProjectInput{ProjectID: "project-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.DeleteProjectNamespaceActivityName, 1)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkProjectDeleteFailedActivityName, mock.Anything, markInput)
	})

	// delete_failed にもできなかった場合は、両方の原因を返す。
	testingT.Run("delete_failed にもできなかったときは、両方の原因を含めて失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newDeleteProjectEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.DeleteProjectNamespaceActivityName, mock.Anything, namespaceInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkProjectDeleteFailedActivityName, mock.Anything, markInput).Return(errors.New("db down"))

		testEnvironment.ExecuteWorkflow(DeleteProjectWorkflow, sharedworkflow.DeleteProjectInput{ProjectID: "project-1"})

		workflowError := testEnvironment.GetWorkflowError()
		if workflowError == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		if message := workflowError.Error(); !strings.Contains(message, "namespace を削除できませんでした") || !strings.Contains(message, "削除失敗の記録もできませんでした") {
			subTest.Errorf("両方の原因を含むはず: %v", workflowError)
		}
	})
}
