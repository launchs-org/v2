package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"launchs/shared/model"
	"sort"
	"strconv"
)

// MountDefinition は、Pod にマウントするボリューム1つ。
type MountDefinition struct {
	// ClaimName は、マウントする PVC の名前(volume の ID)。
	ClaimName string

	// MountPath は、コンテナの中のマウント先の path。
	MountPath string
}

// FileDefinition は、Pod にマウントするファイル1つ(deploy の「ファイル」。k8s の ConfigMap の 1 つのキーを、subPath でマウントする)。
type FileDefinition struct {
	// Key は、ConfigMap の中のキー(path から決めた、安全な名前: f0、f1 …)。
	Key string

	// Path は、コンテナの中のファイルの path。
	Path string
}

// ConfigMapMountDefinition は、Pod にマウントする、共有の ConfigMap のファイル1つ(k8s の ConfigMap の 1 つのキーを、subPath でマウントする)。
type ConfigMapMountDefinition struct {
	// ConfigMapName は、k8s の ConfigMap の名前(版ごとに作る、immutable な ConfigMap。"<configMapId>-r<revision>")。
	ConfigMapName string

	// Key は、ConfigMap の中のキー(ファイル名)。
	Key string

	// Path は、コンテナの中のファイルの path。
	Path string

	// Mode は、ファイルの権限(0o644 など)。
	Mode int32
}

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

	// FSGroup は、Pod のボリュームの所有グループ(fsGroup)。nil なら指定しない。
	FSGroup *int64

	// Mounts は、Pod にマウントするボリューム(PVC)。無ければ空。
	Mounts []MountDefinition

	// Args は、起動引数。nil ならイメージの既定。
	Args []string

	// Files は、Pod にマウントするファイル(ConfigMap。名前は Name と同じ)。無ければ空。
	Files []FileDefinition

	// ConfigMapMounts は、Pod にマウントする、共有の ConfigMap のファイル(path の順)。無ければ空。
	ConfigMapMounts []ConfigMapMountDefinition
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
	// マウント(現在反映されているもの)を読む。壊れていたら、起動できないので、エラーにする。
	parsedMounts, mountsError := model.ParseMounts(deploy.Mounts)
	if mountsError != nil {
		return DeploymentDefinition{}, fmt.Errorf("マウントを読み取れません: %w", mountsError)
	}
	mounts := make([]MountDefinition, 0, len(parsedMounts))
	for _, mount := range parsedMounts {
		mounts = append(mounts, MountDefinition{ClaimName: mount.VolumeID, MountPath: mount.MountPath})
	}
	// ファイル(現在反映されているもの)を読む。起動設定やマウントを適用するときも、現在のファイルを落とさないよう、必ず入れる。
	parsedFiles, filesError := model.ParseFiles(deploy.Files)
	if filesError != nil {
		return DeploymentDefinition{}, fmt.Errorf("ファイルを読み取れません: %w", filesError)
	}
	// 共有の ConfigMap のマウント(現在反映されているもの)を読む。他の適用でも落とさないよう、必ず入れる(path の順)。
	parsedConfigMapMounts, configMapMountsError := model.ParseConfigMapMounts(deploy.ConfigMapMounts)
	if configMapMountsError != nil {
		return DeploymentDefinition{}, fmt.Errorf("ConfigMap のマウントを読み取れません: %w", configMapMountsError)
	}
	configMapMounts := make([]ConfigMapMountDefinition, 0, len(parsedConfigMapMounts))
	for _, mount := range model.SortConfigMapMounts(parsedConfigMapMounts) {
		configMapMounts = append(configMapMounts, ConfigMapMountDefinition{
			ConfigMapName: model.ConfigMapRevisionResourceName(mount.ConfigMapID, mount.Revision), Key: mount.Key, Path: mount.Path, Mode: mount.ModeBits(),
		})
	}
	// ボリュームの所有グループ(テンプレートが指定したときだけ)。
	var fsGroup *int64
	if deploy.FSGroup != nil {
		group := int64(*deploy.FSGroup)
		fsGroup = &group
	}
	// 組み立てて返す。
	return DeploymentDefinition{
		FSGroup:         fsGroup,
		ConfigMapMounts: configMapMounts,
		Files:           newFileDefinitions(parsedFiles),
		Namespace:       deploy.ProjectID,
		Name:            deploy.ID,
		Image:           deploy.ImageRef,
		Replicas:        int32(deploy.Replicas),
		CPUMillicores:   spec.CPUMillicores,
		MemoryMiB:       spec.MemoryMiB,
		Env:             environment,
		Command:         deploy.Command,
		Args:            arguments,
		Mounts:          mounts,
	}, nil
}

// newFileDefinitions は、ファイルの構成から、Pod にマウントするファイルの一覧を作る。
// ConfigMap のキーは、path の順に f0、f1 … と付ける(path に使える文字は、キーには使えない文字を含むため、path から作らない)。
//
// 引数:
//   - files: ファイルの構成。
//
// 戻り値:
//   - マウントするファイルの一覧(path の順)。無ければ空。
func newFileDefinitions(files []model.FileSpec) []FileDefinition {
	// path の順に並べる(同じ内容なら、毎回、同じキーになるように)。
	sorted := append([]model.FileSpec(nil), files...)
	sort.Slice(sorted, func(first int, second int) bool { return sorted[first].Path < sorted[second].Path })
	// 順に、キーを付ける。
	definitions := make([]FileDefinition, 0, len(sorted))
	for index, file := range sorted {
		definitions = append(definitions, FileDefinition{Key: "f" + strconv.Itoa(index), Path: file.Path})
	}
	return definitions
}

// newConfigMapData は、ファイルの構成から、ConfigMap のデータ(キー → 内容)を作る。キーは、newFileDefinitions と同じ付け方。
//
// 引数:
//   - files: ファイルの構成。
//
// 戻り値:
//   - ConfigMap のデータ。
func newConfigMapData(files []model.FileSpec) map[string]string {
	// 同じ順(path の順)に、同じキーを付けて、内容を入れる。
	sorted := append([]model.FileSpec(nil), files...)
	sort.Slice(sorted, func(first int, second int) bool { return sorted[first].Path < sorted[second].Path })
	data := make(map[string]string, len(sorted))
	for index, file := range sorted {
		data["f"+strconv.Itoa(index)] = file.Content
	}
	return data
}

// ConfigMapOperator は、k8s の ConfigMap(deploy のファイルの置き場)を操作する。
//
// 実体は、client-go で操作する KubernetesConfigMapOperator(worker/src/cluster)。
// k8s につながない設定(KUBERNETES_ACCESS=none)のときは、LoggingConfigMapOperator を使う。
type ConfigMapOperator interface {
	// ApplyConfigMap は、ConfigMap を作る。すでにあれば内容を置き換える(冪等)。
	ApplyConfigMap(requestContext context.Context, namespaceName string, name string, data map[string]string) error

	// DeleteConfigMap は、ConfigMap を削除する。無ければ何もしない(冪等)。
	DeleteConfigMap(requestContext context.Context, namespaceName string, name string) error
}
