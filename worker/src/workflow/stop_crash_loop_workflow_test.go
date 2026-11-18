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

// newStopCrashLoopEnvironment は、deploy を止める Workflow のテスト環境を作り、3つの Activity を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。Activity は、呼び出し側が OnActivity で振る舞いを決める。
func newStopCrashLoopEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る(待ち時間は、時計を進めて飛ばす)。
	testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	// Activity は、名前だけを登録する(中身はテストが差し替える)。
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.BeginCrashLoopStopInput) (sharedworkflow.BeginCrashLoopStopResult, error) {
		return sharedworkflow.BeginCrashLoopStopResult{}, nil
	}, activity.RegisterOptions{Name: sharedworkflow.BeginCrashLoopStopActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.ScaleToZeroInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.ScaleToZeroActivityName})
	testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.FinishCrashLoopStopInput) error { return nil },
		activity.RegisterOptions{Name: sharedworkflow.FinishCrashLoopStopActivityName})
	return testEnvironment
}

// TestStopCrashLoopWorkflow は、準備、レプリカを 0 に、failed の順に進み、準備ができなければ何もせず、
// レプリカを 0 にできなくても failed にして終わることを確認する。
func TestStopCrashLoopWorkflow(testingT *testing.T) {
	// 入力と、各 Activity の入力。
	stopInput := sharedworkflow.StopCrashLoopInput{DeployID: "deploy-1", PodUID: "uid-1", PodName: "pod-1", Reason: "CrashLoopBackOff", Message: "再起動 9 回"}
	beginInput := sharedworkflow.BeginCrashLoopStopInput{Stop: stopInput}
	scaleInput := sharedworkflow.ScaleToZeroInput{DeployID: "deploy-1"}
	finishInput := sharedworkflow.FinishCrashLoopStopInput{DeployID: "deploy-1"}

	testingT.Run("準備ができれば、レプリカを 0 にして、failed にして、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newStopCrashLoopEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.BeginCrashLoopStopActivityName, mock.Anything, beginInput).Return(sharedworkflow.BeginCrashLoopStopResult{Started: true}, nil)
		testEnvironment.OnActivity(sharedworkflow.ScaleToZeroActivityName, mock.Anything, scaleInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.FinishCrashLoopStopActivityName, mock.Anything, finishInput).Return(nil)

		testEnvironment.ExecuteWorkflow(StopCrashLoopWorkflow, stopInput)

		if !testEnvironment.IsWorkflowCompleted() || testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.ScaleToZeroActivityName, mock.Anything, scaleInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.FinishCrashLoopStopActivityName, mock.Anything, finishInput)
	})
	testingT.Run("準備ができなければ(active でない)、何もせずに、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newStopCrashLoopEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.BeginCrashLoopStopActivityName, mock.Anything, beginInput).Return(sharedworkflow.BeginCrashLoopStopResult{Started: false}, nil)

		testEnvironment.ExecuteWorkflow(StopCrashLoopWorkflow, stopInput)

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.ScaleToZeroActivityName, mock.Anything, scaleInput)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.FinishCrashLoopStopActivityName, mock.Anything, finishInput)
	})
	testingT.Run("レプリカを 0 にするのが一時的に失敗しても、リトライで回復すれば、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newStopCrashLoopEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.BeginCrashLoopStopActivityName, mock.Anything, beginInput).Return(sharedworkflow.BeginCrashLoopStopResult{Started: true}, nil)
		testEnvironment.OnActivity(sharedworkflow.ScaleToZeroActivityName, mock.Anything, scaleInput).Return(errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.ScaleToZeroActivityName, mock.Anything, scaleInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.FinishCrashLoopStopActivityName, mock.Anything, finishInput).Return(nil)

		testEnvironment.ExecuteWorkflow(StopCrashLoopWorkflow, stopInput)

		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("リトライで回復して成功するはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.ScaleToZeroActivityName, 3)
	})
	testingT.Run("レプリカを 0 にできなくても、applying のまま残さず、failed にして、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newStopCrashLoopEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.BeginCrashLoopStopActivityName, mock.Anything, beginInput).Return(sharedworkflow.BeginCrashLoopStopResult{Started: true}, nil)
		testEnvironment.OnActivity(sharedworkflow.ScaleToZeroActivityName, mock.Anything, scaleInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.FinishCrashLoopStopActivityName, mock.Anything, finishInput).Return(nil)

		testEnvironment.ExecuteWorkflow(StopCrashLoopWorkflow, stopInput)

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.FinishCrashLoopStopActivityName, mock.Anything, finishInput)
	})
	testingT.Run("failed にもできなければ、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newStopCrashLoopEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.BeginCrashLoopStopActivityName, mock.Anything, beginInput).Return(sharedworkflow.BeginCrashLoopStopResult{Started: true}, nil)
		testEnvironment.OnActivity(sharedworkflow.ScaleToZeroActivityName, mock.Anything, scaleInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.FinishCrashLoopStopActivityName, mock.Anything, finishInput).Return(errors.New("database down"))

		testEnvironment.ExecuteWorkflow(StopCrashLoopWorkflow, stopInput)

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
	})
	testingT.Run("準備に失敗し続けたら、何もせずに、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newStopCrashLoopEnvironment(subTest)
		testEnvironment.OnActivity(sharedworkflow.BeginCrashLoopStopActivityName, mock.Anything, beginInput).Return(sharedworkflow.BeginCrashLoopStopResult{}, errors.New("database down"))

		testEnvironment.ExecuteWorkflow(StopCrashLoopWorkflow, stopInput)

		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.ScaleToZeroActivityName, mock.Anything, scaleInput)
	})
}
