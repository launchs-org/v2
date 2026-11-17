// Package events は、プロジェクトの namespace で起きた k8s の Event を観測し、DB に保存する(docs/project-events.md)。
package events

import (
	"regexp"
	"time"

	"launchs/shared/model"

	corev1 "k8s.io/api/core/v1"
)

// projectNamespacePattern は、プロジェクトの namespace の名前(「project-」+ UUID)。これ以外の namespace の Event は、保存しない。
var projectNamespacePattern = regexp.MustCompile(`^project-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// deployNamePattern は、deploy の ID(「deploy-」+ UUID)を、k8s の名前の先頭から取り出す式。
// Deployment の名前は deploy の ID そのもの、ReplicaSet は「<deploy の ID>-<ハッシュ>」、Pod は「<deploy の ID>-<ハッシュ>-<乱数>」。
var deployNamePattern = regexp.MustCompile(`^(deploy-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:-|$)`)

// IsProjectNamespace は、namespace が、プロジェクトのものかを返す。
//
// 引数:
//   - namespace: namespace の名前。
//
// 戻り値:
//   - 「project-」+ UUID の形なら true。
func IsProjectNamespace(namespace string) bool {
	// 形が合うかだけを調べる。
	return projectNamespacePattern.MatchString(namespace)
}

// deployIDFromObjectName は、対象(Deployment、ReplicaSet、Pod)の名前から、属する deploy の ID を取り出す。
//
// 例: 「deploy-aaaaaaaa-…-aaaaaaaaaaaa-5bdf7f4589-bnhdv」(Pod)→「deploy-aaaaaaaa-…-aaaaaaaaaaaa」。
//
// 引数:
//   - name: 対象の名前。
//
// 戻り値:
//   - deploy の ID。deploy の名前の形でなければ空(PVC など)。
func deployIDFromObjectName(name string) string {
	// 先頭が deploy の ID の形なら、それを返す。
	matches := deployNamePattern.FindStringSubmatch(name)
	if matches == nil {
		return ""
	}
	return matches[1]
}

// firstNonZero は、最初のゼロでない時刻を返す。全てゼロなら、ゼロの時刻を返す。
//
// 引数:
//   - times: 優先順に並べた時刻。
//
// 戻り値:
//   - 最初のゼロでない時刻。
func firstNonZero(times ...time.Time) time.Time {
	// 優先順に見て、ゼロでないものを返す。
	for _, candidate := range times {
		if !candidate.IsZero() {
			return candidate
		}
	}
	return time.Time{}
}

// ToProjectEvent は、k8s の Event を、保存する形に変換する。
//
// 引数:
//   - event: k8s の Event(core/v1)。
//
// 戻り値:
//   - 保存する Event。
//   - 保存の対象でない(プロジェクトの namespace でない、UID が無い、時刻が全く分からない)ときは false。
func ToProjectEvent(event *corev1.Event) (model.ProjectEvent, bool) {
	// プロジェクトの namespace でない Event と、UID の無い Event は、保存しない。
	if event == nil || !IsProjectNamespace(event.Namespace) || event.UID == "" {
		return model.ProjectEvent{}, false
	}
	// 最初と最後の時刻を決める。k8s のバージョンや発行元によって、入る項目が違うので、順に探す。
	created := event.CreationTimestamp.Time
	first := firstNonZero(event.FirstTimestamp.Time, event.EventTime.Time, created, event.LastTimestamp.Time)
	last := firstNonZero(event.LastTimestamp.Time, event.EventTime.Time, first)
	// 時刻が全く分からないものは、並べられないので、保存しない。
	if first.IsZero() || last.IsZero() {
		return model.ProjectEvent{}, false
	}
	// 最後が最初より前になる、ずれた値は、最後を最初に合わせる。
	if last.Before(first) {
		last = first
	}
	// 回数が 0 のもの(回数をまとめない発行元)は、1 回として扱う。
	count := int(event.Count)
	if count < 1 {
		count = 1
	}
	return model.ProjectEvent{
		ProjectID:   event.Namespace,
		EventUID:    string(event.UID),
		Type:        event.Type,
		Reason:      event.Reason,
		Message:     event.Message,
		ObjectKind:  event.InvolvedObject.Kind,
		ObjectName:  event.InvolvedObject.Name,
		DeployID:    deployIDFromObjectName(event.InvolvedObject.Name),
		Count:       count,
		FirstSeenAt: first,
		LastSeenAt:  last,
	}, true
}
