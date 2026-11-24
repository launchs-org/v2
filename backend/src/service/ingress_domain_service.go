package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/repository"
)

// IngressDomainStore は、カスタムドメインの保存先。IngressDomainRepository が実装する。
type IngressDomainStore interface {
	// Create は、ingress にドメインを(確認待ちで)付ける。
	Create(requestContext context.Context, userID string, projectID string, domain *model.IngressDomain) error

	// List は、ingress に付いているドメインを、追加した順に返す。
	List(requestContext context.Context, userID string, projectID string, ingressID string) ([]model.IngressDomain, error)

	// Find は、ドメインを 1 件返す。
	Find(requestContext context.Context, userID string, projectID string, ingressID string, domainID string) (model.IngressDomain, error)

	// MarkVerified は、ドメインを確認済みにする。
	MarkVerified(requestContext context.Context, domainID string) error

	// Delete は、ドメインを外し、外したドメインを返す。
	Delete(requestContext context.Context, userID string, projectID string, ingressID string, domainID string) (model.IngressDomain, error)
}

// IngressDomainReader は、カスタムドメインの操作のために、ingress を読むもの。IngressRepository が実装する。
type IngressDomainReader interface {
	// FindByID は、持ち主の ingress を 1 件返す。
	FindByID(requestContext context.Context, userID string, projectID string, ingressID string) (model.Ingress, error)
}

// IngressDomainVerifier は、DNS を見て、ドメインの持ち主であることを確かめるもの。
type IngressDomainVerifier interface {
	// Verify は、TXT(_launchs-challenge.<ドメイン> に token がある)か、CNAME(ドメインが cnameTarget を指す)のどちらかが満たされているかを返す。
	// 満たされないときは、false と、利用者に見せる理由を返す。
	Verify(requestContext context.Context, hostname string, token string, cnameTarget string) (bool, string)
}

// IngressDomainService は、ingress のカスタムドメイン(追加、確認、削除)の処理をまとめる。
type IngressDomainService struct {
	// ingressReader は、ingress を読む。
	ingressReader IngressDomainReader

	// domainStore は、カスタムドメインの保存先。
	domainStore IngressDomainStore

	// verifier は、DNS で所有を確かめる。
	verifier IngressDomainVerifier

	// workflowStarter は、確認済みのドメインが変わったときに、IngressRoute を作り直す Workflow を開始する。
	workflowStarter IngressWorkflowStarter

	// baseDomain は、払い出すホスト名のベースドメイン(これと、その下は、カスタムドメインにできない)。
	baseDomain string
}

// NewIngressDomainService は、IngressDomainService を作る。
//
// 引数:
//   - ingressReader: ingress を読むもの。
//   - domainStore: カスタムドメインの保存先。
//   - verifier: DNS で所有を確かめるもの。
//   - workflowStarter: IngressRoute を作り直す Workflow を開始するもの。
//   - baseDomain: 払い出すホスト名のベースドメイン。
//
// 戻り値:
//   - 作った service。
func NewIngressDomainService(ingressReader IngressDomainReader, domainStore IngressDomainStore, verifier IngressDomainVerifier, workflowStarter IngressWorkflowStarter, baseDomain string) *IngressDomainService {
	// 渡されたものを保持する。
	return &IngressDomainService{ingressReader: ingressReader, domainStore: domainStore, verifier: verifier, workflowStarter: workflowStarter, baseDomain: baseDomain}
}

// IngressDomainVerifyResult は、確認の結果。
type IngressDomainVerifyResult struct {
	// Domain は、確認後のドメイン。
	Domain model.IngressDomain

	// Verified は、確認できたか(すでに確認済みだった場合も true)。
	Verified bool

	// Reason は、確認できなかった理由(利用者に見せる。確認できたときは空)。
	Reason string
}

// List は、ingress に付いているカスタムドメインと、ingress を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - ingress と、ドメインの一覧。ingress が無いときは not_found。
func (ingressDomainService *IngressDomainService) List(requestContext context.Context, userID string, projectID string, ingressID string) (model.Ingress, []model.IngressDomain, error) {
	// ingress を読む(持ち主でなければ not_found)。
	ingress, findError := ingressDomainService.ingressReader.FindByID(requestContext, userID, projectID, ingressID)
	if findError != nil {
		return model.Ingress{}, nil, findError
	}
	// ドメインを読む。
	domains, listError := ingressDomainService.domainStore.List(requestContext, userID, projectID, ingressID)
	if listError != nil {
		return model.Ingress{}, nil, listError
	}
	return ingress, domains, nil
}

// Add は、ingress にカスタムドメインを(確認待ちで)追加する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - rawHostname: 利用者が入力したドメイン名。
//
// 戻り値:
//   - 追加したドメインと、ingress。形が不正なら invalid_request、削除中の ingress、上限、重複は conflict。
func (ingressDomainService *IngressDomainService) Add(requestContext context.Context, userID string, projectID string, ingressID string, rawHostname string) (model.IngressDomain, model.Ingress, error) {
	// ドメイン名を整えて、形を確かめる。
	hostname := model.NormalizeHostname(rawHostname)
	if validateError := model.ValidateCustomHostname(hostname, ingressDomainService.baseDomain); validateError != nil {
		return model.IngressDomain{}, model.Ingress{}, validateError
	}
	// ingress を読み、削除中でないことを確かめる。
	ingress, findError := ingressDomainService.ingressReader.FindByID(requestContext, userID, projectID, ingressID)
	if findError != nil {
		return model.IngressDomain{}, model.Ingress{}, findError
	}
	if ingress.Status == model.IngressStatusDeleting {
		return model.IngressDomain{}, model.Ingress{}, apperror.New(apperror.KindConflict, "削除中の ingress には、ドメインを追加できません")
	}
	// 確認のための値を作って、保存する。
	token, tokenError := model.NewIngressDomainToken()
	if tokenError != nil {
		return model.IngressDomain{}, model.Ingress{}, tokenError
	}
	domain := model.IngressDomain{ID: model.IngressDomainIDPrefix + uuid.NewString(), IngressID: ingressID, Hostname: hostname, Token: token}
	if createError := ingressDomainService.domainStore.Create(requestContext, userID, projectID, &domain); createError != nil {
		return model.IngressDomain{}, model.Ingress{}, createError
	}
	return domain, ingress, nil
}

// Verify は、DNS を見て、ドメインの持ち主であることを確かめ、確認できたら、ルーティングに加える。
// すでに確認済みのドメインに対しては、IngressRoute の作り直しを、もう一度依頼する(やり直しのため)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - domainID: ドメインの ID。
//
// 戻り値:
//   - 確認の結果と、ingress。DNS が合わないのは、エラーではなく、Verified が false の結果。
//     ingress が削除中、または、別の ingress がすでに確認済みのときは conflict。
func (ingressDomainService *IngressDomainService) Verify(requestContext context.Context, userID string, projectID string, ingressID string, domainID string) (IngressDomainVerifyResult, model.Ingress, error) {
	// ingress とドメインを読む。
	ingress, findError := ingressDomainService.ingressReader.FindByID(requestContext, userID, projectID, ingressID)
	if findError != nil {
		return IngressDomainVerifyResult{}, model.Ingress{}, findError
	}
	if ingress.Status == model.IngressStatusDeleting {
		return IngressDomainVerifyResult{}, model.Ingress{}, apperror.New(apperror.KindConflict, "削除中の ingress の、ドメインは確認できません")
	}
	domain, domainError := ingressDomainService.domainStore.Find(requestContext, userID, projectID, ingressID, domainID)
	if domainError != nil {
		return IngressDomainVerifyResult{}, model.Ingress{}, domainError
	}
	// 確認済みでなければ、DNS を見る。合わなければ、理由を返して終わる。
	if !domain.IsVerified() {
		verified, reason := ingressDomainService.verifier.Verify(requestContext, domain.Hostname, domain.Token, ingress.Host)
		if !verified {
			return IngressDomainVerifyResult{Domain: domain, Verified: false, Reason: reason}, ingress, nil
		}
		// 確認済みにする。別の ingress が先に確認済みなら、conflict にする。
		if markError := ingressDomainService.domainStore.MarkVerified(requestContext, domain.ID); markError != nil {
			if errors.Is(markError, repository.ErrIngressDomainTaken) {
				return IngressDomainVerifyResult{}, model.Ingress{}, apperror.New(apperror.KindConflict, "このドメインは、すでに別の ingress で使われています")
			}
			return IngressDomainVerifyResult{}, model.Ingress{}, markError
		}
	}
	// 確認後の状態を読み直して、ルーティングに加える。
	domain, domainError = ingressDomainService.domainStore.Find(requestContext, userID, projectID, ingressID, domainID)
	if domainError != nil {
		return IngressDomainVerifyResult{}, model.Ingress{}, domainError
	}
	if reapplyError := ingressDomainService.reapplyIfRouted(requestContext, ingress); reapplyError != nil {
		return IngressDomainVerifyResult{}, model.Ingress{}, reapplyError
	}
	return IngressDomainVerifyResult{Domain: domain, Verified: true}, ingress, nil
}

// Remove は、ingress からカスタムドメインを外す。確認済みのドメインなら、ルーティングからも外す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - domainID: ドメインの ID。
//
// 戻り値:
//   - ドメインや ingress が無いときは not_found。IngressRoute の作り直しを始められなければ internal。
func (ingressDomainService *IngressDomainService) Remove(requestContext context.Context, userID string, projectID string, ingressID string, domainID string) error {
	// ingress を読む(持ち主でなければ not_found)。
	ingress, findError := ingressDomainService.ingressReader.FindByID(requestContext, userID, projectID, ingressID)
	if findError != nil {
		return findError
	}
	// ドメインを外す。
	removed, deleteError := ingressDomainService.domainStore.Delete(requestContext, userID, projectID, ingressID, domainID)
	if deleteError != nil {
		return deleteError
	}
	// 確認済みだったものは、ルーティングからも外す(IngressRoute を作り直す)。
	if removed.IsVerified() {
		return ingressDomainService.reapplyIfRouted(requestContext, ingress)
	}
	return nil
}

// reapplyIfRouted は、ingress に、反映済みのルールがあるときだけ、IngressRoute の作り直しを依頼する。
// ルールが無いときは、IngressRoute が無いので、何もしない(あとでルールを反映するときに、ドメインも入る)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingress: 対象の ingress。
//
// 戻り値:
//   - 作り直しを始められなければ internal。
func (ingressDomainService *IngressDomainService) reapplyIfRouted(requestContext context.Context, ingress model.Ingress) error {
	// 反映済みのルールが無ければ、何もしない。
	rules, parseError := model.ParseIngressRules(ingress.Rules)
	if parseError != nil || len(rules) == 0 {
		return nil
	}
	// 作り直しの Workflow を開始する。
	if startError := ingressDomainService.workflowStarter.StartRouteReapply(requestContext, ingress.ID); startError != nil {
		return apperror.Wrap(apperror.KindInternal, "IngressRoute の作り直しを開始できませんでした", startError)
	}
	return nil
}
