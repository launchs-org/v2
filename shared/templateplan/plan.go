// Package templateplan は、テンプレートから部品を作る「計画」(docs/templates.md の段階 C)の型を持つ。
//
// 計画は、変数を展開し終えた、作る部品の一覧で、ステップの順に並ぶ。バックエンド(受け付ける側)が作って、
// template_applies テーブルに保存し、ワーカーの Workflow が読んで、順に実行する。どちらからも使うので、shared に置く。
package templateplan

import (
	"strings"

	"launchs/shared/model"
)

// ステップの種類。
const (
	StepVolume    = "volume"
	StepConfigMap = "configmap"
	StepDeploy    = "deploy"
	StepService   = "service"
	StepIngress   = "ingress"
)

// ステップの進み具合。
const (
	StatusWaiting  = "waiting"
	StatusCreating = "creating"
	StatusDone     = "done"
	StatusFailed   = "failed"
)

// DNSTokenPrefix は、${service.X.dns} の、決まる前の目印の先頭。目印は、この実行だけの、ランダムな部分を含む。
const DNSTokenPrefix = "@@dns-"

// Plan は、テンプレート 1 回分の、作る部品の計画。
type Plan struct {
	// ProjectName は、${project.name} に入れた、プロジェクトの名前。
	ProjectName string `json:"projectName"`

	// DNSToken は、この実行だけの、ランダムな文字列。${service.X.dns} の目印に使う(利用者の入力と、ぶつからない)。
	DNSToken string `json:"dnsToken"`

	// Hosts は、ingress のホスト名の候補(重なったら、次を使う)。
	Hosts []string `json:"hosts"`

	// Steps は、作る順に並んだステップ。
	Steps []Step `json:"steps"`
}

// Step は、作る部品 1 つ。Kind によって、使う項目が違う。
type Step struct {
	// Kind は、StepVolume などのどれか。
	Kind string `json:"kind"`

	// Name は、テンプレートの中の部品の name(service は、その deploy の name。ingress は "ingress")。
	Name string `json:"name"`

	// ID は、作る部品の ID(計画のときに決める。再実行しても、同じものを、二重に作らないため)。
	ID string `json:"id"`

	Volume    *VolumePlan    `json:"volume,omitempty"`
	ConfigMap *ConfigMapPlan `json:"configmap,omitempty"`
	Deploy    *DeployPlan    `json:"deploy,omitempty"`
	Service   *ServicePlan   `json:"service,omitempty"`
	Ingress   *IngressPlan   `json:"ingress,omitempty"`
}

// VolumePlan は、volume の計画。
type VolumePlan struct {
	// SizeMB は、大きさ(MB)。
	SizeMB int `json:"sizeMB"`
}

// ConfigMapPlan は、ConfigMap の計画(版 1 のファイル。key の昇順)。
type ConfigMapPlan struct {
	// Files は、ファイル(text の内容は、展開済み)。
	Files []model.ConfigMapFile `json:"files"`
}

// MountPlan は、deploy が、同じ計画の中の volume を、マウントする指定。
type MountPlan struct {
	// Volume は、volume の name。
	Volume string `json:"volume"`

	// Path は、マウント先。
	Path string `json:"path"`
}

// ConfigMapMountPlan は、deploy が、同じ計画の中の ConfigMap の 1 ファイルを、マウントする指定。
type ConfigMapMountPlan struct {
	// ConfigMap は、configmap の name。
	ConfigMap string `json:"configmap"`

	// Key は、ファイル名。
	Key string `json:"key"`

	// Path は、マウント先。
	Path string `json:"path"`

	// Mode は、権限(8 進数 4 桁)。
	Mode string `json:"mode"`
}

// DeployPlan は、deploy の計画。文字列の値の中の ${service.X.dns} は、目印になっている(作る直前に、DNS 名に置き換える)。
type DeployPlan struct {
	Image      string               `json:"image"`
	Spec       string               `json:"spec"`
	Replicas   int                  `json:"replicas"`
	Env        map[string]string    `json:"env"`
	FSGroup    int                  `json:"fsGroup,omitempty"`
	Command    string               `json:"command"`
	Args       []string             `json:"args"`
	Mounts     []MountPlan          `json:"mounts"`
	Files      []model.FileSpec     `json:"files"`
	ConfigMaps []ConfigMapMountPlan `json:"configmaps"`
}

// ServicePlan は、deploy の service の計画。
type ServicePlan struct {
	// Deploy は、公開する deploy の name。
	Deploy string `json:"deploy"`

	// Ports は、公開するポート。
	Ports []model.ServicePort `json:"ports"`
}

// IngressRulePlan は、ingress のルール 1 つ(転送先は、deploy の name)。
type IngressRulePlan struct {
	Path   string `json:"path"`
	Deploy string `json:"deploy"`
	Port   int    `json:"port"`
}

// IngressPlan は、ingress の計画。
type IngressPlan struct {
	// Rules は、ルール。
	Rules []IngressRulePlan `json:"rules"`
}

// StepState は、ステップ 1 つの進み具合(template_applies.steps に、計画と同じ順で入る)。
type StepState struct {
	// Kind と Name は、計画のステップと同じ。
	Kind string `json:"kind"`
	Name string `json:"name"`

	// Status は、StatusWaiting、StatusCreating、StatusDone、StatusFailed のどれか。
	Status string `json:"status"`

	// ResourceID は、作った部品の ID(作り始めたら入る)。
	ResourceID string `json:"resourceId"`

	// Error は、失敗の理由(失敗したときだけ)。
	Error string `json:"error"`
}

// DNSToken は、ある deploy の service の DNS 名の、目印を返す。
//
// 引数:
//   - random: この実行だけの、ランダムな文字列(Plan.DNSToken)。
//   - deployName: deploy の name。
//
// 戻り値:
//   - 「@@dns-<ランダム>-<deploy の name>@@」の形の目印。
func DNSToken(random string, deployName string) string {
	// 先頭、ランダムな部分、名前を、つなぐ。
	return DNSTokenPrefix + random + "-" + deployName + "@@"
}

// ReplaceDNS は、文字列の中の、DNS 名の目印を、DNS 名に置き換える。この実行の目印だけを置き換える。
//
// 引数:
//   - text: 元の文字列。
//   - random: この実行だけの、ランダムな文字列。
//   - dnsByDeploy: deploy の name から、DNS 名への対応。
//
// 戻り値:
//   - 置き換えた文字列。
func ReplaceDNS(text string, random string, dnsByDeploy map[string]string) string {
	// 対応がある deploy の目印を、1 つずつ置き換える。
	for deployName, dns := range dnsByDeploy {
		text = strings.ReplaceAll(text, DNSToken(random, deployName), dns)
	}
	return text
}

// HasUnresolvedDNS は、文字列に、この実行の目印が、まだ残っているかを返す(置き換えのあとの、確認用)。
//
// 引数:
//   - text: 調べる文字列。
//   - random: この実行だけの、ランダムな文字列。
//
// 戻り値:
//   - 残っていれば true。
func HasUnresolvedDNS(text string, random string) bool {
	// この実行の目印の、先頭の部分があるか。
	return strings.Contains(text, DNSTokenPrefix+random+"-")
}

// StepIndex は、計画の中で、種類と name が一致するステップの位置を返す。
//
// 引数:
//   - kind: ステップの種類。
//   - name: ステップの name。
//
// 戻り値:
//   - 位置。無ければ -1。
func (plan Plan) StepIndex(kind string, name string) int {
	// 順に探す。
	for index, step := range plan.Steps {
		if step.Kind == kind && step.Name == name {
			return index
		}
	}
	return -1
}

// IDOf は、計画の中の、部品の ID を返す。
//
// 引数:
//   - kind: ステップの種類。
//   - name: ステップの name。
//
// 戻り値:
//   - ID。無ければ空。
func (plan Plan) IDOf(kind string, name string) string {
	// 位置を探して、その ID を返す。
	index := plan.StepIndex(kind, name)
	if index < 0 {
		return ""
	}
	return plan.Steps[index].ID
}

// InitialStates は、全てのステップが「待機中」の、進み具合を作る。
//
// 戻り値:
//   - 計画と同じ順の、進み具合。
func (plan Plan) InitialStates() []StepState {
	// ステップごとに、待機中の状態を作る。
	states := make([]StepState, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		states = append(states, StepState{Kind: step.Kind, Name: step.Name, Status: StatusWaiting})
	}
	return states
}
