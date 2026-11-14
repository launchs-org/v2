package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"launchs/shared/model"
)

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
	// 戻り値は、更新後の Deployment の generation(設定の世代)。起動を待つときに、この世代以上が処理されるのを待つ。
	ApplyDeployment(requestContext context.Context, definition DeploymentDefinition) (int64, error)

	// DeleteDeployment は、Deployment を削除する(Pod も一緒に消える)。無ければ何もしない(冪等)。
	DeleteDeployment(requestContext context.Context, namespaceName string, deploymentName string) error

	// DeploymentReady は、Deployment が、希望するレプリカ数まで起動し終えているかを返す。
	DeploymentReady(requestContext context.Context, namespaceName string, deploymentName string) (bool, error)
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
