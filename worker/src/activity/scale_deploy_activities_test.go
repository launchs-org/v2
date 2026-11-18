package activity

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// fakeScaleStore は、テスト用の ScaleStore。決めた deploy を返し、状態の更新を覚える。
type fakeScaleStore struct {
	deploy  model.Deploy
	updates []string
}

// FindForWorker は、決めた deploy を返す。
func (fakeStore *fakeScaleStore) FindForWorker(_ context.Context, _ string) (model.Deploy, error) {
	return fakeStore.deploy, nil
}

// FindSpec は、固定のスペックを返す。
func (fakeStore *fakeScaleStore) FindSpec(_ context.Context, specName string) (model.DeploySpec, error) {
	return model.DeploySpec{Name: specName, CPUMillicores: 250, MemoryMiB: 256}, nil
}

// CompleteScale は、モデルで確定した状態の変更を覚えて、成功する。
func (fakeStore *fakeScaleStore) CompleteScale(_ context.Context, _ string, succeeded bool) (bool, error) {
	completed := fakeStore.deploy
	completed.Status = model.DeployStatusApplying
	completed.CompleteScale(succeeded)
	fakeStore.updates = append(fakeStore.updates, model.DeployStatusApplying+"->"+completed.Status)
	return true, nil
}

// TestApplyScale は、現在の値(新しい replicas、pending ではない起動設定)で Deployment を更新し、
// 内容が壊れているときはリトライしないエラーにすることを確認する。
func TestApplyScale(testingT *testing.T) {
	testingT.Run("現在の値で更新する(pending は使わない)", func(subTest *testing.T) {
		deploy := newPendingConfigDeploy()
		deploy.Replicas = 5
		fakeOperator := &fakeDeploymentOperator{}
		activities := NewScaleDeployActivities(&fakeScaleStore{deploy: deploy}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyScale)

		_, executeError := testEnvironment.ExecuteActivity(activities.ApplyScale, workflow.ApplyScaleInput{DeployID: "deploy-1"})

		if executeError != nil || fakeOperator.appliedDefinition == nil {
			subTest.Fatalf("成功するはず: %v", executeError)
		}
		if fakeOperator.appliedDefinition.Replicas != 5 || fakeOperator.appliedDefinition.Env["PORT"] != "8080" {
			subTest.Errorf("replicas は新しい値、起動設定は現在の値のはず: %+v", fakeOperator.appliedDefinition)
		}
	})
	testingT.Run("replicas が 0 でも更新する", func(subTest *testing.T) {
		deploy := newTestDeploy()
		deploy.Replicas = 0
		fakeOperator := &fakeDeploymentOperator{}
		activities := NewScaleDeployActivities(&fakeScaleStore{deploy: deploy}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyScale)

		if _, executeError := testEnvironment.ExecuteActivity(activities.ApplyScale, workflow.ApplyScaleInput{DeployID: "deploy-1"}); executeError != nil {
			subTest.Fatalf("成功するはず: %v", executeError)
		}
		if fakeOperator.appliedDefinition == nil || fakeOperator.appliedDefinition.Replicas != 0 {
			subTest.Errorf("replicas 0 のはず: %+v", fakeOperator.appliedDefinition)
		}
	})
	testingT.Run("環境変数の JSON が壊れていたら、リトライしないエラー", func(subTest *testing.T) {
		deploy := newTestDeploy()
		deploy.Env = []byte(`[1]`)
		fakeOperator := &fakeDeploymentOperator{}
		activities := NewScaleDeployActivities(&fakeScaleStore{deploy: deploy}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyScale)

		_, executeError := testEnvironment.ExecuteActivity(activities.ApplyScale, workflow.ApplyScaleInput{DeployID: "deploy-1"})

		var applicationError *temporal.ApplicationError
		if !errors.As(executeError, &applicationError) || !applicationError.NonRetryable() || fakeOperator.appliedDefinition != nil {
			subTest.Errorf("リトライしないエラーで、何も更新しないはず: %v", executeError)
		}
	})
}

// TestMarkScaleResult は、成否と replicas から、applying の deploy の状態が決まることを確認する。
func TestMarkScaleResult(testingT *testing.T) {
	// 結果ごとの期待。
	testCases := []struct {
		name            string
		succeeded       bool
		replicas        int
		expectedUpdates string
	}{
		{name: "成功なら active", succeeded: true, replicas: 2, expectedUpdates: "applying->active"},
		{name: "成功で replicas が 0 なら scaled_zero", succeeded: true, replicas: 0, expectedUpdates: "applying->scaled_zero"},
		{name: "失敗なら failed", succeeded: false, replicas: 2, expectedUpdates: "applying->failed"},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			deploy := newTestDeploy()
			deploy.Replicas = testCase.replicas
			fakeStore := &fakeScaleStore{deploy: deploy}
			activities := NewScaleDeployActivities(fakeStore, &fakeDeploymentOperator{}, newTestActivities(nil, nil).appLogger)
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.MarkScaleResult)

			_, executeError := testEnvironment.ExecuteActivity(activities.MarkScaleResult, workflow.MarkScaleResultInput{DeployID: "deploy-1", Succeeded: testCase.succeeded})

			if executeError != nil || len(fakeStore.updates) != 1 || fakeStore.updates[0] != testCase.expectedUpdates {
				subTest.Errorf("%s のはず: %v %v", testCase.expectedUpdates, executeError, fakeStore.updates)
			}
		})
	}
}
