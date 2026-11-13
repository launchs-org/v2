package activity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"launchs/shared/logger"
	"launchs/shared/workflow"
)

// fakeNamespaceDeleter は、テスト用の NamespaceDeleter。受け取った名前を覚え、決めたエラーを返す。
type fakeNamespaceDeleter struct {
	deletedNames []string
	deleteError  error
	// remainingChecks は、NamespaceExists が「ある」を返す残りの回数。0 になると「無い」を返す。
	remainingChecks int
	existsError     error
	existsCalls     int
}

// DeleteNamespace は、名前を覚えて、決めたエラーを返す。
func (fakeDeleter *fakeNamespaceDeleter) DeleteNamespace(_ context.Context, namespaceName string) error {
	fakeDeleter.deletedNames = append(fakeDeleter.deletedNames, namespaceName)
	return fakeDeleter.deleteError
}

// NamespaceExists は、決めた回数だけ「ある」を返し、そのあとは「無い」を返す。
func (fakeDeleter *fakeNamespaceDeleter) NamespaceExists(_ context.Context, _ string) (bool, error) {
	fakeDeleter.existsCalls++
	if fakeDeleter.existsError != nil {
		return false, fakeDeleter.existsError
	}
	if fakeDeleter.remainingChecks > 0 {
		fakeDeleter.remainingChecks--
		return true, nil
	}
	return false, nil
}

// fakeProjectRecordDeleter は、テスト用の ProjectRecordDeleter。受け取った ID を覚え、決めたエラーを返す。
type fakeProjectRecordDeleter struct {
	deletedIDs  []string
	deleteError error
	failedIDs   []string
	failError   error
}

// DeleteRecord は、ID を覚えて、決めたエラーを返す。
func (fakeDeleter *fakeProjectRecordDeleter) DeleteRecord(_ context.Context, projectID string) error {
	fakeDeleter.deletedIDs = append(fakeDeleter.deletedIDs, projectID)
	return fakeDeleter.deleteError
}

// MarkDeleteFailed は、ID を覚えて、決めたエラーを返す。
func (fakeDeleter *fakeProjectRecordDeleter) MarkDeleteFailed(_ context.Context, projectID string) error {
	fakeDeleter.failedIDs = append(fakeDeleter.failedIDs, projectID)
	return fakeDeleter.failError
}

// TestMarkProjectDeleteFailed は、projectId で削除失敗の記録を頼み、そのエラーを返すことを確認する。
func TestMarkProjectDeleteFailed(testingT *testing.T) {
	// 成功する場合と、失敗する場合。
	testCases := []struct {
		name      string
		failError error
	}{
		{name: "projectId で削除失敗の記録を頼む"},
		{name: "記録のエラーはそのまま返す", failError: errors.New("db down")},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// Activity をテスト環境で実行する。
			fakeDeleter := &fakeProjectRecordDeleter{failError: testCase.failError}
			activities := NewProjectDeletionActivities(&fakeNamespaceDeleter{}, fakeDeleter, logger.New(&bytes.Buffer{}, "worker", slog.LevelInfo))
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.MarkProjectDeleteFailed)
			_, executeError := testEnvironment.ExecuteActivity(activities.MarkProjectDeleteFailed, workflow.MarkProjectDeleteFailedInput{ProjectID: "project-1"})

			// 渡した ID と、エラーの有無を確認する。
			if len(fakeDeleter.failedIDs) != 1 || fakeDeleter.failedIDs[0] != "project-1" {
				subTest.Errorf("渡した ID が違う: %v", fakeDeleter.failedIDs)
			}
			if (executeError != nil) != (testCase.failError != nil) {
				subTest.Errorf("エラーの有無が違う: %v", executeError)
			}
		})
	}
}

// TestDeleteProjectNamespace は、namespace の名前に projectId を渡して削除を頼み、
// 消えるまで確かめ続けて待ち、途中のエラーを返すことを確認する。
func TestDeleteProjectNamespace(testingT *testing.T) {
	// すぐ消える・消えるまで待つ・削除や確認に失敗する場合。
	testCases := []struct {
		name                string
		deleter             *fakeNamespaceDeleter
		expectError         bool
		expectedExistsCalls int
	}{
		{name: "projectId を namespace 名として削除を頼み、すぐ消えれば終わる", deleter: &fakeNamespaceDeleter{}, expectedExistsCalls: 1},
		{name: "消えるまで、確認を繰り返して待つ", deleter: &fakeNamespaceDeleter{remainingChecks: 3}, expectedExistsCalls: 4},
		{name: "削除のエラーはそのまま返し、待たない", deleter: &fakeNamespaceDeleter{deleteError: errors.New("k8s down")}, expectError: true},
		{name: "確認のエラーはそのまま返す", deleter: &fakeNamespaceDeleter{existsError: errors.New("k8s down")}, expectError: true, expectedExistsCalls: 1},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// Activity をテスト環境で実行する(待ち時間は、テストが遅くならないよう短くする)。
			activities := NewProjectDeletionActivities(testCase.deleter, &fakeProjectRecordDeleter{}, logger.New(&bytes.Buffer{}, "worker", slog.LevelInfo))
			activities.namespacePollInterval = time.Millisecond
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.DeleteProjectNamespace)
			_, executeError := testEnvironment.ExecuteActivity(activities.DeleteProjectNamespace, workflow.DeleteProjectNamespaceInput{ProjectID: "project-1"})

			// 渡した名前、確認の回数、エラーの有無を確認する。
			if len(testCase.deleter.deletedNames) != 1 || testCase.deleter.deletedNames[0] != "project-1" {
				subTest.Errorf("渡した namespace 名が違う: %v", testCase.deleter.deletedNames)
			}
			if testCase.deleter.existsCalls != testCase.expectedExistsCalls {
				subTest.Errorf("確認の回数が違う: 期待 %d、実際 %d", testCase.expectedExistsCalls, testCase.deleter.existsCalls)
			}
			if (executeError != nil) != testCase.expectError {
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

	// 削除を頼み、残っているかも確かめる。
	deleteError := loggingDeleter.DeleteNamespace(context.Background(), "project-1")
	exists, existsError := loggingDeleter.NamespaceExists(context.Background(), "project-1")

	// エラーにならず、警告のログに名前が出ていること。
	if deleteError != nil || existsError != nil || exists {
		testingT.Errorf("エラーにならず、残っていない扱いのはず: %v %v %v", deleteError, existsError, exists)
	}
	if !strings.Contains(logOutput.String(), `"level":"WARN"`) || !strings.Contains(logOutput.String(), "project-1") {
		testingT.Errorf("警告のログに名前が出ていない: %s", logOutput.String())
	}
}

// TestMarkNonRetryable は、名前の形の不正だけがリトライしないエラーになり、
// k8s の一時的な障害などは、そのまま(リトライされる)返ることを確認する。
func TestMarkNonRetryable(testingT *testing.T) {
	// 名前の形が不正なエラーは、リトライしない印が付く。
	testingT.Run("名前の形の不正は、リトライしないエラーになる", func(subTest *testing.T) {
		markedError := markNonRetryable(fmt.Errorf("%w: kube-system", ErrInvalidNamespaceName))
		var applicationError *temporal.ApplicationError
		if !errors.As(markedError, &applicationError) || !applicationError.NonRetryable() {
			subTest.Errorf("リトライしないエラーのはず: %v", markedError)
		}
	})
	// 一時的な障害は、そのまま返る(リトライされる)。
	testingT.Run("一時的な障害は、そのまま返す", func(subTest *testing.T) {
		temporaryError := errors.New("k8s down")
		if markedError := markNonRetryable(temporaryError); !errors.Is(markedError, temporaryError) {
			subTest.Errorf("そのまま返すはず: %v", markedError)
		}
		var applicationError *temporal.ApplicationError
		if errors.As(markNonRetryable(temporaryError), &applicationError) {
			subTest.Errorf("リトライしないエラーにしてはいけない")
		}
	})
}
