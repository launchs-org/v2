package activity

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"gorm.io/datatypes"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// fakeCrashLoopStore は、テスト用の CrashLoopStore。決めた deploy を返し、呼び出しを覚える。
type fakeCrashLoopStore struct {
	deploy      model.Deploy
	started     bool
	beginError  error
	beginCalls  []string
	beginReason string
	finishCalls []string
	finishError error
}

// FindForWorker は、決めた deploy を返す。
func (fakeStore *fakeCrashLoopStore) FindForWorker(_ context.Context, _ string) (model.Deploy, error) {
	return fakeStore.deploy, nil
}

// FindSpec は、固定のスペックを返す。
func (fakeStore *fakeCrashLoopStore) FindSpec(_ context.Context, specName string) (model.DeploySpec, error) {
	return model.DeploySpec{Name: specName, CPUMillicores: 250, MemoryMiB: 256}, nil
}

// BeginCrashLoopStop は、呼び出しを覚えて、決めた結果を返す。
func (fakeStore *fakeCrashLoopStore) BeginCrashLoopStop(_ context.Context, deployID string, _ string, podName string, reason string, _ string, _ time.Time) (bool, error) {
	fakeStore.beginCalls = append(fakeStore.beginCalls, deployID+"/"+podName)
	fakeStore.beginReason = reason
	return fakeStore.started, fakeStore.beginError
}

// FinishCrashLoopStop は、呼び出しを覚えて、決めたエラーを返す。
func (fakeStore *fakeCrashLoopStore) FinishCrashLoopStop(_ context.Context, deployID string) error {
	fakeStore.finishCalls = append(fakeStore.finishCalls, deployID)
	return fakeStore.finishError
}

// TestCrashLoopActivitiesBeginAndFinish は、止める準備と終了が、保存先にそのまま渡され、結果とエラーが返ることを確認する。
func TestCrashLoopActivitiesBeginAndFinish(testingT *testing.T) {
	stopInput := workflow.StopCrashLoopInput{DeployID: "deploy-1", PodUID: "uid-1", PodName: "pod-1", Reason: "CrashLoopBackOff", Message: "再起動 9 回"}
	testingT.Run("止める準備ができれば Started が true で、理由を保存先に渡す", func(subTest *testing.T) {
		fakeStore := &fakeCrashLoopStore{started: true}
		activities := NewCrashLoopActivities(fakeStore, &fakeDeploymentOperator{}, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.BeginCrashLoopStop)

		encoded, executeError := testEnvironment.ExecuteActivity(activities.BeginCrashLoopStop, workflow.BeginCrashLoopStopInput{Stop: stopInput})

		var result workflow.BeginCrashLoopStopResult
		if executeError != nil || encoded.Get(&result) != nil || !result.Started || len(fakeStore.beginCalls) != 1 || fakeStore.beginCalls[0] != "deploy-1/pod-1" || fakeStore.beginReason != "CrashLoopBackOff" {
			subTest.Errorf("Started が true のはず: %v %+v %v", executeError, result, fakeStore.beginCalls)
		}
	})
	testingT.Run("active でなければ Started が false で、エラーにはならない", func(subTest *testing.T) {
		activities := NewCrashLoopActivities(&fakeCrashLoopStore{started: false}, &fakeDeploymentOperator{}, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.BeginCrashLoopStop)

		encoded, executeError := testEnvironment.ExecuteActivity(activities.BeginCrashLoopStop, workflow.BeginCrashLoopStopInput{Stop: stopInput})

		var result workflow.BeginCrashLoopStopResult
		if executeError != nil || encoded.Get(&result) != nil || result.Started {
			subTest.Errorf("Started が false で、エラーにならないはず: %v %+v", executeError, result)
		}
	})
	testingT.Run("保存先のエラーは、返す(リトライされる)", func(subTest *testing.T) {
		activities := NewCrashLoopActivities(&fakeCrashLoopStore{beginError: errors.New("database down")}, &fakeDeploymentOperator{}, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.BeginCrashLoopStop)
		if _, executeError := testEnvironment.ExecuteActivity(activities.BeginCrashLoopStop, workflow.BeginCrashLoopStopInput{Stop: stopInput}); executeError == nil {
			subTest.Errorf("エラーのはず")
		}
	})
	testingT.Run("終了は、deploy の ID を保存先に渡し、エラーを返す", func(subTest *testing.T) {
		fakeStore := &fakeCrashLoopStore{finishError: errors.New("database down")}
		activities := NewCrashLoopActivities(fakeStore, &fakeDeploymentOperator{}, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.FinishCrashLoopStop)
		_, executeError := testEnvironment.ExecuteActivity(activities.FinishCrashLoopStop, workflow.FinishCrashLoopStopInput{DeployID: "deploy-1"})
		if executeError == nil || len(fakeStore.finishCalls) != 1 || fakeStore.finishCalls[0] != "deploy-1" {
			subTest.Errorf("エラーを返し、deploy-1 を渡すはず: %v %v", executeError, fakeStore.finishCalls)
		}
	})
}

// TestCrashLoopActivitiesScaleToZero は、レプリカだけを 0 にして、他は現在の値(起動設定の pending は使わない)で、
// Deployment を更新することを確認する。
func TestCrashLoopActivitiesScaleToZero(testingT *testing.T) {
	testingT.Run("replicas が 3 でも、k8s には 0 を渡し、起動設定は現在の値(pending ではない)を使う", func(subTest *testing.T) {
		deploy := newPendingConfigDeploy()
		deploy.Replicas = 3
		fakeOperator := &fakeDeploymentOperator{}
		activities := NewCrashLoopActivities(&fakeCrashLoopStore{deploy: deploy}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ScaleToZero)

		_, executeError := testEnvironment.ExecuteActivity(activities.ScaleToZero, workflow.ScaleToZeroInput{DeployID: "deploy-1"})

		definition := fakeOperator.appliedDefinition
		if executeError != nil || definition == nil || definition.Replicas != 0 || definition.Env["PORT"] != "8080" || definition.Image != "registry/app:1" {
			subTest.Errorf("replicas 0 で、現在の起動設定のはず: %v %+v", executeError, definition)
		}
	})
	testingT.Run("k8s への依頼の失敗は、リトライできるエラー", func(subTest *testing.T) {
		fakeOperator := &fakeDeploymentOperator{applyError: errors.New("k8s down")}
		activities := NewCrashLoopActivities(&fakeCrashLoopStore{deploy: newTestDeploy()}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ScaleToZero)
		_, executeError := testEnvironment.ExecuteActivity(activities.ScaleToZero, workflow.ScaleToZeroInput{DeployID: "deploy-1"})
		var applicationError *temporal.ApplicationError
		if executeError == nil || (errors.As(executeError, &applicationError) && applicationError.NonRetryable()) {
			subTest.Errorf("リトライできるエラーのはず: %v", executeError)
		}
	})
	testingT.Run("内容が壊れていれば、リトライしないエラーで、何も更新しない", func(subTest *testing.T) {
		deploy := newTestDeploy()
		deploy.Env = datatypes.JSON(`[1]`)
		fakeOperator := &fakeDeploymentOperator{}
		activities := NewCrashLoopActivities(&fakeCrashLoopStore{deploy: deploy}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ScaleToZero)
		_, executeError := testEnvironment.ExecuteActivity(activities.ScaleToZero, workflow.ScaleToZeroInput{DeployID: "deploy-1"})
		var applicationError *temporal.ApplicationError
		if !errors.As(executeError, &applicationError) || !applicationError.NonRetryable() || fakeOperator.appliedDefinition != nil {
			subTest.Errorf("リトライしないエラーで、何も更新しないはず: %v", executeError)
		}
	})
}
