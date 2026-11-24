package activity

import (
	"context"
	"fmt"
	"log/slog"

	"go.temporal.io/sdk/temporal"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// IngressStore は、ingress の Activity が使う、データベースの操作。
type IngressStore interface {
	// FindForWorker は、持ち主を問わずに、ingress を1件取得する。
	FindForWorker(requestContext context.Context, ingressID string) (model.Ingress, error)

	// VerifiedDomainHosts は、ingress に付いている、確認済みのカスタムドメイン名を返す。
	VerifiedDomainHosts(requestContext context.Context, ingressID string) ([]string, error)

	// CommitRulesApply は、ルールの反映の結果を確定する(applying の ingress だけ)。確定できたかを返す。
	CommitRulesApply(requestContext context.Context, ingressID string, succeeded bool) (bool, error)

	// DeleteRecord は、削除中の ingress を物理削除する。
	DeleteRecord(requestContext context.Context, ingressID string) error

	// MarkDeleteFailed は、削除中の ingress を failed にする。
	MarkDeleteFailed(requestContext context.Context, ingressID string) error
}

// IngressActivities は、ingress の Workflow が使う Activity をまとめたもの。
type IngressActivities struct {
	ingressStore    IngressStore
	ingressOperator IngressOperator
	appLogger       *slog.Logger
}

// NewIngressActivities は、IngressActivities を作る。
//
// 引数:
//   - ingressStore: データベースの操作。
//   - ingressOperator: k8s の IngressRoute を操作するもの。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した IngressActivities。
func NewIngressActivities(ingressStore IngressStore, ingressOperator IngressOperator, appLogger *slog.Logger) *IngressActivities {
	// 受け取ったものを持たせる。
	return &IngressActivities{ingressStore: ingressStore, ingressOperator: ingressOperator, appLogger: appLogger}
}

// rulesToDefinition は、ingress と、反映するルール(JSON)と、確認済みのカスタムドメインから、IngressRoute に反映する内容を組み立てる。
//
// 引数:
//   - ingress: 対象の ingress(プロジェクト、ID、ホスト名を使う)。
//   - rulesJSON: 反映するルール(IngressRule の JSON の配列)。
//
// 戻り値:
//   - IngressRoute に反映する内容。ルールが読めないときは、リトライしないエラー。
func rulesToDefinition(ingress model.Ingress, rulesJSON []byte, extraHosts []string) (IngressRouteDefinition, error) {
	// ルールを読み込む。壊れていたら、何度やっても同じなので、リトライしない。
	rules, parseError := model.ParseIngressRules(rulesJSON)
	if parseError != nil {
		return IngressRouteDefinition{}, temporal.NewNonRetryableApplicationError("ルールが壊れています", "InvalidIngressRules", parseError)
	}
	// 各ルールを、IngressRoute のルールにする。
	routeRules := make([]IngressRouteRule, 0, len(rules))
	for _, rule := range rules {
		routeRules = append(routeRules, IngressRouteRule{Path: rule.Path, ServiceName: rule.ServiceID, Port: int32(rule.Port)})
	}
	return IngressRouteDefinition{Namespace: ingress.ProjectID, Name: ingress.ID, Host: ingress.Host, ExtraHosts: extraHosts, Rules: routeRules, AccessStats: ingress.AccessStats}, nil
}

// ApplyIngressRules は、pending のルールで、IngressRoute を更新する。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 更新する ingress の ID。
//
// 戻り値:
//   - ingress を読めなかったとき、適用するルールが無い・壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (ingressActivities *IngressActivities) ApplyIngressRules(requestContext context.Context, input workflow.ApplyIngressRulesActivityInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	ingressActivities.appLogger.Info("pending のルールで IngressRoute を更新する", "ingress_id", input.IngressID, "activity", workflow.ApplyIngressRulesActivityName)
	// DB から ingress を読む。
	ingress, findError := ingressActivities.ingressStore.FindForWorker(requestContext, input.IngressID)
	if findError != nil {
		return findError
	}
	// 適用するルールが無いのは、何度やっても同じなので、リトライしない。
	if len(ingress.PendingRules) == 0 {
		return temporal.NewNonRetryableApplicationError("適用するルールがありません", "NoPendingRules", nil)
	}
	// pending のルールから、IngressRoute の内容を組み立てて、k8s を操作するものに任せる。
	extraHosts, hostsError := ingressActivities.ingressStore.VerifiedDomainHosts(requestContext, ingress.ID)
	if hostsError != nil {
		return hostsError
	}
	definition, definitionError := rulesToDefinition(ingress, ingress.PendingRules, extraHosts)
	if definitionError != nil {
		return definitionError
	}
	return markNonRetryable(ingressActivities.ingressOperator.ApplyIngressRoute(requestContext, definition))
}

// ReapplyIngressRoute は、現在(反映済み)のルールで、IngressRoute を作り直す(アクセスの集計の切り替えなどに使う)。
//
// ルールの状態(pending、status)は変えない。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 対象の ingress の ID。
//
// 戻り値:
//   - ingress を読めなかったとき、ルールが壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (ingressActivities *IngressActivities) ReapplyIngressRoute(requestContext context.Context, input workflow.ReapplyIngressRouteInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	ingressActivities.appLogger.Info("現在のルールで IngressRoute を作り直す", "ingress_id", input.IngressID, "activity", workflow.ReapplyIngressRouteActivityName)
	// DB から ingress を読み、正式なルールと、集計の設定で、内容を組み立てる。
	ingress, findError := ingressActivities.ingressStore.FindForWorker(requestContext, input.IngressID)
	if findError != nil {
		return findError
	}
	extraHosts, hostsError := ingressActivities.ingressStore.VerifiedDomainHosts(requestContext, ingress.ID)
	if hostsError != nil {
		return hostsError
	}
	definition, definitionError := rulesToDefinition(ingress, ingress.Rules, extraHosts)
	if definitionError != nil {
		return definitionError
	}
	return markNonRetryable(ingressActivities.ingressOperator.ApplyIngressRoute(requestContext, definition))
}

// RollbackIngressRules は、反映に失敗したとき、IngressRoute を、現在の(動いていた)ルールに戻す。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 元に戻す ingress の ID。
//
// 戻り値:
//   - ingress を読めなかったとき、ルールが壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (ingressActivities *IngressActivities) RollbackIngressRules(requestContext context.Context, input workflow.ApplyIngressRulesActivityInput) error {
	// 運用で気づけるよう、警告で残す。
	ingressActivities.appLogger.Warn("反映に失敗したので、IngressRoute を元のルールに戻す", "ingress_id", input.IngressID, "activity", workflow.RollbackIngressRulesActivityName)
	// DB から ingress を読み、正式なルールで、IngressRoute の内容を組み立てる。
	ingress, findError := ingressActivities.ingressStore.FindForWorker(requestContext, input.IngressID)
	if findError != nil {
		return findError
	}
	extraHosts, hostsError := ingressActivities.ingressStore.VerifiedDomainHosts(requestContext, ingress.ID)
	if hostsError != nil {
		return hostsError
	}
	definition, definitionError := rulesToDefinition(ingress, ingress.Rules, extraHosts)
	if definitionError != nil {
		return definitionError
	}
	return markNonRetryable(ingressActivities.ingressOperator.ApplyIngressRoute(requestContext, definition))
}

// CommitIngressRulesResult は、ルールの反映の結果を DB に確定する。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 結果を確定する ingress の ID と、成功したか。
//
// 戻り値:
//   - 確定できなかったときのデータベースのエラー。
func (ingressActivities *IngressActivities) CommitIngressRulesResult(requestContext context.Context, input workflow.CommitIngressRulesResultInput) error {
	// 結果をログに残す(失敗は、運用で気づけるよう警告にする)。
	if input.Succeeded {
		ingressActivities.appLogger.Info("ingress のルールの反映に成功した", "ingress_id", input.IngressID, "activity", workflow.CommitIngressRulesResultActivityName)
	} else {
		ingressActivities.appLogger.Warn("ingress のルールの反映に失敗した", "ingress_id", input.IngressID, "activity", workflow.CommitIngressRulesResultActivityName)
	}
	// 確定を、保存先に任せる(確定済みなら何もしない)。
	_, commitError := ingressActivities.ingressStore.CommitRulesApply(requestContext, input.IngressID, input.Succeeded)
	return commitError
}

// DeleteIngressObject は、ingress に対応する IngressRoute を削除する(無くてもエラーにしない)。
//
// ingress が、すでに DB に無いときは、削除するものが無いので、何もしない。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除する ingress の ID。
//
// 戻り値:
//   - ingress を読めなかったとき、k8s への依頼に失敗したときのエラー。
func (ingressActivities *IngressActivities) DeleteIngressObject(requestContext context.Context, input workflow.DeleteIngressObjectInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	ingressActivities.appLogger.Info("IngressRoute を削除する", "ingress_id", input.IngressID, "activity", workflow.DeleteIngressObjectActivityName)
	// DB から ingress を読む(namespace の名前を知るため)。
	ingress, findError := ingressActivities.ingressStore.FindForWorker(requestContext, input.IngressID)
	if findError != nil {
		return fmt.Errorf("ingress を読めませんでした: %w", findError)
	}
	// k8s を操作するものに任せる。
	return markNonRetryable(ingressActivities.ingressOperator.DeleteIngressRoute(requestContext, ingress.ProjectID, ingress.ID))
}

// DeleteIngressRecord は、ingress のレコードを物理削除する。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除する ingress の ID。
//
// 戻り値:
//   - データベースのエラー。
func (ingressActivities *IngressActivities) DeleteIngressRecord(requestContext context.Context, input workflow.DeleteIngressRecordInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	ingressActivities.appLogger.Info("ingress のレコードを削除する", "ingress_id", input.IngressID, "activity", workflow.DeleteIngressRecordActivityName)
	// 保存先に任せる(削除中のものだけが消える)。
	return ingressActivities.ingressStore.DeleteRecord(requestContext, input.IngressID)
}

// MarkIngressDeleteFailed は、削除に失敗した ingress を failed にする。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 対象の ingress の ID。
//
// 戻り値:
//   - データベースのエラー。
func (ingressActivities *IngressActivities) MarkIngressDeleteFailed(requestContext context.Context, input workflow.MarkIngressDeleteFailedInput) error {
	// 運用で気づけるよう、警告で残す。
	ingressActivities.appLogger.Warn("ingress の削除に失敗した", "ingress_id", input.IngressID, "activity", workflow.MarkIngressDeleteFailedActivityName)
	// 保存先に任せる(削除中のものだけが failed になる)。
	return ingressActivities.ingressStore.MarkDeleteFailed(requestContext, input.IngressID)
}
