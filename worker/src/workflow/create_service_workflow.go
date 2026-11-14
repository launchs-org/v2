package workflow

import (
	"errors"
	"fmt"

	sdkworkflow "go.temporal.io/sdk/workflow"

	sharedworkflow "launchs/shared/workflow"
)

// CreateServiceWorkflow は、service を、k8s の Service として作り、結果(active または failed)を記録する。
//
// 処理の順序:
//  1. k8s の Service を作る(払い出された ClusterIP と、DNS 名を受け取る)
//  2. 結果を記録する(成功なら active にして DNS 名と ClusterIP を記録、失敗なら failed)
//
// 1 は、k8s の一時的な障害なら、リトライの期限(2分)までリトライする。期限までに作れなければ、failed にして、失敗で終わる。
//
// 引数:
//   - workflowContext: Temporal の Workflow の context。
//   - input: 作成する service の ID。
//
// 戻り値:
//   - 作成に失敗したときのエラー(service は failed にしてある)。failed にもできなかったときは、両方の原因を含む。
func CreateServiceWorkflow(workflowContext sdkworkflow.Context, input sharedworkflow.CreateServiceInput) error {
	// ===== 1. k8s の Service を作る =====
	ensureContext := sdkworkflow.WithActivityOptions(workflowContext, infrastructureOptions())
	var created sharedworkflow.EnsureServiceResult
	ensureError := sdkworkflow.ExecuteActivity(ensureContext, sharedworkflow.EnsureServiceActivityName,
		sharedworkflow.EnsureServiceInput{ServiceID: input.ServiceID}).Get(ensureContext, &created)
	if ensureError != nil {
		ensureError = fmt.Errorf("Service を作れませんでした: %w", ensureError)
	}

	// ===== 2. 結果を記録する(キャンセルされていても記録できるよう、切り離した context で) =====
	disconnectedContext, cancelDisconnected := sdkworkflow.NewDisconnectedContext(workflowContext)
	defer cancelDisconnected()
	recordContext := sdkworkflow.WithActivityOptions(disconnectedContext, compensationOptions())
	markError := sdkworkflow.ExecuteActivity(recordContext, sharedworkflow.MarkServiceResultActivityName, sharedworkflow.MarkServiceResultInput{
		ServiceID: input.ServiceID, Succeeded: ensureError == nil, DNSName: created.DNSName, ClusterIP: created.ClusterIP,
	}).Get(recordContext, nil)
	// 記録に失敗したときは、作成の失敗と合わせて、両方の原因を返す(service は pending のまま残る)。
	if markError != nil {
		return errors.Join(ensureError, fmt.Errorf("作成の結果を記録できませんでした: %w", markError))
	}
	return ensureError
}
