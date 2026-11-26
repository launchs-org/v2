package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// IngressDomainStore は、追加のホスト名の保存先。IngressDomainRepository が実装する。
type IngressDomainStore interface {
	// Create は、ingress にホスト名を付ける。
	Create(requestContext context.Context, userID string, projectID string, domain *model.IngressDomain) error

	// List は、ingress に付いているホスト名を、追加した順に返す。
	List(requestContext context.Context, userID string, projectID string, ingressID string) ([]model.IngressDomain, error)

	// Delete は、ホスト名を外し、外したホスト名を返す。
	Delete(requestContext context.Context, userID string, projectID string, ingressID string, domainID string) (model.IngressDomain, error)
}

// IngressDomainReader は、ホスト名の操作のために、ingress を読むもの。IngressRepository が実装する。
type IngressDomainReader interface {
	// FindByID は、持ち主の ingress を 1 件返す。
	FindByID(requestContext context.Context, userID string, projectID string, ingressID string) (model.Ingress, error)
}

// IngressSubdomainPolicy は、利用者が選べるサブドメインの決まり(ベースドメイン)。
// ingresshost.Generator が実装する。
// 名前の後ろには、固定の「-run」を付けるので、自分たちが使う名前(api、www など)とは、重ならない。予約語の検査は要らない。
type IngressSubdomainPolicy interface {
	// BaseDomain は、ホスト名の後ろに付くベースドメイン(例: launchs.org)を返す。
	BaseDomain() string
}

// IngressDomainService は、ingress の追加のホスト名(利用者が選ぶサブドメイン。追加、削除)の処理をまとめる。
// 外部のドメイン(CNAME で向けるもの)は、Cloudflare の側の登録が要るので、今は扱わない。
type IngressDomainService struct {
	// ingressReader は、ingress を読む。
	ingressReader IngressDomainReader

	// domainStore は、ホスト名の保存先。
	domainStore IngressDomainStore

	// workflowStarter は、ホスト名が変わったときに、IngressRoute を作り直す Workflow を開始する。
	workflowStarter IngressWorkflowStarter

	// subdomainPolicy は、サブドメインの決まり(ベースドメインと、払い出さない名前)。nil なら、追加できない。
	subdomainPolicy IngressSubdomainPolicy
}

// NewIngressDomainService は、IngressDomainService を作る。
//
// 引数:
//   - ingressReader: ingress を読むもの。
//   - domainStore: ホスト名の保存先。
//   - workflowStarter: IngressRoute を作り直す Workflow を開始するもの。
//   - subdomainPolicy: サブドメインの決まり(nil なら、追加を断る)。
//
// 戻り値:
//   - 作った service。
func NewIngressDomainService(ingressReader IngressDomainReader, domainStore IngressDomainStore, workflowStarter IngressWorkflowStarter, subdomainPolicy IngressSubdomainPolicy) *IngressDomainService {
	// 渡されたものを保持する。
	return &IngressDomainService{ingressReader: ingressReader, domainStore: domainStore, workflowStarter: workflowStarter, subdomainPolicy: subdomainPolicy}
}

// BaseDomain は、ホスト名の後ろに付くベースドメインを返す(画面が、入力欄の後ろに出す)。
//
// 戻り値:
//   - ベースドメイン。決まりが無いときは空。
func (ingressDomainService *IngressDomainService) BaseDomain() string {
	// 決まりが無ければ、空を返す。
	if ingressDomainService.subdomainPolicy == nil {
		return ""
	}
	return ingressDomainService.subdomainPolicy.BaseDomain()
}

// List は、ingress に付いている追加のホスト名と、ingress を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - ingress と、ホスト名の一覧。ingress が無いときは not_found。
func (ingressDomainService *IngressDomainService) List(requestContext context.Context, userID string, projectID string, ingressID string) (model.Ingress, []model.IngressDomain, error) {
	// ingress を読む(持ち主でなければ not_found)。
	ingress, findError := ingressDomainService.ingressReader.FindByID(requestContext, userID, projectID, ingressID)
	if findError != nil {
		return model.Ingress{}, nil, findError
	}
	// ホスト名を読む。
	domains, listError := ingressDomainService.domainStore.List(requestContext, userID, projectID, ingressID)
	if listError != nil {
		return model.Ingress{}, nil, listError
	}
	return ingress, domains, nil
}

// Add は、ingress に、利用者が選んだ名前のホスト名(<名前>-run.<ベースドメイン>)を追加し、すぐにルーティングに加える。
// ベースドメインの下の名前は、ワイルドカードで、すでに Traefik に届くので、所有の確認は要らない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - rawSubdomain: 利用者が入力した名前(例: myapp → myapp-run.<ベースドメイン>)。ホスト名ごと貼り付けたとき(myapp-run.<ベースドメイン>)は、後ろの部分を取り除く。
//
// 戻り値:
//   - 追加したホスト名と、ingress。形が不正なら invalid_request。
//     削除中の ingress、上限、すでに使われている名前は conflict。IngressRoute の作り直しを始められなければ internal(このとき、追加は取り消す)。
func (ingressDomainService *IngressDomainService) Add(requestContext context.Context, userID string, projectID string, ingressID string, rawSubdomain string) (model.IngressDomain, model.Ingress, error) {
	// 決まり(ベースドメイン)が無ければ、追加できない。
	if ingressDomainService.subdomainPolicy == nil || ingressDomainService.subdomainPolicy.BaseDomain() == "" {
		return model.IngressDomain{}, model.Ingress{}, apperror.New(apperror.KindInternal, "ベースドメインが設定されていません")
	}
	baseDomain := ingressDomainService.subdomainPolicy.BaseDomain()
	// 名前を整える。ホスト名ごと貼り付けられたときは、ベースドメインと、固定の後ろを取り除く。そのあと、形を確かめる。
	subdomain := model.NormalizeHostname(rawSubdomain)
	if strings.HasSuffix(subdomain, "."+baseDomain) {
		subdomain = strings.TrimSuffix(strings.TrimSuffix(subdomain, "."+baseDomain), model.CustomSubdomainSuffix)
	}
	if validateError := model.ValidateSubdomain(subdomain); validateError != nil {
		return model.IngressDomain{}, model.Ingress{}, validateError
	}
	// ingress を読み、削除中でないことを確かめる。
	ingress, findError := ingressDomainService.ingressReader.FindByID(requestContext, userID, projectID, ingressID)
	if findError != nil {
		return model.IngressDomain{}, model.Ingress{}, findError
	}
	if ingress.Status == model.IngressStatusDeleting {
		return model.IngressDomain{}, model.Ingress{}, apperror.New(apperror.KindConflict, "削除中の ingress には、ホスト名を追加できません")
	}
	// 保存する(すぐに有効にする)。全体で重なる名前は、保存のときに conflict になる。
	token, tokenError := model.NewIngressDomainToken()
	if tokenError != nil {
		return model.IngressDomain{}, model.Ingress{}, tokenError
	}
	verifiedAt := time.Now()
	domain := model.IngressDomain{ID: model.IngressDomainIDPrefix + uuid.NewString(), IngressID: ingressID, Hostname: subdomain + model.CustomSubdomainSuffix + "." + baseDomain, Token: token, VerifiedAt: &verifiedAt}
	if createError := ingressDomainService.domainStore.Create(requestContext, userID, projectID, &domain); createError != nil {
		return model.IngressDomain{}, model.Ingress{}, createError
	}
	// ルーティングに加える。始められなければ、追加を取り消す(見た目だけ有効なホスト名を残さない)。
	if reapplyError := ingressDomainService.reapplyIfRouted(requestContext, ingress); reapplyError != nil {
		_, _ = ingressDomainService.domainStore.Delete(requestContext, userID, projectID, ingressID, domain.ID)
		return model.IngressDomain{}, model.Ingress{}, reapplyError
	}
	return domain, ingress, nil
}

// Remove は、ingress からホスト名を外し、ルーティングからも外す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - domainID: ホスト名の ID。
//
// 戻り値:
//   - ホスト名や ingress が無いときは not_found。IngressRoute の作り直しを始められなければ internal。
func (ingressDomainService *IngressDomainService) Remove(requestContext context.Context, userID string, projectID string, ingressID string, domainID string) error {
	// ingress を読む(持ち主でなければ not_found)。
	ingress, findError := ingressDomainService.ingressReader.FindByID(requestContext, userID, projectID, ingressID)
	if findError != nil {
		return findError
	}
	// ホスト名を外す。
	removed, deleteError := ingressDomainService.domainStore.Delete(requestContext, userID, projectID, ingressID, domainID)
	if deleteError != nil {
		return deleteError
	}
	// 有効だったものは、ルーティングからも外す(IngressRoute を作り直す)。
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
