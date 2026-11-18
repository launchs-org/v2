package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// newApplyTemplateEnvironment は、テンプレートの実行の Workflow のテスト環境を作る。Activity と、子 Workflow を、名前で登録する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - Workflow のテスト環境。振る舞いは、呼び出し側が OnActivity と OnWorkflow で決める。
func newApplyTemplateEnvironment(testingT *testing.T) *testsuite.TestWorkflowEnvironment {
	// Temporal のテスト用の環境を作る。
	testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	register := func(name string, function any) {
		testEnvironment.RegisterActivityWithOptions(function, activity.RegisterOptions{Name: name})
	}
	register(sharedworkflow.LoadTemplateStepsActivityName, func(context.Context, sharedworkflow.ApplyTemplateInput) ([]sharedworkflow.TemplateStepRef, error) {
		return nil, nil
	})
	for _, name := range []string{sharedworkflow.CreateTemplateVolumeActivityName, sharedworkflow.CreateTemplateConfigMapActivityName, sharedworkflow.CreateTemplateDeployActivityName, sharedworkflow.CreateTemplateServiceActivityName, sharedworkflow.CreateTemplateIngressActivityName} {
		register(name, func(context.Context, sharedworkflow.TemplateStepInput) (sharedworkflow.TemplateStepResult, error) {
			return sharedworkflow.TemplateStepResult{}, nil
		})
	}
	register(sharedworkflow.UpdateTemplateStepActivityName, func(context.Context, sharedworkflow.UpdateTemplateStepInput) error { return nil })
	register(sharedworkflow.FinishTemplateApplyActivityName, func(context.Context, sharedworkflow.FinishTemplateApplyInput) error { return nil })
	// 子 Workflow(既存の Workflow)は、名前だけを登録して、中身を差し替える。
	testEnvironment.RegisterWorkflowWithOptions(func(sdkworkflow.Context, sharedworkflow.CreateDeployInput) error { return nil }, sdkworkflow.RegisterOptions{Name: sharedworkflow.CreateDeployWorkflowName})
	testEnvironment.RegisterWorkflowWithOptions(func(sdkworkflow.Context, sharedworkflow.CreateServiceInput) error { return nil }, sdkworkflow.RegisterOptions{Name: sharedworkflow.CreateServiceWorkflowName})
	testEnvironment.RegisterWorkflowWithOptions(func(sdkworkflow.Context, sharedworkflow.ApplyIngressRulesInput) error { return nil }, sdkworkflow.RegisterOptions{Name: sharedworkflow.ApplyIngressRulesWorkflowName})
	return testEnvironment
}

// TestApplyTemplateWorkflow は、ステップを順に実行し(deploy、service、ingress は、子 Workflow を待つ)、全て成功なら成功を確定し、
// どこかで失敗したら、そのステップを失敗にして、以降は実行せず、失敗を確定して、失敗で終わることを確認する。
func TestApplyTemplateWorkflow(testingT *testing.T) {
	input := sharedworkflow.ApplyTemplateInput{ApplyID: "apply-1"}
	steps := []sharedworkflow.TemplateStepRef{{Kind: "volume", Name: "data"}, {Kind: "configmap", Name: "conf"}, {Kind: "deploy", Name: "db"}, {Kind: "service", Name: "db"}, {Kind: "ingress", Name: "ingress"}}
	step := func(index int) sharedworkflow.TemplateStepInput {
		return sharedworkflow.TemplateStepInput{ApplyID: "apply-1", Index: index}
	}
	results := []string{"volume-1", "configmap-1", "deploy-1", "service-1", "ingress-1"}
	// 全ての行を作る Activity が、成功する設定。
	mockCreates := func(environment *testsuite.TestWorkflowEnvironment) {
		environment.OnActivity(sharedworkflow.LoadTemplateStepsActivityName, mock.Anything, input).Return(steps, nil)
		for index, name := range []string{sharedworkflow.CreateTemplateVolumeActivityName, sharedworkflow.CreateTemplateConfigMapActivityName, sharedworkflow.CreateTemplateDeployActivityName, sharedworkflow.CreateTemplateServiceActivityName, sharedworkflow.CreateTemplateIngressActivityName} {
			environment.OnActivity(name, mock.Anything, step(index)).Return(sharedworkflow.TemplateStepResult{ResourceID: results[index]}, nil)
		}
		environment.OnActivity(sharedworkflow.UpdateTemplateStepActivityName, mock.Anything, mock.Anything).Return(nil)
		environment.OnActivity(sharedworkflow.FinishTemplateApplyActivityName, mock.Anything, mock.Anything).Return(nil)
	}

	testingT.Run("全て成功すると、各ステップを、作成中 → 完了にし、deploy、service、ingress の子 Workflow を実行して、成功を確定する", func(subTest *testing.T) {
		environment := newApplyTemplateEnvironment(subTest)
		mockCreates(environment)
		environment.OnWorkflow(sharedworkflow.CreateDeployWorkflowName, mock.Anything, sharedworkflow.CreateDeployInput{DeployID: "deploy-1"}).Return(nil)
		environment.OnWorkflow(sharedworkflow.CreateServiceWorkflowName, mock.Anything, sharedworkflow.CreateServiceInput{ServiceID: "service-1"}).Return(nil)
		environment.OnWorkflow(sharedworkflow.ApplyIngressRulesWorkflowName, mock.Anything, sharedworkflow.ApplyIngressRulesInput{IngressID: "ingress-1"}).Return(nil)

		environment.ExecuteWorkflow(ApplyTemplateWorkflow, input)

		if !environment.IsWorkflowCompleted() || environment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", environment.GetWorkflowError())
		}
		environment.AssertCalled(subTest, sharedworkflow.FinishTemplateApplyActivityName, mock.Anything, sharedworkflow.FinishTemplateApplyInput{ApplyID: "apply-1", Succeeded: true})
		for index := range steps {
			environment.AssertCalled(subTest, sharedworkflow.UpdateTemplateStepActivityName, mock.Anything, sharedworkflow.UpdateTemplateStepInput{ApplyID: "apply-1", Index: index, Status: "creating"})
			environment.AssertCalled(subTest, sharedworkflow.UpdateTemplateStepActivityName, mock.Anything, sharedworkflow.UpdateTemplateStepInput{ApplyID: "apply-1", Index: index, Status: "done", ResourceID: results[index]})
		}
		environment.AssertNumberOfCalls(subTest, sharedworkflow.UpdateTemplateStepActivityName, 10)
	})

	testingT.Run("deploy の起動(子 Workflow)に失敗すると、そのステップを失敗にし、service 以降は実行せず、失敗を確定して、失敗で終わる。作れた volume は残る(消さない)", func(subTest *testing.T) {
		environment := newApplyTemplateEnvironment(subTest)
		mockCreates(environment)
		environment.OnWorkflow(sharedworkflow.CreateDeployWorkflowName, mock.Anything, mock.Anything).Return(errors.New("起動しませんでした"))

		environment.ExecuteWorkflow(ApplyTemplateWorkflow, input)

		if environment.GetWorkflowError() == nil {
			subTest.Fatal("失敗で終わるはず")
		}
		// 失敗したのは、deploy(位置 2)。作れた deploy の ID も、記録される。
		environment.AssertCalled(subTest, sharedworkflow.UpdateTemplateStepActivityName, mock.Anything, mock.MatchedBy(func(update sharedworkflow.UpdateTemplateStepInput) bool {
			return update.Index == 2 && update.Status == "failed" && update.ResourceID == "deploy-1" && update.Error != ""
		}))
		environment.AssertNotCalled(subTest, sharedworkflow.CreateTemplateServiceActivityName, mock.Anything, mock.Anything)
		environment.AssertNotCalled(subTest, sharedworkflow.CreateTemplateIngressActivityName, mock.Anything, mock.Anything)
		environment.AssertCalled(subTest, sharedworkflow.FinishTemplateApplyActivityName, mock.Anything, mock.MatchedBy(func(finish sharedworkflow.FinishTemplateApplyInput) bool {
			return !finish.Succeeded && finish.Error != ""
		}))
		environment.AssertNotCalled(subTest, sharedworkflow.FinishTemplateApplyActivityName, mock.Anything, sharedworkflow.FinishTemplateApplyInput{ApplyID: "apply-1", Succeeded: true})
	})

	testingT.Run("行を作る Activity が、リトライしないエラーで失敗すると(クォータなど)、そのステップを失敗にして止める", func(subTest *testing.T) {
		environment := newApplyTemplateEnvironment(subTest)
		environment.OnActivity(sharedworkflow.LoadTemplateStepsActivityName, mock.Anything, input).Return(steps, nil)
		environment.OnActivity(sharedworkflow.CreateTemplateVolumeActivityName, mock.Anything, step(0)).Return(sharedworkflow.TemplateStepResult{}, errors.New("volume の数が上限に達しています"))
		environment.OnActivity(sharedworkflow.UpdateTemplateStepActivityName, mock.Anything, mock.Anything).Return(nil)
		environment.OnActivity(sharedworkflow.FinishTemplateApplyActivityName, mock.Anything, mock.Anything).Return(nil)

		environment.ExecuteWorkflow(ApplyTemplateWorkflow, input)

		if environment.GetWorkflowError() == nil {
			subTest.Fatal("失敗で終わるはず")
		}
		environment.AssertCalled(subTest, sharedworkflow.UpdateTemplateStepActivityName, mock.Anything, mock.MatchedBy(func(update sharedworkflow.UpdateTemplateStepInput) bool {
			return update.Index == 0 && update.Status == "failed" && update.Error != ""
		}))
		environment.AssertNotCalled(subTest, sharedworkflow.CreateTemplateConfigMapActivityName, mock.Anything, mock.Anything)
	})

	testingT.Run("計画を読めなければ、失敗を確定して、失敗で終わる", func(subTest *testing.T) {
		environment := newApplyTemplateEnvironment(subTest)
		environment.OnActivity(sharedworkflow.LoadTemplateStepsActivityName, mock.Anything, input).Return(nil, errors.New("計画が壊れています"))
		environment.OnActivity(sharedworkflow.FinishTemplateApplyActivityName, mock.Anything, mock.Anything).Return(nil)

		environment.ExecuteWorkflow(ApplyTemplateWorkflow, input)

		if environment.GetWorkflowError() == nil {
			subTest.Fatal("失敗で終わるはず")
		}
		environment.AssertCalled(subTest, sharedworkflow.FinishTemplateApplyActivityName, mock.Anything, mock.MatchedBy(func(finish sharedworkflow.FinishTemplateApplyInput) bool { return !finish.Succeeded }))
	})
}
