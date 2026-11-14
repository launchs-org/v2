package activity

import (
	"context"
	"launchs/shared/model"
	"launchs/shared/workflow"
	"log/slog"
	"time"

	"go.temporal.io/sdk/temporal"
)

// defaultDeploymentPollInterval は、Deployment の起動を確かめる、既定の間隔。
const defaultDeploymentPollInterval = 2 * time.Second

// DeployStore は、CreateDeployActivities が必要とする、deploy の保存先の操作。
//
// 実体は shared/repository の DeployRepository。
type DeployStore interface {
	// FindForWorker は、持ち主を問わずに、deploy を1件取得する。
	FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error)

	// FindSpec は、スペックの名前から、CPU とメモリの大きさを取得する。
	FindSpec(requestContext context.Context, specName string) (model.DeploySpec, error)

	// CompleteCreation は、作成の起動の結果を確定する(pending の deploy だけ)。確定できたかを返す。
	CompleteCreation(requestContext context.Context, deployID string, succeeded bool) (bool, error)
}

// CreateDeployActivities は、deploy を k8s に作って起動を確かめる Workflow が呼ぶ Activity をまとめる。
type CreateDeployActivities struct {
	deployStore        DeployStore
	deploymentOperator DeploymentOperator
	observationReader  ObservationReader
	appLogger          *slog.Logger
	// deploymentPollInterval は、Deployment の起動を確かめる間隔。
	deploymentPollInterval time.Duration
}

// NewCreateDeployActivities は、CreateDeployActivities を作る。
//
// 引数:
//   - deployStore: deploy の保存先。
//   - deploymentOperator: k8s の namespace と Deployment を操作するもの。
//   - observationReader: watcher が DB に書いた観測値の読み取り先(起動待ちに使う)。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した CreateDeployActivities。
func NewCreateDeployActivities(deployStore DeployStore, deploymentOperator DeploymentOperator, observationReader ObservationReader, appLogger *slog.Logger) *CreateDeployActivities {
	// 依存するものを持たせて返す。
	return &CreateDeployActivities{
		deployStore:            deployStore,
		deploymentOperator:     deploymentOperator,
		observationReader:      observationReader,
		appLogger:              appLogger,
		deploymentPollInterval: defaultDeploymentPollInterval,
	}
}

// EnsureProjectNamespace は、deploy が属するプロジェクトの k8s の namespace を、無ければ作る。
//
// namespace の名前は projectId と同じ。プロジェクトの最初の deploy のときに作られ、
// 2つ目以降は、すでにあるので何もしない。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: これから起動する deploy の ID。
//
// 戻り値:
//   - deploy を読めなかったとき、namespace を作れなかったとき、名前の形が不正なとき(リトライしない)のエラー。
func (createDeployActivities *CreateDeployActivities) EnsureProjectNamespace(requestContext context.Context, input workflow.EnsureProjectNamespaceInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	createDeployActivities.appLogger.Info("プロジェクトの namespace を用意する",
		"deploy_id", input.DeployID, "activity", workflow.EnsureProjectNamespaceActivityName)

	// deploy から、属するプロジェクトの ID(namespace の名前)を調べる。
	deploy, findError := createDeployActivities.deployStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}
	// 用意は、k8s を操作するものに任せる。名前の形の不正は、リトライしても無駄なので、その印を付ける。
	return markNonRetryable(createDeployActivities.deploymentOperator.EnsureNamespace(requestContext, deploy.ProjectID))
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
//   - 更新後の Deployment の generation(設定の世代)。起動を待つときの下限になる。
//   - deploy を読めなかったとき、内容を組み立てられなかったとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (createDeployActivities *CreateDeployActivities) ApplyDeployment(requestContext context.Context, input workflow.ApplyDeploymentInput) (int64, error) {
	// どの Activity が何のために動いているかを、ログに残す。
	createDeployActivities.appLogger.Info("Deployment を作る",
		"deploy_id", input.DeployID, "activity", workflow.ApplyDeploymentActivityName)

	// DB から deploy とスペックを読む。
	deploy, findError := createDeployActivities.deployStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return 0, findError
	}
	spec, specError := createDeployActivities.deployStore.FindSpec(requestContext, deploy.Spec)
	if specError != nil {
		return 0, specError
	}

	// k8s に作る Deployment の内容を組み立てる。DB の値が壊れていたら、何度やっても同じなのでリトライしない。
	definition, definitionError := newDeploymentDefinition(deploy, spec)
	if definitionError != nil {
		return 0, temporal.NewNonRetryableApplicationError(definitionError.Error(), "InvalidDeployment", definitionError)
	}
	// 作成は、k8s を操作するものに任せる。
	return createDeployActivities.deploymentOperator.ApplyDeployment(requestContext, definition)
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

	// 結果の確定は、deploy(モデル)が行う(成功なら replicas に合わせて active か scaled_zero、失敗なら failed)。
	// pending のときだけ変わる(変えられなくても、別の処理が先に決めたということなので、エラーにしない)。
	_, updateError := createDeployActivities.deployStore.CompleteCreation(requestContext, input.DeployID, input.Succeeded)
	return updateError
}
