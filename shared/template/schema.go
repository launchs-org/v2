// Package template は、テンプレート(複数の部品と、入力の変数を、YAML で定義したもの。docs/templates.md)の、
// 読み込み、検証、変数の展開を持つ。どれも、データベースにも、k8s にも触らない(純粋な処理)。
//
// テンプレートから、実際に部品を作る処理(段階 C)は、ここには無い。
package template

import "strings"

// APIVersion と Kind は、テンプレートの YAML の、決まった値。
const (
	APIVersion = "launchs/v1"
	Kind       = "Template"
)

// 変数の型。
const (
	VariableString   = "string"
	VariableInteger  = "integer"
	VariablePassword = "password"
	VariableChoice   = "choice"
	VariableBoolean  = "boolean"
)

// 部品(resources)の種類。
const (
	ResourceVolume    = "volume"
	ResourceDeploy    = "deploy"
	ResourceIngress   = "ingress"
	ResourceConfigMap = "configmap"
)

// Template は、テンプレート1つ(YAML のファイル1つ)。
type Template struct {
	// APIVersion は、YAML の形の版。launchs/v1 だけ。
	APIVersion string `yaml:"apiVersion"`

	// Kind は、Template だけ。
	Kind string `yaml:"kind"`

	// Metadata は、名前や説明。
	Metadata Metadata `yaml:"metadata"`

	// Variables は、利用者が入力する値の定義。
	Variables []Variable `yaml:"variables"`

	// Resources は、作る部品(上から順に作る)。
	Resources []Resource `yaml:"resources"`
}

// Metadata は、テンプレートの、名前や説明。
type Metadata struct {
	// ID は、URL とファイル名に使う識別子(小文字の英数字とハイフン)。
	ID string `yaml:"id"`

	// Name は、画面に出す名前。
	Name string `yaml:"name"`

	// Description は、画面に出す説明。
	Description string `yaml:"description"`

	// Icon は、画面の絵の名前(決まった名前から選ぶ)。
	Icon string `yaml:"icon"`

	// Category は、絞り込みの分類。
	Category string `yaml:"category"`

	// Notes は、作成が終わったあとに、画面に出す案内(最初にすること、初期のログインなど)。${変数名} で、入力した値(自動で作ったパスワードを含む)を差し込める。
	Notes string `yaml:"notes"`
}

// Variable は、利用者が入力する値1つの定義。値は、全て、文字列として受け取り、型に合うかを検査する。
type Variable struct {
	// Name は、${名前} で参照するときの名前(英字で始まる、英数字と _)。
	Name string `yaml:"name"`

	// Label は、入力欄の見出し。
	Label string `yaml:"label"`

	// Type は、string、integer、password、choice、boolean のどれか。
	Type string `yaml:"type"`

	// Default は、既定の値(文字列)。無ければ、必須の入力(generate の password を除く)。
	Default *string `yaml:"default"`

	// Pattern は、string の値が、全体で一致すべき正規表現。
	Pattern string `yaml:"pattern"`

	// MinLength と MaxLength は、string と password の値の長さ(文字数)の範囲。
	MinLength *int `yaml:"minLength"`
	MaxLength *int `yaml:"maxLength"`

	// Min と Max は、integer の値の範囲。
	Min *int `yaml:"min"`
	Max *int `yaml:"max"`

	// Generate は、password の値を、入力が無いとき自動で作る。
	Generate bool `yaml:"generate"`

	// Length は、自動で作る password の文字数(既定 24)。
	Length *int `yaml:"length"`

	// Options は、choice の選択肢。
	Options []Option `yaml:"options"`
}

// Option は、choice の選択肢1つ。
type Option struct {
	// Value は、変数に入る値。
	Value string `yaml:"value"`

	// Label は、画面に出す名前。
	Label string `yaml:"label"`
}

// Resource は、作る部品1つ。Kind によって、使う項目が違う(使わない項目は、書けない)。
type Resource struct {
	// Kind は、volume、deploy、configmap、ingress のどれか。
	Kind string `yaml:"kind"`

	// Name は、テンプレートの中で、部品を指す名前(他の部品が参照する)。ingress は、名前を持たない(1つだけ)。
	Name string `yaml:"name"`

	// --- volume ---

	// SizeMB は、volume の大きさ(MB)。整数、または ${変数}。
	SizeMB string `yaml:"sizeMB"`

	// --- deploy ---

	// Image は、起動するイメージ。
	Image string `yaml:"image"`

	// Spec は、small、medium、large。
	Spec string `yaml:"spec"`

	// Replicas は、台数(既定 1)。
	Replicas *int `yaml:"replicas"`

	// Env は、環境変数。
	Env map[string]string `yaml:"env"`

	// FSGroup は、Pod のボリュームの所有グループ(fsGroup)。イメージが非 root(例: Grafana は 472、n8n は 1000)で、ボリュームに書くとき、そのグループの ID を指定する。0 は指定なし。
	FSGroup int `yaml:"fsGroup"`

	// Command と Args は、起動コマンドと引数。
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`

	// Mounts は、volume のマウント。
	Mounts []Mount `yaml:"mounts"`

	// Files は、設定ファイル。deploy だけが持つファイル(path と content)か、configmap のファイル(key、encoding、content)。
	Files []File `yaml:"files"`

	// ConfigMaps は、deploy が、テンプレートの中の configmap のファイルを、マウントする指定。
	ConfigMaps []ConfigMapMount `yaml:"configmaps"`

	// Service は、ポートの公開。無ければ、service を作らない。
	Service *Service `yaml:"service"`

	// --- ingress ---

	// Rules は、path ごとの転送先。
	Rules []Rule `yaml:"rules"`
}

// Mount は、deploy が、テンプレートの中の volume を、マウントする指定。
type Mount struct {
	// Volume は、volume の name。
	Volume string `yaml:"volume"`

	// Path は、コンテナの中のマウント先。
	Path string `yaml:"path"`
}

// File は、ファイル1つ。deploy の files では、path と content(deploy だけが持つ、テキストのファイル)。
// configmap の files では、key、encoding、content(共有の ConfigMap のファイル)。どちらの項目を使うかは、部品の種類で決まる。
type File struct {
	// Path は、コンテナの中の path(deploy の files だけ)。
	Path string `yaml:"path"`

	// Key は、ConfigMap の中のファイル名(configmap の files だけ)。
	Key string `yaml:"key"`

	// Encoding は、content の持ち方。text(既定)か base64(configmap の files だけ。バイナリを、base64 で書く)。
	Encoding string `yaml:"encoding"`

	// Content は、内容。text なら、${変数} を含められる。base64 では、${変数} は使えない。
	Content string `yaml:"content"`

	// From は、テンプレートのフォルダの中の、ファイルの path(例: files/default.conf)。content の代わりに、フォルダのファイルを読み込む。
	// 読み込みのとき(Load)に、Content と Encoding に置き換わり、空になる。text は、content と同じに、${変数} を含められる。
	// configmap の files では、バイナリも読める(自動で判定して、base64 にする)。deploy の files は、テキストだけ。
	From string `yaml:"from"`
}

// ConfigMapMount は、deploy が、テンプレートの中の configmap の 1 ファイルを、コンテナの 1 つの path に置く指定。
type ConfigMapMount struct {
	// ConfigMap は、configmap の name。
	ConfigMap string `yaml:"configmap"`

	// Key は、その configmap の中の、ファイル名(key)。
	Key string `yaml:"key"`

	// Path は、コンテナの中の path。
	Path string `yaml:"path"`

	// Mode は、ファイルの権限(8進数の4桁の文字列。省略は 0644)。
	Mode string `yaml:"mode"`
}

// Service は、deploy のポートの公開。
type Service struct {
	// Ports は、公開するポート。
	Ports []Port `yaml:"ports"`
}

// Port は、公開するポート1つ。
type Port struct {
	// Name は、ポートの名前(小文字の英数字とハイフン、15文字まで)。
	Name string `yaml:"name"`

	// Port は、ポート番号。
	Port int `yaml:"port"`

	// Protocol は、tcp か udp。
	Protocol string `yaml:"protocol"`
}

// Rule は、ingress の、path ごとの転送先。
type Rule struct {
	// Path は、URL の path(「/」で始まる)。
	Path string `yaml:"path"`

	// Deploy は、転送先の deploy の name(その deploy の service に、転送する)。
	Deploy string `yaml:"deploy"`

	// Port は、転送先の service のポート番号(tcp のもの)。
	Port int `yaml:"port"`
}

// Summary は、テンプレートが、何を作るかの要約(画面に出す)。
type Summary struct {
	// Deploys は、deploy の数。
	Deploys int `json:"deploys"`

	// Volumes は、volume の数。
	Volumes int `json:"volumes"`

	// Ingress は、ingress を作るか。
	Ingress bool `json:"ingress"`

	// Files は、deploy に置くファイルの、合計の数。
	Files int `json:"files"`

	// ConfigMaps は、共有の ConfigMap の数。
	ConfigMaps int `json:"configmaps"`
}

// Summarize は、テンプレートが、何を作るかを、数える。
//
// 戻り値:
//   - 要約。
func (template Template) Summarize() Summary {
	// 部品を数える。
	var summary Summary
	for _, resource := range template.Resources {
		switch resource.Kind {
		case ResourceDeploy:
			summary.Deploys++
			summary.Files += len(resource.Files)
		case ResourceVolume:
			summary.Volumes++
		case ResourceIngress:
			summary.Ingress = true
		case ResourceConfigMap:
			summary.ConfigMaps++
		}
	}
	return summary
}

// RequiredVariable は、変数の入力が必須か(既定値が無く、自動生成でもないか)を返す。
//
// 戻り値:
//   - 必須なら true。
func (variable Variable) RequiredVariable() bool {
	// 自動生成の password は、入力が無くても作れる。既定値がある変数も、入力は要らない。
	if variable.Type == VariablePassword && variable.Generate {
		return false
	}
	return variable.Default == nil
}

// referencesIn は、文字列に含まれる ${...} の中身(「title」「service.db.dns」など)を、出てくる順に返す。
//
// 引数:
//   - text: 調べる文字列。
//
// 戻り値:
//   - ${ と } の間の文字列の一覧。閉じていない ${ は、「(閉じていない)」を、その一覧に入れる(検証で、エラーにするため)。
func referencesIn(text string) []string {
	// 先頭から、${ を探して、対応する } までを切り出す。
	var references []string
	for {
		start := strings.Index(text, "${")
		if start < 0 {
			return references
		}
		end := strings.Index(text[start:], "}")
		if end < 0 {
			return append(references, "(閉じていない)")
		}
		references = append(references, text[start+2:start+end])
		text = text[start+end+1:]
	}
}
