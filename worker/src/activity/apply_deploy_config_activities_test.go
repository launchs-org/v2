package activity

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"gorm.io/datatypes"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// fakeConfigApplyStore は、テスト用の ConfigApplyStore。決めた deploy を返し、確定の呼び出しを覚える。
type fakeConfigApplyStore struct {
	deploy      model.Deploy
	commitCalls []bool
	commitError error
	findError   error
	specError   error
}

// FindForWorker は、決めた deploy を返す。
func (fakeStore *fakeConfigApplyStore) FindForWorker(_ context.Context, _ string) (model.Deploy, error) {
	return fakeStore.deploy, fakeStore.findError
}

// FindSpec は、固定のスペックを返す。
func (fakeStore *fakeConfigApplyStore) FindSpec(_ context.Context, specName string) (model.DeploySpec, error) {
	return model.DeploySpec{Name: specName, CPUMillicores: 250, MemoryMiB: 256}, fakeStore.specError
}

// CommitConfigResult は、成否を覚えて、決めたエラーを返す。
func (fakeStore *fakeConfigApplyStore) CommitConfigResult(_ context.Context, _ string, succeeded bool) (bool, error) {
	fakeStore.commitCalls = append(fakeStore.commitCalls, succeeded)
	return true, fakeStore.commitError
}

// newPendingConfigDeploy は、現在の値と pending の値が違う deploy を作る。
//
// 戻り値:
//   - 現在は PORT=8080、pending は PORT=9090 と起動コマンド・引数が入った deploy。
func newPendingConfigDeploy() model.Deploy {
	deploy := newTestDeploy()
	pendingCommand := "/app/new"
	deploy.PendingEnv = datatypes.JSON(`{"PORT":"9090"}`)
	deploy.PendingCommand = &pendingCommand
	deploy.PendingArgs = datatypes.JSON(`["--new"]`)
	return deploy
}

// TestApplyPendingConfig は、k8s に渡す内容が、現在の値ではなく pending の値になることと、
// 適用する値が無い・壊れているときはリトライしないエラーになることを確認する。
func TestApplyPendingConfig(testingT *testing.T) {
	// pending の値が、Deployment の内容に使われる。
	testingT.Run("pending の値で Deployment の内容を組み立てて渡す(スペックなどは維持)", func(subTest *testing.T) {
		fakeOperator := &fakeDeploymentOperator{}
		activities := NewApplyDeployConfigActivities(&fakeConfigApplyStore{deploy: newPendingConfigDeploy()}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyPendingConfig)

		_, executeError := testEnvironment.ExecuteActivity(activities.ApplyPendingConfig, workflow.ApplyPendingConfigInput{DeployID: "deploy-1"})

		if executeError != nil || fakeOperator.appliedDefinition == nil {
			subTest.Fatalf("成功するはず: %v", executeError)
		}
		definition := fakeOperator.appliedDefinition
		if definition.Env["PORT"] != "9090" || definition.Command == nil || *definition.Command != "/app/new" ||
			len(definition.Args) != 1 || definition.Args[0] != "--new" {
			subTest.Errorf("pending の値のはず: %+v", definition)
		}
		if definition.Image != "registry/app:1" || definition.Replicas != 2 || definition.CPUMillicores != 250 {
			subTest.Errorf("起動設定以外は変わらないはず: %+v", definition)
		}
	})
	// pending の command と args が NULL なら、イメージの既定に戻す(現在の値を引き継がない)。
	testingT.Run("pending の command と args が NULL なら、nil で渡す", func(subTest *testing.T) {
		deploy := newPendingConfigDeploy()
		deploy.PendingCommand, deploy.PendingArgs = nil, nil
		fakeOperator := &fakeDeploymentOperator{}
		activities := NewApplyDeployConfigActivities(&fakeConfigApplyStore{deploy: deploy}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyPendingConfig)

		if _, executeError := testEnvironment.ExecuteActivity(activities.ApplyPendingConfig, workflow.ApplyPendingConfigInput{DeployID: "deploy-1"}); executeError != nil {
			subTest.Fatalf("成功するはず: %v", executeError)
		}
		if fakeOperator.appliedDefinition.Command != nil || fakeOperator.appliedDefinition.Args != nil {
			subTest.Errorf("nil のはず: %+v", fakeOperator.appliedDefinition)
		}
	})
	// リトライしても直らないものは、リトライしない。
	for _, brokenCase := range []struct {
		name   string
		modify func(deploy *model.Deploy)
	}{
		{name: "適用する値が無ければ、リトライしないエラー", modify: func(deploy *model.Deploy) { deploy.PendingEnv = nil }},
		{name: "pending の環境変数の JSON が壊れていたら、リトライしないエラー", modify: func(deploy *model.Deploy) { deploy.PendingEnv = datatypes.JSON(`[1]`) }},
	} {
		testingT.Run(brokenCase.name, func(subTest *testing.T) {
			deploy := newPendingConfigDeploy()
			brokenCase.modify(&deploy)
			fakeOperator := &fakeDeploymentOperator{}
			activities := NewApplyDeployConfigActivities(&fakeConfigApplyStore{deploy: deploy}, fakeOperator, newTestActivities(nil, nil).appLogger)
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.ApplyPendingConfig)

			_, executeError := testEnvironment.ExecuteActivity(activities.ApplyPendingConfig, workflow.ApplyPendingConfigInput{DeployID: "deploy-1"})

			var applicationError *temporal.ApplicationError
			if !errors.As(executeError, &applicationError) || !applicationError.NonRetryable() || fakeOperator.appliedDefinition != nil {
				subTest.Errorf("リトライしないエラーで、何も更新しないはず: %v", executeError)
			}
		})
	}
	// k8s への依頼の失敗は、リトライできるエラーのまま返る。
	testingT.Run("k8s への依頼に失敗したら、リトライできるエラー", func(subTest *testing.T) {
		fakeOperator := &fakeDeploymentOperator{applyError: errors.New("k8s down")}
		activities := NewApplyDeployConfigActivities(&fakeConfigApplyStore{deploy: newPendingConfigDeploy()}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyPendingConfig)

		_, executeError := testEnvironment.ExecuteActivity(activities.ApplyPendingConfig, workflow.ApplyPendingConfigInput{DeployID: "deploy-1"})

		var applicationError *temporal.ApplicationError
		if executeError == nil || (errors.As(executeError, &applicationError) && applicationError.NonRetryable()) {
			subTest.Errorf("リトライできるエラーのはず: %v", executeError)
		}
	})
}

// TestCommitConfigResultActivity は、成否が保存先にそのまま渡され、保存先のエラーが返ることを確認する。
func TestCommitConfigResultActivity(testingT *testing.T) {
	// 成功・失敗・保存先のエラー。
	testCases := []struct {
		name        string
		succeeded   bool
		commitError error
	}{
		{name: "成功を渡す", succeeded: true},
		{name: "失敗を渡す", succeeded: false},
		{name: "保存先のエラーは、そのまま返す(リトライされる)", succeeded: true, commitError: errors.New("database down")},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			fakeStore := &fakeConfigApplyStore{commitError: testCase.commitError}
			activities := NewApplyDeployConfigActivities(fakeStore, &fakeDeploymentOperator{}, newTestActivities(nil, nil).appLogger)
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.CommitConfigResult)

			_, executeError := testEnvironment.ExecuteActivity(activities.CommitConfigResult, workflow.CommitConfigResultInput{DeployID: "deploy-1", Succeeded: testCase.succeeded})

			if (executeError != nil) != (testCase.commitError != nil) {
				subTest.Errorf("エラーの有無が違う: %v", executeError)
			}
			if len(fakeStore.commitCalls) != 1 || fakeStore.commitCalls[0] != testCase.succeeded {
				subTest.Errorf("成否が渡されるはず: %v", fakeStore.commitCalls)
			}
		})
	}
}

// TestRollbackDeploymentConfig は、巻き戻しが、pending ではなく現在の正式な値で Deployment を作り直すことを確認する。
func TestRollbackDeploymentConfig(testingT *testing.T) {
	// 現在の値と pending が違う deploy で、現在の値が k8s に渡る。
	testingT.Run("現在の正式な値で Deployment を戻す", func(subTest *testing.T) {
		fakeOperator := &fakeDeploymentOperator{}
		activities := NewApplyDeployConfigActivities(&fakeConfigApplyStore{deploy: newPendingConfigDeploy()}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.RollbackDeploymentConfig)

		_, executeError := testEnvironment.ExecuteActivity(activities.RollbackDeploymentConfig, workflow.RollbackDeploymentConfigInput{DeployID: "deploy-1"})

		if executeError != nil || fakeOperator.appliedDefinition == nil {
			subTest.Fatalf("成功するはず: %v", executeError)
		}
		definition := fakeOperator.appliedDefinition
		if definition.Env["PORT"] != "8080" || definition.Command == nil || *definition.Command != "/app/server" || len(definition.Args) != 2 {
			subTest.Errorf("現在の値のはず(pending ではない): %+v", definition)
		}
	})
	// k8s への依頼の失敗は、リトライできるエラーのまま返る。
	testingT.Run("k8s への依頼に失敗したら、リトライできるエラー", func(subTest *testing.T) {
		fakeOperator := &fakeDeploymentOperator{applyError: errors.New("k8s down")}
		activities := NewApplyDeployConfigActivities(&fakeConfigApplyStore{deploy: newPendingConfigDeploy()}, fakeOperator, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.RollbackDeploymentConfig)

		_, executeError := testEnvironment.ExecuteActivity(activities.RollbackDeploymentConfig, workflow.RollbackDeploymentConfigInput{DeployID: "deploy-1"})

		var applicationError *temporal.ApplicationError
		if executeError == nil || (errors.As(executeError, &applicationError) && applicationError.NonRetryable()) {
			subTest.Errorf("リトライできるエラーのはず: %v", executeError)
		}
	})
}
