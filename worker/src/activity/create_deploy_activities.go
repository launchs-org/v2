package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// defaultDeploymentPollInterval は、Deployment の起動を確かめる、既定の間隔。
const defaultDeploymentPollInterval = 2 * time.Second

// DeploymentDefinition は、k8s に作る Deployment の内容。
//
// DB の deploy と、スペックのマスターデータから組み立てる。k8s の型を、Activity に持ち込まないための入れ物。
type DeploymentDefinition struct {
	// Namespace は、Deployment を作る namespace(プロジェクトの ID)。
	Namespace string

	// Name は、Deployment の名前(deploy の ID)。
	Name string

	// Image は、起動するイメージ。
	Image string

	// Replicas は、希望するレプリカ数。
	Replicas int32

	// CPUMillicores は、CPU の大きさ(ミリコア)。requests と limits の両方に使う。
	CPUMillicores int

	// MemoryMiB は、メモリの大きさ(MiB)。requests と limits の両方に使う。
	MemoryMiB int

	// Env は、環境変数。
	Env map[string]string

	// Command は、起動コマンド。nil ならイメージの既定。
	Command *string

	// Args は、起動引数。nil ならイメージの既定。
	Args []string
}

// DeploymentOperator は、k8s の namespace と Deployment を操作する。
//
// 実体は、client-go で操作する KubernetesDeploymentOperator(worker/src/cluster)。
// k8s につながない設定(KUBERNETES_ACCESS=none)のときは、LoggingDeploymentOperator を使う。
type DeploymentOperator interface {
	// EnsureNamespace は、namespace が無ければ作る。すでにあれば何もしない(冪等)。
	EnsureNamespace(requestContext context.Context, namespaceName string) error

	// ApplyDeployment は、Deployment を作る。すでにあれば内容を更新する(冪等)。
	ApplyDeployment(requestContext context.Context, definition DeploymentDefinition) error

	// DeploymentReady は、Deployment が、希望するレプリカ数まで起動し終えているかを返す。
	DeploymentReady(requestContext context.Context, namespaceName string, deploymentName string) (bool, error)
}

// DeployStore は、CreateDeployActivities が必要とする、deploy の保存先の操作。
//
// 実体は shared/repository の DeployRepository。
type DeployStore interface {
	// FindForWorker は、持ち主を問わずに、deploy を1件取得する。
	FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error)

	// FindSpec は、スペックの名前から、CPU とメモリの大きさを取得する。
	FindSpec(requestContext context.Context, specName string) (model.DeploySpec, error)

	// UpdateStatus は、期待する状態のときだけ、状態を変える。変えられたかを返す。
	UpdateStatus(requestContext context.Context, deployID string, fromStatus string, toStatus string) (bool, error)
}

// CreateDeployActivities は、deploy を k8s に作って起動を確かめる Workflow が呼ぶ Activity をまとめる。
type CreateDeployActivities struct {
	deployStore        DeployStore
	deploymentOperator DeploymentOperator
	appLogger          *slog.Logger
	// deploymentPollInterval は、Deployment の起動を確かめる間隔。
	deploymentPollInterval time.Duration
}

// NewCreateDeployActivities は、CreateDeployActivities を作る。
//
// 引数:
//   - deployStore: deploy の保存先。
//   - deploymentOperator: k8s の namespace と Deployment を操作するもの。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した CreateDeployActivities。
func NewCreateDeployActivities(deployStore DeployStore, deploymentOperator DeploymentOperator, appLogger *slog.Logger) *CreateDeployActivities {
	// 依存するものを持たせて返す。
	return &CreateDeployActivities{
		deployStore:            deployStore,
		deploymentOperator:     deploymentOperator,
		appLogger:              appLogger,
		deploymentPollInterval: defaultDeploymentPollInterval,
	}
}

// EnsureProjectNamespace は、プロジェクトの k8s の namespace を、無ければ作る。
//
// namespace の名前は projectId と同じ。プロジェクトの最初の deploy のときに作られ、
// 2つ目以降は、すでにあるので何もしない。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: namespace を用意するプロジェクトの ID。
//
// 戻り値:
//   - namespace を作れなかったとき、または名前の形が不正なときのエラー(後者はリトライしない)。
func (createDeployActivities *CreateDeployActivities) EnsureProjectNamespace(requestContext context.Context, input workflow.EnsureProjectNamespaceInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	createDeployActivities.appLogger.Info("プロジェクトの namespace を用意する",
		"project_id", input.ProjectID, "activity", workflow.EnsureProjectNamespaceActivityName)
	// 用意は、k8s を操作するものに任せる。名前の形の不正は、リトライしても無駄なので、その印を付ける。
	return markNonRetryable(createDeployActivities.deploymentOperator.EnsureNamespace(requestContext, input.ProjectID))
}

// ApplyDeployment は、deploy に対応する k8s の Deployment を作る。すでにあれば更新する。
//
// DB から deploy とスペックを読み、k8s に作る Deployment の内容を組み立てて、k8s を操作するものに渡す。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: Deployment を作る deploy の ID。
//
// 戻り値:
//   - deploy を読めなかったとき、内容を組み立てられなかったとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (createDeployActivities *CreateDeployActivities) ApplyDeployment(requestContext context.Context, input workflow.ApplyDeploymentInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	createDeployActivities.appLogger.Info("Deployment を作る",
		"deploy_id", input.DeployID, "activity", workflow.ApplyDeploymentActivityName)

	// DB から deploy とスペックを読む。
	deploy, findError := createDeployActivities.deployStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}
	spec, specError := createDeployActivities.deployStore.FindSpec(requestContext, deploy.Spec)
	if specError != nil {
		return specError
	}

	// k8s に作る Deployment の内容を組み立てる。DB の値が壊れていたら、何度やっても同じなのでリトライしない。
	definition, definitionError := newDeploymentDefinition(deploy, spec)
	if definitionError != nil {
		return temporal.NewNonRetryableApplicationError(definitionError.Error(), "InvalidDeployment", definitionError)
	}
	// 作成は、k8s を操作するものに任せる。
	return createDeployActivities.deploymentOperator.ApplyDeployment(requestContext, definition)
}

// WaitDeploymentReady は、k8s の Deployment が起動し終える(Ready になる)まで待つ。
//
// 一定の間隔で確かめ、待つ間は、Temporal に生きていることを伝える。Activity の実行時間の上限
// (Workflow が決める。仕様では300秒)までに Ready にならなければ、タイムアウトで失敗する。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 起動を待つ deploy の ID。
//
// 戻り値:
//   - 期限までに Ready にならなかったとき、または k8s に問い合わせられなかったときのエラー。
func (createDeployActivities *CreateDeployActivities) WaitDeploymentReady(requestContext context.Context, input workflow.WaitDeploymentReadyInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	createDeployActivities.appLogger.Info("Deployment の起動を待つ",
		"deploy_id", input.DeployID, "activity", workflow.WaitDeploymentReadyActivityName)

	// namespace の名前(プロジェクトの ID)を知るため、deploy を読む。
	deploy, findError := createDeployActivities.deployStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}

	// Ready になるまで、間隔を空けて確かめ続ける。
	for {
		// 起動し終えているかを確かめる。
		ready, readyError := createDeployActivities.deploymentOperator.DeploymentReady(requestContext, deploy.ProjectID, deploy.ID)
		if readyError != nil {
			return readyError
		}
		// 終えていれば、完了。
		if ready {
			return nil
		}
		// まだなので、生きていることを伝えて、少し待つ(キャンセルや期限なら、そこで終わる)。
		activity.RecordHeartbeat(requestContext)
		select {
		case <-requestContext.Done():
			return requestContext.Err()
		case <-time.After(createDeployActivities.deploymentPollInterval):
		}
	}
}

// MarkDeployResult は、起動の結果を、deploy の状態として DB に記録する。
//
// pending の deploy だけを変える(別の処理が先に状態を変えていたら、上書きしない)。
//   - 成功: active(replicas が 0 なら scaled_zero)
//   - 失敗: failed
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 結果を記録する deploy の ID と、成功したか。
//
// 戻り値:
//   - deploy を読めなかったとき、または状態を更新できなかったときのエラー。
func (createDeployActivities *CreateDeployActivities) MarkDeployResult(requestContext context.Context, input workflow.MarkDeployResultInput) error {
	// 結果をログに残す(失敗は、運用で気づけるよう警告にする)。
	if input.Succeeded {
		createDeployActivities.appLogger.Info("deploy の起動に成功した", "deploy_id", input.DeployID, "activity", workflow.MarkDeployResultActivityName)
	} else {
		createDeployActivities.appLogger.Warn("deploy の起動に失敗した", "deploy_id", input.DeployID, "activity", workflow.MarkDeployResultActivityName)
	}

	// 記録する状態を決める。成功なら、replicas に合わせて active か scaled_zero にする。
	targetStatus := model.DeployStatusFailed
	if input.Succeeded {
		deploy, findError := createDeployActivities.deployStore.FindForWorker(requestContext, input.DeployID)
		if findError != nil {
			return findError
		}
		targetStatus = model.DeployStatusActive
		if deploy.Replicas == 0 {
			targetStatus = model.DeployStatusScaledZero
		}
	}

	// pending のときだけ、状態を変える(変えられなくても、別の処理が先に決めたということなので、エラーにしない)。
	_, updateError := createDeployActivities.deployStore.UpdateStatus(requestContext, input.DeployID, model.DeployStatusPending, targetStatus)
	return updateError
}

// newDeploymentDefinition は、DB の deploy とスペックから、k8s に作る Deployment の内容を組み立てる。
//
// 引数:
//   - deploy: DB の deploy。
//   - spec: deploy のスペック(CPU とメモリの大きさ)。
//
// 戻り値:
//   - Deployment の内容。
//   - 環境変数や引数の JSON が、期待する形(文字列のオブジェクト、文字列の配列)でないときのエラー。
func newDeploymentDefinition(deploy model.Deploy, spec model.DeploySpec) (DeploymentDefinition, error) {
	// 環境変数(文字列のオブジェクト)を読む。DB の列が空(NULL)なら、環境変数なし。
	environment := map[string]string{}
	if len(deploy.Env) > 0 {
		if unmarshalError := json.Unmarshal(deploy.Env, &environment); unmarshalError != nil {
			return DeploymentDefinition{}, fmt.Errorf("環境変数を読み取れません: %w", unmarshalError)
		}
	}
	// 起動引数(文字列の配列)を読む。NULL のときは、イメージの既定のまま(nil)にする。
	var arguments []string
	if len(deploy.Args) > 0 && string(deploy.Args) != "null" {
		if unmarshalError := json.Unmarshal(deploy.Args, &arguments); unmarshalError != nil {
			return DeploymentDefinition{}, fmt.Errorf("起動引数を読み取れません: %w", unmarshalError)
		}
	}
	// 組み立てて返す。
	return DeploymentDefinition{
		Namespace:     deploy.ProjectID,
		Name:          deploy.ID,
		Image:         deploy.ImageRef,
		Replicas:      int32(deploy.Replicas),
		CPUMillicores: spec.CPUMillicores,
		MemoryMiB:     spec.MemoryMiB,
		Env:           environment,
		Command:       deploy.Command,
		Args:          arguments,
	}, nil
}

// LoggingDeploymentOperator は、k8s を操作せず、ログに出すだけの DeploymentOperator。
//
// KUBERNETES_ACCESS=none のときの、開発用の仮の実装。起動を確かめずに「Ready」と答えるため、
// deploy は、実際には何も動いていなくても active になる。本番では使わない。
type LoggingDeploymentOperator struct {
	appLogger *slog.Logger
}

// NewLoggingDeploymentOperator は、LoggingDeploymentOperator を作る。
//
// 引数:
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した LoggingDeploymentOperator。
func NewLoggingDeploymentOperator(appLogger *slog.Logger) *LoggingDeploymentOperator {
	// ロガーを持たせて返す。
	return &LoggingDeploymentOperator{appLogger: appLogger}
}

// EnsureNamespace は、何も作らず、警告のログを出すだけ。
//
// 引数:
//   - requestContext: 使わない。
//   - namespaceName: 作る予定の namespace の名前。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingDeploymentOperator) EnsureNamespace(_ context.Context, namespaceName string) error {
	// 実際には作っていないことが分かるよう、警告で出す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、namespace は作らない(仮の実装)", "namespace", namespaceName)
	return nil
}

// ApplyDeployment は、何も作らず、警告のログを出すだけ。
//
// 引数:
//   - requestContext: 使わない。
//   - definition: 作る予定の Deployment の内容。
//
// 戻り値:
//   - 常に nil。
func (loggingOperator *LoggingDeploymentOperator) ApplyDeployment(_ context.Context, definition DeploymentDefinition) error {
	// 実際には作っていないことが分かるよう、警告で出す。
	loggingOperator.appLogger.Warn("k8s に未接続のため、Deployment は作らない(仮の実装)", "namespace", definition.Namespace, "deployment", definition.Name)
	return nil
}

// DeploymentReady は、仮の実装なので、常に Ready を返す。
//
// 引数:
//   - requestContext: 使わない。
//   - namespaceName: 使わない。
//   - deploymentName: 使わない。
//
// 戻り値:
//   - 常に true と nil。
func (loggingOperator *LoggingDeploymentOperator) DeploymentReady(_ context.Context, _ string, _ string) (bool, error) {
	// 何も作っていないので待つ必要は無く、常に Ready として扱う。
	return true, nil
}
