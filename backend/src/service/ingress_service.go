package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
	"launchs/shared/repository"
)

// maxHostAttempts は、ホスト名が他と重なったとき、作り直して保存をやり直す回数の上限。
const maxHostAttempts = 10

// IngressStore は、ingress のユースケースが使う、データベースの操作。
type IngressStore interface {
	// Create は、ingress を保存する。ホスト名が重なったときは repository.ErrIngressHostTaken。
	Create(requestContext context.Context, ingress *model.Ingress) error

	// ListByProject は、プロジェクトの ingress を、新しい順に取得する。
	ListByProject(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.Ingress, *pagination.Position, error)

	// FindByID は、ingress を1件取得する。
	FindByID(requestContext context.Context, userID string, projectID string, ingressID string) (model.Ingress, error)

	// MarkDeleting は、削除を受け付ける(状態を deleting にする)。変える前の状態を返す。
	MarkDeleting(requestContext context.Context, userID string, projectID string, ingressID string) (string, error)

	// RevertDeleting は、削除中にした ingress を、元の状態に戻す。
	RevertDeleting(requestContext context.Context, ingressID string, previousStatus string) error

	// SavePendingRules は、ルールを、まだ適用しない値として保存する。
	SavePendingRules(requestContext context.Context, userID string, projectID string, ingressID string, rules []model.IngressRule) error

	// DiscardPendingRules は、pending のルールを破棄する。
	DiscardPendingRules(requestContext context.Context, userID string, projectID string, ingressID string) error

	// BeginRulesApply は、ルールの反映を始める(状態を applying にする)。変える前の状態を返す。
	BeginRulesApply(requestContext context.Context, userID string, projectID string, ingressID string) (string, error)

	// SetAccessStats は、アクセスの集計の有効・無効を保存し、保存後の ingress を返す。
	SetAccessStats(requestContext context.Context, userID string, projectID string, ingressID string, enabled bool) (model.Ingress, error)

	// RevertApply は、反映の開始に失敗したとき、状態を元に戻す。
	RevertApply(requestContext context.Context, ingressID string, previousStatus string) error
}

// IngressWorkflowStarter は、ingress のルールの反映と削除の Workflow を開始するもの。
type IngressWorkflowStarter interface {
	// StartRulesApply は、ルールの反映の Workflow を開始する。
	StartRulesApply(requestContext context.Context, ingressID string) error

	// StartRouteReapply は、現在のルールのまま、IngressRoute を作り直す Workflow を開始する。
	StartRouteReapply(requestContext context.Context, ingressID string) error

	// StartIngressDeletion は、ingress の削除の Workflow を開始する。
	StartIngressDeletion(requestContext context.Context, ingressID string) error
}

// IngressHostGenerator は、ingress に払い出すホスト名を作るもの。
type IngressHostGenerator interface {
	// Generate は、ホスト名を1つ作る。
	Generate() (string, error)
}

// IngressService は、ingress に関するユースケース。
type IngressService struct {
	ingressStore           IngressStore
	ingressWorkflowStarter IngressWorkflowStarter
	hostGenerator          IngressHostGenerator
}

// NewIngressService は、IngressService を作る。
//
// 引数:
//   - ingressStore: データベースの操作。
//   - ingressWorkflowStarter: Workflow を開始するもの。
//   - hostGenerator: ホスト名を作るもの。
//
// 戻り値:
//   - 作成した IngressService。
func NewIngressService(ingressStore IngressStore, ingressWorkflowStarter IngressWorkflowStarter, hostGenerator IngressHostGenerator) *IngressService {
	// 依存するものを持たせて返す。
	return &IngressService{ingressStore: ingressStore, ingressWorkflowStarter: ingressWorkflowStarter, hostGenerator: hostGenerator}
}

// Create は、プロジェクトに ingress を作る(ランダムなホスト名を払い出す)。
//
// ホスト名が他の ingress と重なったときは、作り直して、保存をやり直す(上限あり)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 属するプロジェクトの ID。
//
// 戻り値:
//   - 作成した ingress。
//   - プロジェクトが無ければ not_found、active でなければ conflict、それ以外は internal_error。
func (ingressService *IngressService) Create(requestContext context.Context, userID string, projectID string) (model.Ingress, error) {
	// ホスト名が重なったときのために、何度か試す。
	for attempt := 0; attempt < maxHostAttempts; attempt++ {
		// ホスト名を払い出す。
		host, hostError := ingressService.hostGenerator.Generate()
		if hostError != nil {
			return model.Ingress{}, apperror.Wrap(apperror.KindInternal, "ホスト名を払い出せませんでした", hostError)
		}
		// ID を作って、保存する。
		ingress := model.Ingress{ID: model.IngressIDPrefix + uuid.NewString(), ProjectID: projectID, UserID: userID, Host: host}
		createError := ingressService.ingressStore.Create(requestContext, &ingress)
		// 重なったときだけ、作り直す。
		if errors.Is(createError, repository.ErrIngressHostTaken) {
			continue
		}
		if createError != nil {
			return model.Ingress{}, createError
		}
		return ingress, nil
	}
	return model.Ingress{}, apperror.New(apperror.KindInternal, "重ならないホスト名を払い出せませんでした")
}

// List は、プロジェクトの ingress を、新しい順に返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - limit: 1ページの件数。
//   - after: 前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - ingress の一覧と、次のページの位置。
//   - プロジェクトが無ければ not_found。
func (ingressService *IngressService) List(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.Ingress, *pagination.Position, error) {
	// 保存先に任せる。
	return ingressService.ingressStore.ListByProject(requestContext, userID, projectID, limit, after)
}

// Get は、ingress を1件返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - ingress。無ければ not_found。
func (ingressService *IngressService) Get(requestContext context.Context, userID string, projectID string, ingressID string) (model.Ingress, error) {
	// 保存先に任せる(他のユーザーのものは、見つからない扱いになる)。
	return ingressService.ingressStore.FindByID(requestContext, userID, projectID, ingressID)
}

// UpdateRules は、ルールを、まだ適用しない値として保存する(一括置き換え)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - rules: 新しいルール(形は、確かめてから保存する)。
//
// 戻り値:
//   - ルールの形・流す先が不正なら invalid_request。ingress が無ければ not_found。反映中・削除中なら conflict。
func (ingressService *IngressService) UpdateRules(requestContext context.Context, userID string, projectID string, ingressID string, rules []model.IngressRule) error {
	// ルールの形(path の書式、重複、個数など)を確かめる。
	if validateError := model.ValidateIngressRules(rules); validateError != nil {
		return validateError
	}
	// 流す先の確認を含めて、保存先に任せる。
	return ingressService.ingressStore.SavePendingRules(requestContext, userID, projectID, ingressID, rules)
}

// DiscardRules は、pending のルールを破棄する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - ingress が無ければ not_found。反映中・削除中、または pending が無ければ conflict。
func (ingressService *IngressService) DiscardRules(requestContext context.Context, userID string, projectID string, ingressID string) error {
	// 保存先に任せる。
	return ingressService.ingressStore.DiscardPendingRules(requestContext, userID, projectID, ingressID)
}

// UpdateAccessStats は、アクセスの集計の有効・無効を切り替え、IngressRoute に反映する。
//
// 流れ: 設定を保存する → ルールがあれば、IngressRoute を作り直す Workflow を開始する(同じ値でも開始する。冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - enabled: 有効にするなら true。
//
// 戻り値:
//   - 保存後の ingress。無ければ not_found、反映中・削除中なら conflict、Workflow を開始できなければ internal_error。
func (ingressService *IngressService) UpdateAccessStats(requestContext context.Context, userID string, projectID string, ingressID string, enabled bool) (model.Ingress, error) {
	// 設定を保存する。
	ingress, saveError := ingressService.ingressStore.SetAccessStats(requestContext, userID, projectID, ingressID, enabled)
	if saveError != nil {
		return model.Ingress{}, saveError
	}
	// 反映済みのルールが無ければ、IngressRoute が無いので、作り直すものが無い。
	rules, parseError := model.ParseIngressRules(ingress.Rules)
	if parseError != nil || len(rules) == 0 {
		return ingress, nil
	}
	// IngressRoute を作り直す。
	if startError := ingressService.ingressWorkflowStarter.StartRouteReapply(requestContext, ingressID); startError != nil {
		return model.Ingress{}, apperror.Wrap(apperror.KindInternal, "IngressRoute の作り直しを開始できませんでした", startError)
	}
	return ingress, nil
}

// ApplyRules は、pending のルールの反映を始める。
//
// 流れ: 状態を applying にする → Workflow を開始する。Workflow を開始できなかったときは、状態を元に戻す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: 反映する ingress の ID。
//
// 戻り値:
//   - ingress が無ければ not_found。反映中・削除中、pending が無い、流す先が不正なら conflict。
//   - Workflow を開始できなかったときは internal_error。
func (ingressService *IngressService) ApplyRules(requestContext context.Context, userID string, projectID string, ingressID string) error {
	// 1. 状態を applying にする。
	previousStatus, beginError := ingressService.ingressStore.BeginRulesApply(requestContext, userID, projectID, ingressID)
	if beginError != nil {
		return beginError
	}
	// 2. Workflow を開始する。
	startError := ingressService.ingressWorkflowStarter.StartRulesApply(requestContext, ingressID)
	if startError == nil {
		return nil
	}
	// 3. 開始できなかったので、状態を元に戻す(リクエストが中断されていても戻せるよう、context を切り離す)。
	if revertError := ingressService.ingressStore.RevertApply(context.WithoutCancel(requestContext), ingressID, previousStatus); revertError != nil {
		return apperror.Wrap(apperror.KindInternal, "ルールの反映を開始できませんでした", joinErrors(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "ルールの反映を開始できませんでした", startError)
}

// Delete は、ingress の削除を受け付け、実体の削除を開始する。
//
// 流れ: 状態を deleting にする → Workflow を開始する。Workflow を開始できなかったときは、状態を元に戻す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: 削除する ingress の ID。
//
// 戻り値:
//   - ingress が無ければ not_found。反映中・削除中なら conflict。
//   - Workflow を開始できなかったときは internal_error。
func (ingressService *IngressService) Delete(requestContext context.Context, userID string, projectID string, ingressID string) error {
	// 1. 状態を deleting にする。
	previousStatus, markError := ingressService.ingressStore.MarkDeleting(requestContext, userID, projectID, ingressID)
	if markError != nil {
		return markError
	}
	// 2. Workflow を開始する。
	startError := ingressService.ingressWorkflowStarter.StartIngressDeletion(requestContext, ingressID)
	if startError == nil {
		return nil
	}
	// 3. 開始できなかったので、状態を元に戻す。
	if revertError := ingressService.ingressStore.RevertDeleting(context.WithoutCancel(requestContext), ingressID, previousStatus); revertError != nil {
		return apperror.Wrap(apperror.KindInternal, "ingress の削除を開始できませんでした", joinErrors(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "ingress の削除を開始できませんでした", startError)
}
