package activity

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"gorm.io/datatypes"

	"launchs/shared/logger"
	"launchs/shared/model"
	"launchs/shared/workflow"
)

// fakeDeployStore は、テスト用の DeployStore。決めた deploy を返し、状態の更新を覚える。
type fakeDeployStore struct {
	deploy      model.Deploy
	findError   error
	specError   error
	updateError error
	updates     []string
}

// FindForWorker は、決めた deploy を返す。
func (fakeStore *fakeDeployStore) FindForWorker(_ context.Context, _ string) (model.Deploy, error) {
	return fakeStore.deploy, fakeStore.findError
}

// FindSpec は、small 相当の大きさを返す。
func (fakeStore *fakeDeployStore) FindSpec(_ context.Context, specName string) (model.DeploySpec, error) {
	return model.DeploySpec{Name: specName, CPUMillicores: 250, MemoryMiB: 256}, fakeStore.specError
}

// UpdateStatus は、「変更前→変更後」を覚えて、決めたエラーを返す。
func (fakeStore *fakeDeployStore) UpdateStatus(_ context.Context, _ string, fromStatus string, toStatus string) (bool, error) {
	fakeStore.updates = append(fakeStore.updates, fromStatus+"->"+toStatus)
	return true, fakeStore.updateError
}

// fakeDeploymentOperator は、テスト用の DeploymentOperator。呼び出しを覚え、決めた結果を返す。
type fakeDeploymentOperator struct {
	ensuredNamespaces []string
	ensureError       error
	appliedDefinition *DeploymentDefinition
	applyError        error
	// notReadyChecks は、DeploymentReady が「まだ」を返す残りの回数。
	notReadyChecks int
	readyChecks    int
	readyError     error
}

// EnsureNamespace は、名前を覚えて、決めたエラーを返す。
func (fakeOperator *fakeDeploymentOperator) EnsureNamespace(_ context.Context, namespaceName string) error {
	fakeOperator.ensuredNamespaces = append(fakeOperator.ensuredNamespaces, namespaceName)
	return fakeOperator.ensureError
}

// ApplyDeployment は、内容を覚えて、決めたエラーを返す。
func (fakeOperator *fakeDeploymentOperator) ApplyDeployment(_ context.Context, definition DeploymentDefinition) error {
	fakeOperator.appliedDefinition = &definition
	return fakeOperator.applyError
}

// DeploymentReady は、決めた回数だけ「まだ」を返し、そのあとは「Ready」を返す。
func (fakeOperator *fakeDeploymentOperator) DeploymentReady(_ context.Context, _ string, _ string) (bool, error) {
	fakeOperator.readyChecks++
	if fakeOperator.readyError != nil {
		return false, fakeOperator.readyError
	}
	if fakeOperator.notReadyChecks > 0 {
		fakeOperator.notReadyChecks--
		return false, nil
	}
	return true, nil
}

// newTestActivities は、fake を持つ CreateDeployActivities を作る(待ち時間は短くする)。
//
// 引数:
//   - fakeStore: deploy の保存先の fake。
//   - fakeOperator: k8s を操作するものの fake。
//
// 戻り値:
//   - 作成した CreateDeployActivities。
func newTestActivities(fakeStore *fakeDeployStore, fakeOperator *fakeDeploymentOperator) *CreateDeployActivities {
	// ログは捨てる入れ物に出し、確かめる間隔はテストが遅くならないよう短くする。
	activities := NewCreateDeployActivities(fakeStore, fakeOperator, logger.New(&bytes.Buffer{}, "worker", slog.LevelInfo))
	activities.deploymentPollInterval = time.Millisecond
	return activities
}

// newTestDeploy は、テスト用の deploy を作る。
//
// 戻り値:
//   - pending 状態の、環境変数と引数が入った deploy。
func newTestDeploy() model.Deploy {
	command := "/app/server"
	return model.Deploy{
		ID: "deploy-1", ProjectID: "project-1", ImageRef: "registry/app:1", Spec: "small", Replicas: 2, Status: model.DeployStatusPending,
		Env: datatypes.JSON(`{"PORT":"8080"}`), Command: &command, Args: datatypes.JSON(`["--x","1"]`),
	}
}

// TestEnsureProjectNamespace は、deploy が属するプロジェクトの namespace を用意し、
// 名前の形の不正だけはリトライしないエラーにすることを確認する。
func TestEnsureProjectNamespace(testingT *testing.T) {
	// 成功する場合と、一時的な障害、名前の不正の場合。
	testCases := []struct {
		name               string
		ensureError        error
		expectError        bool
		expectNonRetryable bool
	}{
		{name: "deploy のプロジェクトの ID で namespace を用意する"},
		{name: "一時的な障害はそのまま返す(リトライされる)", ensureError: errors.New("k8s down"), expectError: true},
		{name: "名前の形の不正はリトライしない", ensureError: ErrInvalidNamespaceName, expectError: true, expectNonRetryable: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// Activity をテスト環境で実行する。
			fakeOperator := &fakeDeploymentOperator{ensureError: testCase.ensureError}
			activities := newTestActivities(&fakeDeployStore{deploy: newTestDeploy()}, fakeOperator)
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.EnsureProjectNamespace)
			_, executeError := testEnvironment.ExecuteActivity(activities.EnsureProjectNamespace, workflow.EnsureProjectNamespaceInput{DeployID: "deploy-1"})

			// namespace 名がプロジェクトの ID、エラーの有無、リトライしないかを確認する。
			if len(fakeOperator.ensuredNamespaces) != 1 || fakeOperator.ensuredNamespaces[0] != "project-1" {
				subTest.Errorf("プロジェクトの ID で用意するはず: %v", fakeOperator.ensuredNamespaces)
			}
			if (executeError != nil) != testCase.expectError {
				subTest.Fatalf("エラーの有無が違う: %v", executeError)
			}
			// 普通のエラーも ApplicationError に包まれるので、リトライしない印(NonRetryable)の値で見分ける。
			var applicationError *temporal.ApplicationError
			isNonRetryable := errors.As(executeError, &applicationError) && applicationError.NonRetryable()
			if testCase.expectError && isNonRetryable != testCase.expectNonRetryable {
				subTest.Errorf("リトライしないかどうかが違う: %v", executeError)
			}
		})
	}
}

// TestApplyDeployment は、deploy とスペックから、k8s に作る内容を組み立てて渡し、
// 壊れた JSON はリトライしないエラーにすることを確認する。
func TestApplyDeployment(testingT *testing.T) {
	// 内容の組み立て。
	testingT.Run("deploy とスペックから、Deployment の内容を組み立てて渡す", func(subTest *testing.T) {
		fakeOperator := &fakeDeploymentOperator{}
		activities := newTestActivities(&fakeDeployStore{deploy: newTestDeploy()}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyDeployment)

		_, executeError := testEnvironment.ExecuteActivity(activities.ApplyDeployment, workflow.ApplyDeploymentInput{DeployID: "deploy-1"})

		if executeError != nil || fakeOperator.appliedDefinition == nil {
			subTest.Fatalf("成功するはず: %v", executeError)
		}
		definition := fakeOperator.appliedDefinition
		if definition.Namespace != "project-1" || definition.Name != "deploy-1" || definition.Image != "registry/app:1" || definition.Replicas != 2 ||
			definition.CPUMillicores != 250 || definition.MemoryMiB != 256 || definition.Env["PORT"] != "8080" ||
			definition.Command == nil || *definition.Command != "/app/server" || len(definition.Args) != 2 || definition.Args[0] != "--x" {
			subTest.Errorf("内容が違う: %+v", definition)
		}
	})
	// イメージの既定のコマンドと引数(NULL)のときは、nil のまま渡す。
	testingT.Run("コマンドと引数が NULL なら、nil で渡す", func(subTest *testing.T) {
		deploy := newTestDeploy()
		deploy.Command, deploy.Args = nil, nil
		fakeOperator := &fakeDeploymentOperator{}
		activities := newTestActivities(&fakeDeployStore{deploy: deploy}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyDeployment)

		if _, executeError := testEnvironment.ExecuteActivity(activities.ApplyDeployment, workflow.ApplyDeploymentInput{DeployID: "deploy-1"}); executeError != nil {
			subTest.Fatalf("成功するはず: %v", executeError)
		}
		if fakeOperator.appliedDefinition.Command != nil || fakeOperator.appliedDefinition.Args != nil {
			subTest.Errorf("nil のはず: %+v", fakeOperator.appliedDefinition)
		}
	})
	// DB の JSON が壊れているときは、リトライしても無駄なので、リトライしない。
	testingT.Run("環境変数の JSON が壊れていたら、リトライしないエラー", func(subTest *testing.T) {
		deploy := newTestDeploy()
		deploy.Env = datatypes.JSON(`[1,2]`)
		fakeOperator := &fakeDeploymentOperator{}
		activities := newTestActivities(&fakeDeployStore{deploy: deploy}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyDeployment)

		_, executeError := testEnvironment.ExecuteActivity(activities.ApplyDeployment, workflow.ApplyDeploymentInput{DeployID: "deploy-1"})

		var applicationError *temporal.ApplicationError
		if !errors.As(executeError, &applicationError) || !applicationError.NonRetryable() || fakeOperator.appliedDefinition != nil {
			subTest.Errorf("リトライしないエラーで、何も作らないはず: %v", executeError)
		}
	})
	// k8s の障害は、そのまま返る(リトライされる)。
	testingT.Run("k8s の障害はエラーになる", func(subTest *testing.T) {
		fakeOperator := &fakeDeploymentOperator{applyError: errors.New("k8s down")}
		activities := newTestActivities(&fakeDeployStore{deploy: newTestDeploy()}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyDeployment)
		if _, executeError := testEnvironment.ExecuteActivity(activities.ApplyDeployment, workflow.ApplyDeploymentInput{DeployID: "deploy-1"}); executeError == nil {
			subTest.Errorf("エラーになるはず")
		}
	})
}

// TestWaitDeploymentReady は、Ready になるまで確かめ続け、確認の失敗はエラーにすることを確認する。
func TestWaitDeploymentReady(testingT *testing.T) {
	// すぐ Ready・待つ・確認の失敗の場合。
	testCases := []struct {
		name           string
		fakeOperator   *fakeDeploymentOperator
		expectedChecks int
		expectError    bool
	}{
		{name: "すぐ Ready なら、1回の確認で終わる", fakeOperator: &fakeDeploymentOperator{}, expectedChecks: 1},
		{name: "Ready になるまで、確認を繰り返して待つ", fakeOperator: &fakeDeploymentOperator{notReadyChecks: 3}, expectedChecks: 4},
		{name: "確認のエラーはそのまま返す", fakeOperator: &fakeDeploymentOperator{readyError: errors.New("k8s down")}, expectedChecks: 1, expectError: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// Activity をテスト環境で実行する。
			activities := newTestActivities(&fakeDeployStore{deploy: newTestDeploy()}, testCase.fakeOperator)
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.WaitDeploymentReady)
			_, executeError := testEnvironment.ExecuteActivity(activities.WaitDeploymentReady, workflow.WaitDeploymentReadyInput{DeployID: "deploy-1"})

			// 確認の回数と、エラーの有無を確認する。
			if testCase.fakeOperator.readyChecks != testCase.expectedChecks {
				subTest.Errorf("確認の回数が違う: 期待 %d、実際 %d", testCase.expectedChecks, testCase.fakeOperator.readyChecks)
			}
			if (executeError != nil) != testCase.expectError {
				subTest.Errorf("エラーの有無が違う: %v", executeError)
			}
		})
	}
}

// TestMarkDeployResult は、成功なら active(replicas が 0 なら scaled_zero)、失敗なら failed に、
// pending のときだけ変えることを確認する。
func TestMarkDeployResult(testingT *testing.T) {
	// 成功・replicas 0 での成功・失敗の場合。
	testCases := []struct {
		name            string
		replicas        int
		succeeded       bool
		expectedUpdates []string
	}{
		{name: "成功なら pending から active にする", replicas: 1, succeeded: true, expectedUpdates: []string{"pending->active"}},
		{name: "replicas が 0 の成功なら scaled_zero にする", replicas: 0, succeeded: true, expectedUpdates: []string{"pending->scaled_zero"}},
		{name: "失敗なら pending から failed にする", replicas: 1, succeeded: false, expectedUpdates: []string{"pending->failed"}},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// Activity をテスト環境で実行する。
			deploy := newTestDeploy()
			deploy.Replicas = testCase.replicas
			fakeStore := &fakeDeployStore{deploy: deploy}
			activities := newTestActivities(fakeStore, &fakeDeploymentOperator{})
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.MarkDeployResult)
			_, executeError := testEnvironment.ExecuteActivity(activities.MarkDeployResult, workflow.MarkDeployResultInput{DeployID: "deploy-1", Succeeded: testCase.succeeded})

			// 更新の内容を確認する。
			if executeError != nil || len(fakeStore.updates) != 1 || fakeStore.updates[0] != testCase.expectedUpdates[0] {
				subTest.Errorf("期待 %v、実際 %v(エラー %v)", testCase.expectedUpdates, fakeStore.updates, executeError)
			}
		})
	}
	// 状態の更新に失敗したときは、エラーを返す(Temporal がリトライする)。
	testingT.Run("状態の更新の失敗は、エラーになる", func(subTest *testing.T) {
		activities := newTestActivities(&fakeDeployStore{deploy: newTestDeploy(), updateError: errors.New("db down")}, &fakeDeploymentOperator{})
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.MarkDeployResult)
		if _, executeError := testEnvironment.ExecuteActivity(activities.MarkDeployResult, workflow.MarkDeployResultInput{DeployID: "deploy-1", Succeeded: false}); executeError == nil {
			subTest.Errorf("エラーになるはず")
		}
	})
}

// TestLoggingDeploymentOperator は、仮の実装が、何も作らず、常に Ready を返し、警告のログを出すことを確認する。
func TestLoggingDeploymentOperator(testingT *testing.T) {
	// ログの出力先を、テストで読める入れ物にする。
	var logOutput bytes.Buffer
	loggingOperator := NewLoggingDeploymentOperator(logger.New(&logOutput, "worker", slog.LevelInfo))

	// 3つの操作を呼ぶ。
	ensureError := loggingOperator.EnsureNamespace(context.Background(), "project-1")
	applyError := loggingOperator.ApplyDeployment(context.Background(), DeploymentDefinition{Namespace: "project-1", Name: "deploy-1"})
	ready, readyError := loggingOperator.DeploymentReady(context.Background(), "project-1", "deploy-1")

	// どれもエラーにならず、Ready で、警告のログが出ていること。
	if ensureError != nil || applyError != nil || readyError != nil || !ready {
		testingT.Errorf("エラーにならず、Ready のはず: %v %v %v %v", ensureError, applyError, readyError, ready)
	}
	if bytes.Count(logOutput.Bytes(), []byte(`"level":"WARN"`)) != 2 {
		testingT.Errorf("警告のログが2回出るはず: %s", logOutput.String())
	}
}
