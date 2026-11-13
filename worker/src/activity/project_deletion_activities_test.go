package activity

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"go.temporal.io/sdk/testsuite"

	"launchs/shared/logger"
	"launchs/shared/workflow"
)

// fakeNamespaceDeleter は、テスト用の NamespaceDeleter。受け取った名前を覚え、決めたエラーを返す。
type fakeNamespaceDeleter struct {
	deletedNames []string
	deleteError  error
}

// DeleteNamespace は、名前を覚えて、決めたエラーを返す。
func (fakeDeleter *fakeNamespaceDeleter) DeleteNamespace(_ context.Context, namespaceName string) error {
	fakeDeleter.deletedNames = append(fakeDeleter.deletedNames, namespaceName)
	return fakeDeleter.deleteError
}

// fakeProjectRecordDeleter は、テスト用の ProjectRecordDeleter。受け取った ID を覚え、決めたエラーを返す。
type fakeProjectRecordDeleter struct {
	deletedIDs  []string
	deleteError error
}

// DeleteRecord は、ID を覚えて、決めたエラーを返す。
func (fakeDeleter *fakeProjectRecordDeleter) DeleteRecord(_ context.Context, projectID string) error {
	fakeDeleter.deletedIDs = append(fakeDeleter.deletedIDs, projectID)
	return fakeDeleter.deleteError
}

// TestDeleteProjectNamespace は、namespace の名前に projectId を渡して削除を頼み、そのエラーを返すことを確認する。
func TestDeleteProjectNamespace(testingT *testing.T) {
	// 成功する場合と、失敗する場合。
	testCases := []struct {
		name        string
		deleteError error
	}{
		{name: "projectId を namespace 名として削除を頼む"},
		{name: "削除のエラーはそのまま返す", deleteError: errors.New("k8s down")},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// Activity をテスト環境で実行する。
			fakeDeleter := &fakeNamespaceDeleter{deleteError: testCase.deleteError}
			activities := NewProjectDeletionActivities(fakeDeleter, &fakeProjectRecordDeleter{}, logger.New(&bytes.Buffer{}, "worker", slog.LevelInfo))
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.DeleteProjectNamespace)
			_, executeError := testEnvironment.ExecuteActivity(activities.DeleteProjectNamespace, workflow.DeleteProjectNamespaceInput{ProjectID: "project-1"})

			// 渡した名前と、エラーの有無を確認する。
			if len(fakeDeleter.deletedNames) != 1 || fakeDeleter.deletedNames[0] != "project-1" {
				subTest.Errorf("渡した namespace 名が違う: %v", fakeDeleter.deletedNames)
			}
			if (executeError != nil) != (testCase.deleteError != nil) {
				subTest.Errorf("エラーの有無が違う: %v", executeError)
			}
		})
	}
}

// TestDeleteProjectRecords は、projectId でレコードの削除を頼み、そのエラーを返すことを確認する。
func TestDeleteProjectRecords(testingT *testing.T) {
	// 成功する場合と、失敗する場合。
	testCases := []struct {
		name        string
		deleteError error
	}{
		{name: "projectId でレコードの削除を頼む"},
		{name: "削除のエラーはそのまま返す", deleteError: errors.New("db down")},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// Activity をテスト環境で実行する。
			fakeDeleter := &fakeProjectRecordDeleter{deleteError: testCase.deleteError}
			activities := NewProjectDeletionActivities(&fakeNamespaceDeleter{}, fakeDeleter, logger.New(&bytes.Buffer{}, "worker", slog.LevelInfo))
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.DeleteProjectRecords)
			_, executeError := testEnvironment.ExecuteActivity(activities.DeleteProjectRecords, workflow.DeleteProjectRecordsInput{ProjectID: "project-1"})

			// 渡した ID と、エラーの有無を確認する。
			if len(fakeDeleter.deletedIDs) != 1 || fakeDeleter.deletedIDs[0] != "project-1" {
				subTest.Errorf("渡した ID が違う: %v", fakeDeleter.deletedIDs)
			}
			if (executeError != nil) != (testCase.deleteError != nil) {
				subTest.Errorf("エラーの有無が違う: %v", executeError)
			}
		})
	}
}

// TestLoggingNamespaceDeleter は、仮の実装が、何も削除せず、警告のログに namespace 名を出すことを確認する。
func TestLoggingNamespaceDeleter(testingT *testing.T) {
	// ログの出力先を、テストで読める入れ物にする。
	var logOutput bytes.Buffer
	loggingDeleter := NewLoggingNamespaceDeleter(logger.New(&logOutput, "worker", slog.LevelInfo))

	// 削除を頼む。
	deleteError := loggingDeleter.DeleteNamespace(context.Background(), "project-1")

	// エラーにならず、警告のログに名前が出ていること。
	if deleteError != nil {
		testingT.Errorf("エラーにならないはず: %v", deleteError)
	}
	if !strings.Contains(logOutput.String(), `"level":"WARN"`) || !strings.Contains(logOutput.String(), "project-1") {
		testingT.Errorf("警告のログに名前が出ていない: %s", logOutput.String())
	}
}
