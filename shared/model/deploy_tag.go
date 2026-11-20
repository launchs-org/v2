package model

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"launchs/shared/apperror"
)

const (
	// MaxDeployTags は、1つの deploy に付けられるタグの最大の数。
	MaxDeployTags = 10

	// MaxDeployTagLength は、タグ1つの最大の長さ(文字数)。
	MaxDeployTagLength = 40
)

// deployTagPattern は、タグに使える文字(小文字の英数字で始まり、英数字と . _ : - が続く)。
var deployTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]*$`)

// NormalizeDeployTags は、タグの一覧を検査して、重複を除き、昇順に並べて返す。
//
// 前後の空白は取り除く。空の配列(全て外す)は許す。
//
// 引数:
//   - tags: 利用者が指定したタグ。
//
// 戻り値:
//   - 整えたタグ(nil にはしない)。
//   - 数が多すぎる、長すぎる、使えない文字がある、空のタグがあるときは、invalid_request。
func NormalizeDeployTags(tags []string) ([]string, error) {
	// 重複を除くための集合を作る。
	seen := map[string]bool{}
	// 整えた結果を入れる(空でも、null ではなく [] になるよう、空の配列で作る)。
	normalized := []string{}
	// 1つずつ検査する。
	for _, rawTag := range tags {
		// 前後の空白を取り除く。
		tag := strings.TrimSpace(rawTag)
		// 長さと文字を検査する。
		if tag == "" || len(tag) > MaxDeployTagLength || !deployTagPattern.MatchString(tag) {
			return nil, apperror.New(apperror.KindInvalidRequest, "タグは、小文字の英数字で始まり、英数字と . _ : - だけを使った、40 文字までの文字列にしてください")
		}
		// 重複は、1つにまとめる。
		if !seen[tag] {
			seen[tag] = true
			normalized = append(normalized, tag)
		}
	}
	// 数の上限を確かめる。
	if len(normalized) > MaxDeployTags {
		return nil, apperror.New(apperror.KindInvalidRequest, "タグは、1つの deploy に 10 個までです")
	}
	// 昇順に並べる(同じ組が、同じ並びで保存されるように)。
	sort.Strings(normalized)
	return normalized, nil
}

// decodeTagList は、タグの JSON の列から、タグの配列を読む。空や壊れた値は、空の配列(nil にはしない)。
//
// 引数:
//   - column: タグの JSON の列。
//
// 戻り値:
//   - タグの配列。
func decodeTagList(column []byte) []string {
	// 空の配列から始める(null を返さない)。
	tags := []string{}
	// 列が空なら、そのまま返す。
	if len(column) == 0 {
		return tags
	}
	// JSON の配列を読む。読めなければ、空を返す。
	if decodeError := json.Unmarshal(column, &tags); decodeError != nil {
		return []string{}
	}
	return tags
}

// TagList は、deploy のタグの一覧を返す。
//
// 戻り値:
//   - タグの配列。無い、または読めないときは、空の配列。
func (deploy Deploy) TagList() []string {
	// 列から読む。
	return decodeTagList(deploy.Tags)
}

// TagList は、volume のタグの一覧を返す。
func (volume Volume) TagList() []string {
	// 列から読む。
	return decodeTagList(volume.Tags)
}

// TagList は、service のタグの一覧を返す。
func (service Service) TagList() []string {
	// 列から読む。
	return decodeTagList(service.Tags)
}

// TagList は、ingress のタグの一覧を返す。
func (ingress Ingress) TagList() []string {
	// 列から読む。
	return decodeTagList(ingress.Tags)
}

// TemplateDeployTags は、テンプレートから作る deploy に自動で付けるタグを返す。
//
// 「template:<テンプレートの id>」(同じテンプレートの全て)と、「apply:<適用の識別>」(この 1 回の適用の分だけ)の 2 つ。
// 後者があるので、同じテンプレートを 2 回適用しても、片方だけを一斉に削除できる。
//
// 引数:
//   - templateID: テンプレートの id。
//   - applyID: 適用の ID(末尾の 8 文字を識別に使う)。
//
// 戻り値:
//   - 昇順のタグ 2 つ。
func TemplateDeployTags(templateID string, applyID string) []string {
	// 適用の ID の末尾 8 文字を、識別にする(短ければ、全部)。
	suffix := applyID
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}
	// 小文字にして、使えない文字を - に置き換えて、タグにする。
	tag := strings.ToLower(suffix)
	tag = regexp.MustCompile(`[^a-z0-9._:-]`).ReplaceAllString(tag, "-")
	return []string{"apply:" + tag, "template:" + templateID}
}

// DeployMetricSample は、deploy_metric_samples テーブルの1行(ある時刻の、deploy の観測値)。
type DeployMetricSample struct {
	// DeployID は、観測した deploy の ID。
	DeployID string `gorm:"column:deploy_id;primaryKey"`

	// ProjectID は、deploy が属するプロジェクトの ID。
	ProjectID string `gorm:"column:project_id"`

	// SampledAt は、観測した時刻。
	SampledAt time.Time `gorm:"column:sampled_at;primaryKey"`

	// CPUMillicores は、CPU の使用量の合計(ミリコア)。測れなかったときは nil。
	CPUMillicores *int64 `gorm:"column:cpu_millicores"`

	// MemoryBytes は、メモリの使用量の合計(バイト)。測れなかったときは nil。
	MemoryBytes *int64 `gorm:"column:memory_bytes"`

	// Pods は、Pod の数。
	Pods int `gorm:"column:pods"`

	// ReadyPods は、Ready の Pod の数。
	ReadyPods int `gorm:"column:ready_pods"`

	// Restarts は、コンテナの再起動の回数の合計。
	Restarts int `gorm:"column:restarts"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("deploy_metric_samples")。
func (DeployMetricSample) TableName() string {
	return "deploy_metric_samples"
}
