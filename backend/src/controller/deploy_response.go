package controller

import (
	"encoding/json"
	"launchs/shared/jst"
	"launchs/shared/model"
)

// PodResponse は、deploy の Pod 1つのレスポンス。
type PodResponse struct {
	// PodID は、Pod の ID。
	PodID string `json:"podId"`

	// Status は、Pod の状態(pending、running、terminating のどれか)。
	Status string `json:"status"`
}

// DeployConfigResponse は、deploy の起動設定のレスポンス(現在の値と、まだ適用していない値)。
type DeployConfigResponse struct {
	// Env は、現在コンテナに反映されている環境変数。
	Env json.RawMessage `json:"env"`

	// PendingEnv は、まだ適用していない環境変数。変更が無ければ null。
	PendingEnv json.RawMessage `json:"pending_env"`

	// Command は、現在の起動コマンド。null ならイメージの既定。
	Command *string `json:"command"`

	// PendingCommand は、まだ適用していない起動コマンド。
	PendingCommand *string `json:"pending_command"`

	// Args は、現在の起動引数。null ならイメージの既定。
	Args json.RawMessage `json:"args"`

	// PendingArgs は、まだ適用していない起動引数。
	PendingArgs json.RawMessage `json:"pending_args"`
}

// DeployVolumesResponse は、deploy のボリュームのマウント設定のレスポンス。
type DeployVolumesResponse struct {
	// Mounts は、現在のマウント。
	Mounts []MountResponse `json:"mounts"`

	// PendingMounts は、まだ適用していないマウント。変更が無ければ null。
	PendingMounts []MountResponse `json:"pending_mounts"`
}

// DeployResponse は、deploy 詳細のレスポンスのボディ。
type DeployResponse struct {
	// DeployID は、deploy の ID。
	DeployID string `json:"deployId"`

	// ProjectID は、deploy が属するプロジェクトの ID(画面が、プロジェクトの volume などを引くために使う)。
	ProjectID string `json:"projectId"`

	// Status は、deploy の状態。
	Status string `json:"status"`

	// Spec は、スペックの名前。
	Spec string `json:"spec"`

	// Replicas は、希望するレプリカ数。
	Replicas int `json:"replicas"`

	// Pods は、いま存在する Pod の一覧(watcher が DB に書いた観測値。作られた順)。無ければ空の配列。
	Pods []PodResponse `json:"pods"`

	// LogPods は、ログのある Pod の一覧(終了した Pod も含む。新しい Pod が先。最大 50 件)。GET /deploy/:deployId/logs の podId に使える。無ければ空の配列。
	LogPods []LogPodResponse `json:"logPods"`

	// ImageRef は、起動しているイメージ。
	ImageRef string `json:"imageRef"`

	// CreatedAt は、作成日時(JST)。
	CreatedAt string `json:"createdAt"`

	// Config は、起動設定。
	Config DeployConfigResponse `json:"config"`

	// Volumes は、ボリュームのマウント設定。ボリュームの機能ができるまでは、空のマウントを返す。
	Volumes DeployVolumesResponse `json:"volumes"`

	// Files は、deploy のファイル(設定ファイルなど)。現在のファイルと、まだ適用していないファイル。
	Files FilesResponse `json:"files"`

	// Service は、deploy に紐づく service。まだ作っていなければ null(controller が、設定する)。
	Service any `json:"service"`
}

// newDeployResponse は、モデルを、レスポンスの形にする。
//
// JSON の列(環境変数など)は、中身を解釈せず、そのまま出す。値が無い列は null になる。
//
// 引数:
//   - deploy: deploy のモデル。
//
// 戻り値:
//   - レスポンスのボディ。
func newDeployResponse(deploy model.Deploy) DeployResponse {
	// 現在のマウント。無ければ、null ではなく、空の配列にする。
	mounts := mountResponses(deploy.Mounts)
	if mounts == nil {
		mounts = []MountResponse{}
	}
	// 適用前のマウント。変更が無ければ null、空の配列は「全て外す変更」として、そのまま出す。
	var pendingMounts []MountResponse
	if deploy.HasPendingMounts() {
		pendingMounts = mountResponses(deploy.PendingMounts)
		if pendingMounts == nil {
			pendingMounts = []MountResponse{}
		}
	}
	return DeployResponse{
		DeployID:  deploy.ID,
		ProjectID: deploy.ProjectID,
		Status:    deploy.Status,
		Spec:      deploy.Spec,
		Replicas:  deploy.Replicas,
		// Pod の一覧は、呼び出し側が設定する(config だけを返す API では使わない)。ここでは、空の配列(null にしない)。
		Pods:      []PodResponse{},
		LogPods:   []LogPodResponse{},
		ImageRef:  deploy.ImageRef,
		CreatedAt: jst.Format(deploy.CreatedAt),
		Config: DeployConfigResponse{
			Env:            jsonOrNull(deploy.Env),
			PendingEnv:     jsonOrNull(deploy.PendingEnv),
			Command:        deploy.Command,
			PendingCommand: deploy.PendingCommand,
			Args:           jsonOrNull(deploy.Args),
			PendingArgs:    jsonOrNull(deploy.PendingArgs),
		},
		Volumes: DeployVolumesResponse{Mounts: mounts, PendingMounts: pendingMounts},
		Files:   newFilesResponse(deploy),
		Service: nil,
	}
}

// jsonOrNull は、JSON の列の値を、レスポンスにそのまま出せる形にする。値が無ければ JSON の null にする。
//
// 引数:
//   - column: JSON の列の値(NULL のときは長さ 0)。
//
// 戻り値:
//   - そのままの JSON。無ければ null。
func jsonOrNull(column []byte) json.RawMessage {
	// 値が無いとき(DB の NULL)は、JSON の null にする。
	if len(column) == 0 {
		return json.RawMessage("null")
	}
	// 値があるときは、そのまま出す。
	return json.RawMessage(column)
}

// podIDPrefix は、API に出す Pod の ID の接頭辞。ID は、この後ろに k8s の Pod の UID(UUID)を付けたもの。
const podIDPrefix = "pod-"

// newPodResponses は、Pod のモデルの一覧を、レスポンスの形にする。
//
// podId は「pod-」+ k8s の Pod の UID。Pod が作り直されると別の UID になるので、別の podId になる。
//
// 引数:
//   - pods: Pod のモデルの一覧。
//
// 戻り値:
//   - レスポンスの Pod の一覧。無ければ空の配列(null にしない)。
func newPodResponses(pods []model.DeployPod) []PodResponse {
	// モデルを1つずつ、レスポンスの形にする。
	responses := make([]PodResponse, 0, len(pods))
	for _, pod := range pods {
		responses = append(responses, PodResponse{PodID: podIDPrefix + pod.PodUID, Status: pod.Status})
	}
	return responses
}
