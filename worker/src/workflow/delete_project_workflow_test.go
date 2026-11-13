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

// newDeleteProjectEnvironment は、Workflow のテスト環境を作り、2つの Activity を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。Activity は、呼び出し側が OnActivity で振る舞いを決める。
func newDeleteProjectEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る。
	testSuite := &testsuite.WorkflowTestSuite{}
	testEnvironment := testSuite.NewTestWorkflowEnvironment()
	// Activity は、名前だけを登録する(中身はテストが差し替える)。
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteProjectNamespaceInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.DeleteProjectNamespaceActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteProjectRecordsInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.DeleteProjectRecordsActivityName})
	return testEnvironment
}

// TestDeleteProjectWorkflow は、namespace、レコードの順に Activity を呼び、
// 途中で失敗したら、それ以降は呼ばずに失敗で終わることを確認する。
func TestDeleteProjectWorkflow(testingT *testing.T) {
	// 正常に終わる場合と、各 Activity が失敗する場合。
	testCases := []struct {
		name                string
		namespaceError      error
		recordsError        error
		expectWorkflowError bool
		expectRecordsCalled bool
	}{
		{name: "両方成功すると、成功で終わる", expectRecordsCalled: true},
		{name: "namespace の削除が失敗すると、レコードは消さずに失敗で終わる", namespaceError: errors.New("k8s down"), expectWorkflowError: true},
		{name: "レコードの削除が失敗すると、失敗で終わる", recordsError: errors.New("db down"), expectWorkflowError: true, expectRecordsCalled: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			testEnvironment := newDeleteProjectEnvironment(subTest)
			// 各 Activity の結果を決める。
			testEnvironment.OnActivity(sharedworkflow.DeleteProjectNamespaceActivityName, mock.Anything, sharedworkflow.DeleteProjectNamespaceInput{ProjectID: "project-1"}).
				Return(testCase.namespaceError)
			testEnvironment.OnActivity(sharedworkflow.DeleteProjectRecordsActivityName, mock.Anything, sharedworkflow.DeleteProjectRecordsInput{ProjectID: "project-1"}).
				Return(testCase.recordsError)

			// Workflow を実行する。
			testEnvironment.ExecuteWorkflow(DeleteProjectWorkflow, sharedworkflow.DeleteProjectInput{ProjectID: "project-1"})

			// 完了していて、失敗の有無が期待どおりであること。
			if !testEnvironment.IsWorkflowCompleted() {
				subTest.Fatalf("Workflow が完了していない")
			}
			if (testEnvironment.GetWorkflowError() != nil) != testCase.expectWorkflowError {
				subTest.Errorf("失敗の有無が違う: 期待 %v、実際 %v", testCase.expectWorkflowError, testEnvironment.GetWorkflowError())
			}
			// レコードの Activity が呼ばれたかどうかが期待どおりであること。
			recordsInput := sharedworkflow.DeleteProjectRecordsInput{ProjectID: "project-1"}
			if testCase.expectRecordsCalled {
				testEnvironment.AssertCalled(subTest, sharedworkflow.DeleteProjectRecordsActivityName, mock.Anything, recordsInput)
			} else {
				testEnvironment.AssertNotCalled(subTest, sharedworkflow.DeleteProjectRecordsActivityName, mock.Anything, recordsInput)
			}
		})
	}
}
