package workflow

import (
	"time"

	"github.com/google/uuid"
)

const (
	// CreateDeployWorkflowName は、deploy を k8s に作って、起動を確かめる Workflow の名前。
	CreateDeployWorkflowName = "CreateDeployWorkflow"

	// EnsureProjectNamespaceActivityName は、プロジェクトの k8s の namespace を(無ければ)作る Activity の名前。
	EnsureProjectNamespaceActivityName = "EnsureProjectNamespace"

	// ApplyDeploymentActivityName は、deploy に対応する k8s の Deployment を作る(または更新する) Activity の名前。
	ApplyDeploymentActivityName = "ApplyDeployment"

	// WaitDeploymentReadyActivityName は、k8s の Deployment が起動し終えるのを待つ Activity の名前。
	WaitDeploymentReadyActivityName = "WaitDeploymentReady"

	// MarkDeployResultActivityName は、起動の結果(active または failed)を DB に記録する Activity の名前。
	MarkDeployResultActivityName = "MarkDeployResult"

	// DeployStartTimeout は、deploy の起動(Ready になるまで)を待つ最長の時間。超えたら failed にする。
	// 仕様の applying のタイムアウト(300秒)と同じ値。
	DeployStartTimeout = 300 * time.Second

	// DeployInfrastructureRetryWindow は、k8s への依頼(namespace と Deployment の作成)を、失敗してもリトライし続ける最長の時間(仮)。
	DeployInfrastructureRetryWindow = 2 * time.Minute

	// CreateDeployWorkflowTimeout は、deploy 作成の Workflow 全体の最長の実行時間(仮)。
	CreateDeployWorkflowTimeout = 15 * time.Minute
)

// CreateDeployInput は、CreateDeployWorkflow の入力。
type CreateDeployInput struct {
	// DeployID は、起動する deploy の ID。deploy の内容は、Activity が DB から読む。
	DeployID string
}

// PrepareDeployDependenciesActivityName は、起動の前に、マウントとファイルに必要なもの(PVC、ConfigMap)を用意する Activity の名前。
const PrepareDeployDependenciesActivityName = "PrepareDeployDependencies"

// PrepareDeployDependenciesInput は、PrepareDeployDependencies Activity の入力。
type PrepareDeployDependenciesInput struct {
	// DeployID は、起動する deploy の ID。
	DeployID string
}

// EnsureProjectNamespaceInput は、EnsureProjectNamespace Activity の入力。
type EnsureProjectNamespaceInput struct {
	// DeployID は、これから起動する deploy の ID。属するプロジェクトの namespace を用意する。
	// プロジェクトの ID は、Activity が DB から読む(Workflow は deploy の ID だけを知っていればよい)。
	DeployID string
}

// ApplyDeploymentInput は、ApplyDeployment Activity の入力。
type ApplyDeploymentInput struct {
	// DeployID は、k8s の Deployment を作る deploy の ID。
	DeployID string
}

// WaitDeploymentReadyInput は、WaitDeploymentReady Activity の入力。
type WaitDeploymentReadyInput struct {
	// DeployID は、起動を待つ deploy の ID。
	DeployID string

	// MinGeneration は、Deployment の設定の世代の下限。k8s がこの世代以上を処理し終えるまで待つ。
	// 更新の直後に、古い設定の観測値(すでに Ready)を見て、起動し終えたと誤解しないために使う。
	MinGeneration int64
}

// MarkDeployResultInput は、MarkDeployResult Activity の入力。
type MarkDeployResultInput struct {
	// DeployID は、結果を記録する deploy の ID。
	DeployID string

	// Succeeded は、起動に成功したか。true なら active(replicas が 0 なら scaled_zero)、false なら failed にする。
	Succeeded bool
}

// NewCreateDeployWorkflowID は、deploy 作成の Workflow ID を作る。
//
// 形式は `{操作}-{リソースID}-{uuid}`(ハイフン区切り)。
//
// 引数:
//   - deployID: 起動する deploy の ID。
//
// 戻り値:
//   - Workflow ID。
func NewCreateDeployWorkflowID(deployID string) string {
	// 操作ごとに一意にするため、末尾に UUID を付ける。
	return "create-" + deployID + "-" + uuid.NewString()
}
