package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	sharedworkflow "launchs/shared/workflow"
)

// newCreateDeployEnvironment は、deploy 作成の Workflow のテスト環境を作り、4つの Activity を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。Activity は、呼び出し側が OnActivity で振る舞いを決める。
func newCreateDeployEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る(待ち時間は、実際には待たずに、時計を進めて飛ばす)。
	testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	// Activity は、名前だけを登録する(中身はテストが差し替える)。
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.EnsureProjectNamespaceInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.EnsureProjectNamespaceActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.ApplyDeploymentInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.ApplyDeploymentActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.WaitDeploymentReadyInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.WaitDeploymentReadyActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.MarkDeployResultInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.MarkDeployResultActivityName})
	return testEnvironment
}

// TestCreateDeployWorkflow は、namespace、Deployment、起動待ちの順に進み、成功なら active に、
// どこかで失敗したら、以降は進まずに failed にして、失敗で終わることを確認する。
func TestCreateDeployWorkflow(testingT *testing.T) {
	// 各 Activity の入力と、記録する結果。
	ensureInput := sharedworkflow.EnsureProjectNamespaceInput{DeployID: "deploy-1"}
	applyInput := sharedworkflow.ApplyDeploymentInput{DeployID: "deploy-1"}
	waitInput := sharedworkflow.WaitDeploymentReadyInput{DeployID: "deploy-1"}
	succeededInput := sharedworkflow.MarkDeployResultInput{DeployID: "deploy-1", Succeeded: true}
	failedInput := sharedworkflow.MarkDeployResultInput{DeployID: "deploy-1", Succeeded: false}

	// 全て成功する場合。
	testingT.Run("全て成功すると、成功を記録して、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newCreateDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.EnsureProjectNamespaceActivityName, mock.Anything, ensureInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.ApplyDeploymentActivityName, mock.Anything, applyInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.MarkDeployResultActivityName, mock.Anything, succeededInput).Return(nil)

		testEnvironment.ExecuteWorkflow(CreateDeployWorkflow, sharedworkflow.CreateDeployInput{DeployID: "deploy-1"})

		if !testEnvironment.IsWorkflowCompleted() || testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkDeployResultActivityName, mock.Anything, succeededInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.MarkDeployResultActivityName, mock.Anything, failedInput)
	})

	// namespace の用意が、期限まで失敗し続ける場合。
	testingT.Run("namespace を用意できなければ、Deployment は作らず、失敗を記録して失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newCreateDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.EnsureProjectNamespaceActivityName, mock.Anything, ensureInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkDeployResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(CreateDeployWorkflow, sharedworkflow.CreateDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.ApplyDeploymentActivityName, mock.Anything, applyInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkDeployResultActivityName, mock.Anything, failedInput)
	})

	// 一時的な障害から回復する場合。
	testingT.Run("namespace の用意が一時的に失敗しても、リトライで回復すれば成功で終わる", func(subTest *testing.T) {
		testEnvironment := newCreateDeployEnvironment(subTest)
		// 最初の2回は失敗し、3回目から成功する。
		testEnvironment.OnActivity(sharedworkflow.EnsureProjectNamespaceActivityName, mock.Anything, ensureInput).Return(errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.EnsureProjectNamespaceActivityName, mock.Anything, ensureInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.ApplyDeploymentActivityName, mock.Anything, applyInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.MarkDeployResultActivityName, mock.Anything, succeededInput).Return(nil)

		testEnvironment.ExecuteWorkflow(CreateDeployWorkflow, sharedworkflow.CreateDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("リトライで回復して成功するはず: %v", testEnvironment.GetWorkflowError())
		}
	})

	// Deployment の作成が失敗する場合。
	testingT.Run("Deployment を作れなければ、起動を待たず、失敗を記録して失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newCreateDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.EnsureProjectNamespaceActivityName, mock.Anything, ensureInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.ApplyDeploymentActivityName, mock.Anything, applyInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkDeployResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(CreateDeployWorkflow, sharedworkflow.CreateDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkDeployResultActivityName, mock.Anything, failedInput)
	})

	// 起動が確認できない(タイムアウトなど)場合は、リトライせずに failed にする。
	testingT.Run("起動を待つ Activity が失敗したら、リトライせず、失敗を記録して失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newCreateDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.EnsureProjectNamespaceActivityName, mock.Anything, ensureInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.ApplyDeploymentActivityName, mock.Anything, applyInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(errors.New("not ready in time")).Once()
		testEnvironment.OnActivity(sharedworkflow.MarkDeployResultActivityName, mock.Anything, failedInput).Return(nil)

		testEnvironment.ExecuteWorkflow(CreateDeployWorkflow, sharedworkflow.CreateDeployInput{DeployID: "deploy-1"})

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		// 待つ Activity は、1回だけ(リトライしない)。
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.WaitDeploymentReadyActivityName, 1)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkDeployResultActivityName, mock.Anything, failedInput)
	})

	// 結果の記録にも失敗した場合。
	testingT.Run("結果の記録にも失敗したら、両方の原因を含めて失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newCreateDeployEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.EnsureProjectNamespaceActivityName, mock.Anything, ensureInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.ApplyDeploymentActivityName, mock.Anything, applyInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkDeployResultActivityName, mock.Anything, failedInput).Return(errors.New("db down"))

		testEnvironment.ExecuteWorkflow(CreateDeployWorkflow, sharedworkflow.CreateDeployInput{DeployID: "deploy-1"})

		workflowError := testEnvironment.GetWorkflowError()
		if workflowError == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		if message := workflowError.Error(); !strings.Contains(message, "Deployment を作れませんでした") || !strings.Contains(message, "起動の結果を記録できませんでした") {
			subTest.Errorf("両方の原因を含むはず: %v", workflowError)
		}
	})
}
