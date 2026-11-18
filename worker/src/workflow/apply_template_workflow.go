package workflow

import (
	"errors"
	"fmt"
	"time"

	sdkworkflow "go.temporal.io/sdk/workflow"

	"launchs/shared/templateplan"
	sharedworkflow "launchs/shared/workflow"
)

// ApplyTemplateWorkflow は、テンプレートの計画のステップを、順に実行して、部品を作る Workflow。
//
// 処理の流れ:
//  1. 計画のステップ(種類と name)を読む。
//  2. ステップごとに、「作成中」にして、実行し、「完了」にする。
//     - volume、configmap: 行を作る。
//     - deploy: 行を作り(マウントとファイルを持った形)、既存の CreateDeployWorkflow を子として実行して、起動を待つ。
//     - service: 行を作り、既存の CreateServiceWorkflow を子として実行して、active を待つ。
//     - ingress: 行を作り、ルールの反映を始め、既存の ApplyIngressRulesWorkflow を子として実行して、待つ。
//  3. 失敗したら、そのステップを「失敗」にして、そこで止める。作れたものは、残す(自動では消さない)。
//  4. 結果(成功か失敗か)を、実行に記録する。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 実行の ID。
//
// 戻り値:
//   - どれかのステップが失敗したときのエラー(記録は、してある)。
func ApplyTemplateWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.ApplyTemplateInput) error {
	// 失敗の記録は、Workflow がキャンセルされても残るよう、切り離した context で行う。
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	recordContext := sdkworkflow.WithActivityOptions(disconnectedContext, compensationOptions())
	infrastructureContext := sdkworkflow.WithActivityOptions(workflowContext, infrastructureOptions())

	// 1. 計画のステップを読む。読めなければ、記録のしようが無いので、そのまま失敗にする。
	var steps []sharedworkflow.TemplateStepRef
	if loadError := sdkworkflow.ExecuteActivity(infrastructureContext, sharedworkflow.LoadTemplateStepsActivityName, input).Get(infrastructureContext, &steps); loadError != nil {
		_ = finishTemplateApply(recordContext, input.ApplyID, false, "計画を読めませんでした")
		return fmt.Errorf("計画を読めませんでした: %w", loadError)
	}

	// 2. ステップを順に実行する。
	for index, step := range steps {
		if updateError := updateTemplateStep(infrastructureContext, input.ApplyID, index, templateplan.StatusCreating, "", ""); updateError != nil {
			return failTemplateApply(recordContext, input.ApplyID, index, step, "", updateError)
		}
		resourceID, stepError := runTemplateStep(workflowContext, input.ApplyID, index, step)
		if stepError != nil {
			return failTemplateApply(recordContext, input.ApplyID, index, step, resourceID, stepError)
		}
		if updateError := updateTemplateStep(infrastructureContext, input.ApplyID, index, templateplan.StatusDone, resourceID, ""); updateError != nil {
			return failTemplateApply(recordContext, input.ApplyID, index, step, resourceID, updateError)
		}
	}

	// 4. 全て作れた。
	if finishError := finishTemplateApply(recordContext, input.ApplyID, true, ""); finishError != nil {
		return fmt.Errorf("結果を記録できませんでした: %w", finishError)
	}
	return nil
}

// runTemplateStep は、ステップ 1 つを実行する(行を作り、必要なら、子 Workflow で、反映を待つ)。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - applyID: 実行の ID。
//   - index: ステップの位置。
//   - step: ステップの種類と name。
//
// 戻り値:
//   - 作った部品の ID(行を作れた後で、失敗したときも、分かっていれば返す)。
//   - 失敗したときのエラー。
func runTemplateStep(workflowContext sdkworkflow.Context, applyID string, index int, step sharedworkflow.TemplateStepRef) (string, error) {
	// 種類ごとの、行を作る Activity を決める。
	activityName := map[string]string{
		templateplan.StepVolume:    sharedworkflow.CreateTemplateVolumeActivityName,
		templateplan.StepConfigMap: sharedworkflow.CreateTemplateConfigMapActivityName,
		templateplan.StepDeploy:    sharedworkflow.CreateTemplateDeployActivityName,
		templateplan.StepService:   sharedworkflow.CreateTemplateServiceActivityName,
		templateplan.StepIngress:   sharedworkflow.CreateTemplateIngressActivityName,
	}[step.Kind]
	if activityName == "" {
		return "", fmt.Errorf("知らないステップの種類です: %s", step.Kind)
	}
	// 行を作る(DB の操作だけ。一時的な障害は、期限までリトライする)。
	infrastructureContext := sdkworkflow.WithActivityOptions(workflowContext, infrastructureOptions())
	var result sharedworkflow.TemplateStepResult
	if createError := sdkworkflow.ExecuteActivity(infrastructureContext, activityName, sharedworkflow.TemplateStepInput{ApplyID: applyID, Index: index}).Get(infrastructureContext, &result); createError != nil {
		return "", createError
	}
	// deploy、service、ingress は、既存の Workflow を子として実行して、完了を待つ。
	childName, childInput, childTimeout := childOf(step.Kind, result.ResourceID)
	if childName == "" {
		return result.ResourceID, nil
	}
	childContext := sdkworkflow.WithChildOptions(workflowContext, sdkworkflow.ChildWorkflowOptions{
		WorkflowID:               "template-" + applyID + "-" + step.Kind + "-" + step.Name,
		TaskQueue:                sharedworkflow.TaskQueueGeneral,
		WorkflowExecutionTimeout: childTimeout,
	})
	if childError := sdkworkflow.ExecuteChildWorkflow(childContext, childName, childInput).Get(childContext, nil); childError != nil {
		return result.ResourceID, childError
	}
	return result.ResourceID, nil
}

// childOf は、ステップの種類から、実行する子 Workflow の名前、入力、期限を決める。子を持たない種類は、名前が空。
//
// 引数:
//   - kind: ステップの種類。
//   - resourceID: 作った部品の ID。
//
// 戻り値:
//   - 子 Workflow の名前、入力、期限。
func childOf(kind string, resourceID string) (string, any, time.Duration) {
	// 種類ごとに、既存の Workflow を割り当てる。
	switch kind {
	case templateplan.StepDeploy:
		return sharedworkflow.CreateDeployWorkflowName, sharedworkflow.CreateDeployInput{DeployID: resourceID}, sharedworkflow.CreateDeployWorkflowTimeout
	case templateplan.StepService:
		return sharedworkflow.CreateServiceWorkflowName, sharedworkflow.CreateServiceInput{ServiceID: resourceID}, sharedworkflow.CreateServiceWorkflowTimeout
	case templateplan.StepIngress:
		return sharedworkflow.ApplyIngressRulesWorkflowName, sharedworkflow.ApplyIngressRulesInput{IngressID: resourceID}, sharedworkflow.ApplyIngressRulesWorkflowTimeout
	}
	return "", nil, 0
}

// updateTemplateStep は、ステップの進み具合を記録する。
//
// 引数:
//   - activityContext: Activity の設定を持つ context。
//   - applyID: 実行の ID。
//   - index: ステップの位置。
//   - status: 新しい進み具合。
//   - resourceID: 作った部品の ID。
//   - failure: 失敗の理由。
//
// 戻り値:
//   - 記録できなかったときのエラー。
func updateTemplateStep(activityContext sdkworkflow.Context, applyID string, index int, status string, resourceID string, failure string) error {
	// 記録の Activity を呼ぶ。
	return sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.UpdateTemplateStepActivityName,
		sharedworkflow.UpdateTemplateStepInput{ApplyID: applyID, Index: index, Status: status, ResourceID: resourceID, Error: failure}).Get(activityContext, nil)
}

// finishTemplateApply は、実行の結果を確定する。
//
// 引数:
//   - activityContext: Activity の設定を持つ context。
//   - applyID: 実行の ID。
//   - succeeded: 全て作れたか。
//   - failure: 失敗の理由。
//
// 戻り値:
//   - 確定できなかったときのエラー。
func finishTemplateApply(activityContext sdkworkflow.Context, applyID string, succeeded bool, failure string) error {
	// 確定の Activity を呼ぶ。
	return sdkworkflow.ExecuteActivity(activityContext, sharedworkflow.FinishTemplateApplyActivityName,
		sharedworkflow.FinishTemplateApplyInput{ApplyID: applyID, Succeeded: succeeded, Error: failure}).Get(activityContext, nil)
}

// failTemplateApply は、ステップの失敗を記録して、実行を失敗にして、元のエラーを返す(以降のステップは、実行しない)。
//
// 引数:
//   - recordContext: 記録用の Activity の設定を持つ context(キャンセルされても記録できる)。
//   - applyID: 実行の ID。
//   - index: 失敗したステップの位置。
//   - step: 失敗したステップ。
//   - resourceID: 作れていた部品の ID(分かっていれば)。
//   - stepError: 失敗の原因。
//
// 戻り値:
//   - 失敗の原因(ステップの種類と name つき)。記録に失敗したときは、その原因も含める。
func failTemplateApply(recordContext sdkworkflow.Context, applyID string, index int, step sharedworkflow.TemplateStepRef, resourceID string, stepError error) error {
	// 利用者に見せる理由は、ステップの種類と name を添えた、エラーの文。
	message := fmt.Sprintf("%s %s: %s", step.Kind, step.Name, rootMessage(stepError))
	updateError := updateTemplateStep(recordContext, applyID, index, templateplan.StatusFailed, resourceID, message)
	finishError := finishTemplateApply(recordContext, applyID, false, message)
	if updateError != nil || finishError != nil {
		return fmt.Errorf("%s(失敗の記録にも失敗しました: %v %v)", message, updateError, finishError)
	}
	return fmt.Errorf("%s", message)
}

// rootMessage は、エラーの、いちばん内側の、利用者に見せられる文を返す(Temporal のエラーの包みを、外す)。
//
// 引数:
//   - err: エラー。
//
// 戻り値:
//   - 内側のエラーの文。
func rootMessage(err error) string {
	// 包みを、最後まで外す。
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return err.Error()
		}
		err = inner
	}
}
