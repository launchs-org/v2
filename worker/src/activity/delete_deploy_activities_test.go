package activity

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/workflow"
)

// fakeDeletionStore は、テスト用の DeployDeletionStore。呼び出しを覚え、決めたエラーを返す。
type fakeDeletionStore struct {
	deploy      model.Deploy
	findError   error
	recordCalls []string
	failedCalls []string
	recordError error
}

// FindForWorker は、決めた deploy とエラーを返す。
func (fakeStore *fakeDeletionStore) FindForWorker(_ context.Context, _ string) (model.Deploy, error) {
	return fakeStore.deploy, fakeStore.findError
}

// DeleteRecord は、ID を覚えて、決めたエラーを返す。
func (fakeStore *fakeDeletionStore) DeleteRecord(_ context.Context, deployID string) error {
	fakeStore.recordCalls = append(fakeStore.recordCalls, deployID)
	return fakeStore.recordError
}

// MarkDeleteFailed は、ID を覚えて成功する。
func (fakeStore *fakeDeletionStore) MarkDeleteFailed(_ context.Context, deployID string) error {
	fakeStore.failedCalls = append(fakeStore.failedCalls, deployID)
	return nil
}

// TestDeleteDeploymentActivity は、プロジェクトの namespace と deploy の名前で削除を頼み、
// 行が無ければ成功、名前の不正はリトライしないエラーにすることを確認する。
func TestDeleteDeploymentActivity(testingT *testing.T) {
	testingT.Run("プロジェクトの ID と deploy の ID で、Deployment の削除を頼む", func(subTest *testing.T) {
		fakeOperator := &fakeDeploymentOperator{}
		activities := NewDeleteDeployActivities(&fakeDeletionStore{deploy: newTestDeploy()}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteDeployment)

		_, executeError := testEnvironment.ExecuteActivity(activities.DeleteDeployment, workflow.DeleteDeploymentInput{DeployID: "deploy-1"})

		if executeError != nil || len(fakeOperator.deletedDeployments) != 1 || fakeOperator.deletedDeployments[0] != "project-1/deploy-1" {
			subTest.Errorf("project-1/deploy-1 の削除を頼むはず: %v %v", executeError, fakeOperator.deletedDeployments)
		}
	})
	testingT.Run("deploy の行がすでに無ければ、何もせず成功する", func(subTest *testing.T) {
		fakeOperator := &fakeDeploymentOperator{}
		notFound := apperror.New(apperror.KindNotFound, "deploy が見つかりません")
		activities := NewDeleteDeployActivities(&fakeDeletionStore{findError: notFound}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteDeployment)

		_, executeError := testEnvironment.ExecuteActivity(activities.DeleteDeployment, workflow.DeleteDeploymentInput{DeployID: "deploy-1"})

		if executeError != nil || len(fakeOperator.deletedDeployments) != 0 {
			subTest.Errorf("成功で、何も頼まないはず: %v %v", executeError, fakeOperator.deletedDeployments)
		}
	})
	testingT.Run("DB のエラーは、リトライできるエラーとして返す", func(subTest *testing.T) {
		activities := NewDeleteDeployActivities(&fakeDeletionStore{findError: errors.New("database down")}, &fakeDeploymentOperator{}, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteDeployment)

		_, executeError := testEnvironment.ExecuteActivity(activities.DeleteDeployment, workflow.DeleteDeploymentInput{DeployID: "deploy-1"})

		var applicationError *temporal.ApplicationError
		if executeError == nil || (errors.As(executeError, &applicationError) && applicationError.NonRetryable()) {
			subTest.Errorf("リトライできるエラーのはず: %v", executeError)
		}
	})
	testingT.Run("名前の形が不正なら、リトライしないエラー", func(subTest *testing.T) {
		fakeOperator := &fakeDeploymentOperator{deleteError: ErrInvalidDeploymentName}
		activities := NewDeleteDeployActivities(&fakeDeletionStore{deploy: newTestDeploy()}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteDeployment)

		_, executeError := testEnvironment.ExecuteActivity(activities.DeleteDeployment, workflow.DeleteDeploymentInput{DeployID: "deploy-1"})

		var applicationError *temporal.ApplicationError
		if !errors.As(executeError, &applicationError) || !applicationError.NonRetryable() {
			subTest.Errorf("リトライしないエラーのはず: %v", executeError)
		}
	})
	testingT.Run("k8s への依頼の失敗は、リトライできるエラー", func(subTest *testing.T) {
		fakeOperator := &fakeDeploymentOperator{deleteError: errors.New("k8s down")}
		activities := NewDeleteDeployActivities(&fakeDeletionStore{deploy: newTestDeploy()}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteDeployment)

		_, executeError := testEnvironment.ExecuteActivity(activities.DeleteDeployment, workflow.DeleteDeploymentInput{DeployID: "deploy-1"})

		var applicationError *temporal.ApplicationError
		if executeError == nil || (errors.As(executeError, &applicationError) && applicationError.NonRetryable()) {
			subTest.Errorf("リトライできるエラーのはず: %v", executeError)
		}
	})
}

// TestDeleteDeployRecordAndMarkFailedActivities は、レコードの削除と失敗の記録が、保存先にそのまま渡されることを確認する。
func TestDeleteDeployRecordAndMarkFailedActivities(testingT *testing.T) {
	testingT.Run("レコードの削除を保存先に渡し、そのエラーを返す", func(subTest *testing.T) {
		fakeStore := &fakeDeletionStore{recordError: errors.New("database down")}
		activities := NewDeleteDeployActivities(fakeStore, &fakeDeploymentOperator{}, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteDeployRecord)

		_, executeError := testEnvironment.ExecuteActivity(activities.DeleteDeployRecord, workflow.DeleteDeployRecordInput{DeployID: "deploy-1"})

		if executeError == nil || len(fakeStore.recordCalls) != 1 || fakeStore.recordCalls[0] != "deploy-1" {
			subTest.Errorf("エラーを返し、deploy-1 を渡すはず: %v %v", executeError, fakeStore.recordCalls)
		}
	})
	testingT.Run("失敗の記録を保存先に渡す", func(subTest *testing.T) {
		fakeStore := &fakeDeletionStore{}
		activities := NewDeleteDeployActivities(fakeStore, &fakeDeploymentOperator{}, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.MarkDeployDeleteFailed)

		_, executeError := testEnvironment.ExecuteActivity(activities.MarkDeployDeleteFailed, workflow.MarkDeployDeleteFailedInput{DeployID: "deploy-1"})

		if executeError != nil || len(fakeStore.failedCalls) != 1 || fakeStore.failedCalls[0] != "deploy-1" {
			subTest.Errorf("成功し、deploy-1 を渡すはず: %v %v", executeError, fakeStore.failedCalls)
		}
	})
}
