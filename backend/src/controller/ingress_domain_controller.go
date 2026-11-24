package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/backend/src/service"
	"launchs/shared/model"
)

// IngressDomainUseCase は、ingress のカスタムドメインの処理を、controller から見たもの。service.IngressDomainService が実装する。
type IngressDomainUseCase interface {
	List(requestContext context.Context, userID string, projectID string, ingressID string) (model.Ingress, []model.IngressDomain, error)
	Add(requestContext context.Context, userID string, projectID string, ingressID string, rawHostname string) (model.IngressDomain, model.Ingress, error)
	Verify(requestContext context.Context, userID string, projectID string, ingressID string, domainID string) (service.IngressDomainVerifyResult, model.Ingress, error)
	Remove(requestContext context.Context, userID string, projectID string, ingressID string, domainID string) error
}

// AddIngressDomainRequest は、カスタムドメインの追加のリクエスト。
type AddIngressDomainRequest struct {
	// Hostname は、追加するドメイン名(必須。形は service で確かめる)。
	Hostname string `json:"hostname" validate:"required,max=253"`
}

// IngressDomainChallengeResponse は、所有の確認のために、DNS に設定してもらう内容。
type IngressDomainChallengeResponse struct {
	// TXTName は、TXT レコードの名前。
	TXTName string `json:"txtName"`

	// TXTValue は、TXT レコードの値。
	TXTValue string `json:"txtValue"`

	// CNAMEValue は、CNAME で向ける先(払い出されたホスト名)。TXT の代わりに、CNAME でも確認できる。
	CNAMEValue string `json:"cnameValue"`
}

// IngressDomainResponse は、カスタムドメイン 1 件のレスポンス。
type IngressDomainResponse struct {
	// DomainID は、ドメインの ID。
	DomainID string `json:"domainId"`

	// Hostname は、ドメイン名。
	Hostname string `json:"hostname"`

	// Status は、状態(pending=確認待ち、verified=確認済み)。
	Status string `json:"status"`

	// VerifiedAt は、確認した日時(RFC 3339)。確認待ちなら null。
	VerifiedAt *string `json:"verifiedAt"`

	// Challenge は、確認のために DNS に設定してもらう内容。
	Challenge IngressDomainChallengeResponse `json:"challenge"`
}

// ListIngressDomainsResponse は、カスタムドメインの一覧のレスポンス。
type ListIngressDomainsResponse struct {
	// Domains は、追加した順のドメイン(無ければ空の配列)。
	Domains []IngressDomainResponse `json:"domains"`
}

// VerifyIngressDomainResponse は、確認のレスポンス。
type VerifyIngressDomainResponse struct {
	// Verified は、確認できたか。
	Verified bool `json:"verified"`

	// Reason は、確認できなかった理由(確認できたときは空)。
	Reason string `json:"reason"`

	// Domain は、確認後のドメイン。
	Domain IngressDomainResponse `json:"domain"`
}

// IngressDomainController は、ingress のカスタムドメインの HTTP ハンドラーをまとめる。
type IngressDomainController struct {
	// useCase は、処理の本体。
	useCase IngressDomainUseCase
}

// NewIngressDomainController は、IngressDomainController を作る。
//
// 引数:
//   - useCase: カスタムドメインの処理。
//
// 戻り値:
//   - 作った controller。
func NewIngressDomainController(useCase IngressDomainUseCase) *IngressDomainController {
	// 渡された処理を保持する。
	return &IngressDomainController{useCase: useCase}
}

// newIngressDomainResponse は、ドメインを、レスポンスの形にする。
//
// 引数:
//   - domain: ドメイン。
//   - ingress: 付け先の ingress(CNAME の向き先に使う)。
//
// 戻り値:
//   - レスポンス。
func newIngressDomainResponse(domain model.IngressDomain, ingress model.Ingress) IngressDomainResponse {
	// 状態と、確認した日時を決める。
	status := "pending"
	var verifiedAt *string
	if domain.IsVerified() {
		status = "verified"
		formatted := domain.VerifiedAt.UTC().Format(time.RFC3339)
		verifiedAt = &formatted
	}
	return IngressDomainResponse{
		DomainID: domain.ID, Hostname: domain.Hostname, Status: status, VerifiedAt: verifiedAt,
		Challenge: IngressDomainChallengeResponse{TXTName: domain.ChallengeName(), TXTValue: domain.Token, CNAMEValue: ingress.Host},
	}
}

// List は、GET /projects/:projectId/ingresses/:ingressId/domains のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - ドメインの一覧(200)。ingress が無ければ 404。
func (ingressDomainController *IngressDomainController) List(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ingress とドメインを読む。
	ingress, domains, listError := ingressDomainController.useCase.List(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"))
	if listError != nil {
		return listError
	}
	// レスポンスにする(空でも配列)。
	responses := make([]IngressDomainResponse, 0, len(domains))
	for _, domain := range domains {
		responses = append(responses, newIngressDomainResponse(domain, ingress))
	}
	return echoContext.JSON(http.StatusOK, ListIngressDomainsResponse{Domains: responses})
}

// Add は、POST /projects/:projectId/ingresses/:ingressId/domains のハンドラー。ドメインを、確認待ちで追加する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 追加したドメイン(201)。形が不正なら 400、無ければ 404、削除中・上限・重複なら 409。
func (ingressDomainController *IngressDomainController) Add(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request AddIngressDomainRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 追加する。
	domain, ingress, addError := ingressDomainController.useCase.Add(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"), request.Hostname)
	if addError != nil {
		return addError
	}
	return echoContext.JSON(http.StatusCreated, newIngressDomainResponse(domain, ingress))
}

// Verify は、POST /projects/:projectId/ingresses/:ingressId/domains/:domainId/verify のハンドラー。DNS を見て、所有を確かめる。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 確認の結果(200。DNS が合わないときも 200 で、verified=false と理由)。無ければ 404、削除中・他の ingress が確認済みなら 409。
func (ingressDomainController *IngressDomainController) Verify(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 確認する。
	result, ingress, verifyError := ingressDomainController.useCase.Verify(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"), echoContext.Param("domainId"))
	if verifyError != nil {
		return verifyError
	}
	return echoContext.JSON(http.StatusOK, VerifyIngressDomainResponse{Verified: result.Verified, Reason: result.Reason, Domain: newIngressDomainResponse(result.Domain, ingress)})
}

// Remove は、DELETE /projects/:projectId/ingresses/:ingressId/domains/:domainId のハンドラー。ドメインを外す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404。
func (ingressDomainController *IngressDomainController) Remove(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 外す。
	if removeError := ingressDomainController.useCase.Remove(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"), echoContext.Param("domainId")); removeError != nil {
		return removeError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}
