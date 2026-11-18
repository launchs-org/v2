// Package projecttask は、プロジェクトのタスク(Temporal の Workflow 1 回につき 1 行の記録。docs/tasks.md)の、種類の対応と、失敗の理由の整え方を持つ。
package projecttask

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.temporal.io/sdk/temporal"

	"launchs/shared/workflow"
)

// MaxErrorRunes は、保存する失敗の理由の、最大の文字数。
const MaxErrorRunes = 500

// kinds は、Workflow の型の名前 → タスクの種類の対応。ここに無い Workflow は、記録しない。
var kinds = map[string]string{
	workflow.CreateDeployWorkflowName:               "deploy_create",
	workflow.DeleteDeployWorkflowName:               "deploy_delete",
	workflow.ApplyDeployConfigWorkflowName:          "deploy_config",
	workflow.ApplyDeployFilesWorkflowName:           "deploy_files",
	workflow.ApplyDeployMountsWorkflowName:          "deploy_mounts",
	workflow.ApplyDeployConfigMapMountsWorkflowName: "deploy_configmap_mounts",
	workflow.ScaleDeployWorkflowName:                "deploy_scale",
	workflow.StopCrashLoopWorkflowName:              "deploy_crash_stop",
	workflow.CreateServiceWorkflowName:              "service_create",
	workflow.DeleteServiceWorkflowName:              "service_delete",
	workflow.ApplyServicePortsWorkflowName:          "service_ports",
	workflow.ApplyVolumeSizeWorkflowName:            "volume_resize",
	workflow.DeleteVolumeWorkflowName:               "volume_delete",
	workflow.ApplyIngressRulesWorkflowName:          "ingress_apply",
	workflow.ReapplyIngressRouteWorkflowName:        "ingress_reapply",
	workflow.DeleteIngressWorkflowName:              "ingress_delete",
	workflow.ApplyTemplateWorkflowName:              "template_apply",
	workflow.DeleteProjectWorkflowName:              "project_delete",
}

// KindProjectCreate は、プロジェクトの作成の種類。Workflow ではなく、作成のときに、backend が、成功として記録する。
const KindProjectCreate = "project_create"

// KindOf は、Workflow の型の名前から、タスクの種類を返す。
//
// 引数:
//   - workflowType: Workflow の型の名前。
//
// 戻り値:
//   - 種類と、記録の対象か(対応が無い Workflow は、false)。
func KindOf(workflowType string) (string, bool) {
	// 表から引く。
	kind, found := kinds[workflowType]
	return kind, found
}

// Kinds は、全ての種類の一覧を返す(絞り込みの検査に使う)。
//
// 戻り値:
//   - 種類の集合。
func Kinds() map[string]bool {
	// 表の値を集める。
	result := make(map[string]bool, len(kinds)+1)
	result[KindProjectCreate] = true
	for _, kind := range kinds {
		result[kind] = true
	}
	return result
}

// 秘密を含みうる文字列の形(順に、伏せる)。
var (
	// URL に埋め込まれた、ユーザーとパスワード(postgres://user:pass@host など)。
	credentialsInURL = regexp.MustCompile(`(://)[^/\s:@]+:[^/\s@]+@`)

	// password=…、token: …、"token": "…" のような、名前と値の組。
	secretPair = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key)\b("?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,}]+)`)

	// Bearer トークン。
	bearerToken = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)

	// 連続する空白(改行を含む)。
	whitespaceRun = regexp.MustCompile(`\s+`)
)

// Sanitize は、失敗の理由を、保存して画面に出せる形に整える(秘密を含みうる部分を伏せ、1 行にして、長さを切る)。
//
// 引数:
//   - message: 失敗の理由。
//
// 戻り値:
//   - 整えた文字列(MaxErrorRunes 文字まで)。
func Sanitize(message string) string {
	// URL の認証情報、名前つきの秘密、Bearer トークンを伏せる。
	cleaned := credentialsInURL.ReplaceAllString(message, "${1}***@")
	cleaned = secretPair.ReplaceAllString(cleaned, "${1}${2}***")
	cleaned = bearerToken.ReplaceAllString(cleaned, "Bearer ***")
	// 1 行にまとめる。
	cleaned = strings.TrimSpace(whitespaceRun.ReplaceAllString(cleaned, " "))
	// 長ければ、切る(文字の途中で切らない)。
	if utf8.RuneCountInString(cleaned) > MaxErrorRunes {
		runes := []rune(cleaned)
		cleaned = string(runes[:MaxErrorRunes]) + "…"
	}
	return cleaned
}

// ErrorMessage は、Workflow が返したエラーから、画面に出す失敗の理由を取り出す。
// Temporal のエラーは、何重にも包まれているので、原因(一番内側のアプリケーションのエラー)のメッセージを使う。
//
// 引数:
//   - workflowError: Workflow が返したエラー。
//
// 戻り値:
//   - 整えた失敗の理由。
func ErrorMessage(workflowError error) string {
	// 取り消しと時間切れは、決まった文にする。
	if temporal.IsCanceledError(workflowError) {
		return "取り消されました"
	}
	if temporal.IsTimeoutError(workflowError) {
		// 時間切れになった処理(Activity)の名前が分かれば、添える。
		var activityError *temporal.ActivityError
		if errors.As(workflowError, &activityError) && activityError.ActivityType() != nil && activityError.ActivityType().Name != "" {
			return "処理「" + activityError.ActivityType().Name + "」が、時間切れになりました"
		}
		return "時間切れになりました"
	}
	// 一番内側のアプリケーションのエラーを探す。
	var innermost *temporal.ApplicationError
	for current := workflowError; current != nil; current = errors.Unwrap(current) {
		var applicationError *temporal.ApplicationError
		if errors.As(current, &applicationError) {
			innermost = applicationError
		}
	}
	if innermost != nil && innermost.Message() != "" {
		return Sanitize(innermost.Message())
	}
	return Sanitize(workflowError.Error())
}
