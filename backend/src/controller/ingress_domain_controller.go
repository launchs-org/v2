package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/model"
)

// IngressDomainUseCase は、ingress の追加のホスト名(利用者が選ぶサブドメイン)の処理を、controller から見たもの。service.IngressDomainService が実装する。
type IngressDomainUseCase interface {
	List(requestContext context.Context, userID string, projectID string, ingressID string) (model.Ingress, []model.IngressDomain, error)
	Add(requestContext context.Context, userID string, projectID string, ingressID string, rawSubdomain string) (model.IngressDomain, model.Ingress, error)
	Remove(requestContext context.Context, userID string, projectID string, ingressID string, domainID string) error
	BaseDomain() string
}

// AddIngressDomainRequest は、ホスト名の追加のリクエスト。
type AddIngressDomainRequest struct {
	// Subdomain は、追加する名前(必須。例: myapp。<名前>.<ベースドメイン> になる。形は service で確かめる)。
	Subdomain string `json:"subdomain" validate:"required,max=253"`
}

// IngressDomainResponse は、カスタムドメイン 1 件のレスポンス。
type IngressDomainResponse struct {
	// DomainID は、ドメインの ID。
	DomainID string `json:"domainId"`

	// Hostname は、ドメイン名。
	Hostname string `json:"hostname"`

	// Status は、状態(verified=有効)。今は、追加するとすぐに有効になるので、verified だけ。
	Status string `json:"status"`

	// VerifiedAt は、有効にした日時(RFC 3339)。
	VerifiedAt *string `json:"verifiedAt"`
}

// ListIngressDomainsResponse は、カスタムドメインの一覧のレスポンス。
type ListIngressDomainsResponse struct {
	// BaseDomain は、名前の後ろに付くベースドメイン(画面が、入力欄の後ろに出す)。
	BaseDomain string `json:"baseDomain"`

	// Domains は、追加した順のホスト名(無ければ空の配列)。
	Domains []IngressDomainResponse `json:"domains"`
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

// newIngressDomainResponse は、ホスト名を、レスポンスの形にする。
//
// 引数:
//   - domain: ドメイン。
//
// 戻り値:
//   - レスポンス。
func newIngressDomainResponse(domain model.IngressDomain) IngressDomainResponse {
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
	_, domains, listError := ingressDomainController.useCase.List(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"))
	if listError != nil {
		return listError
	}
	// レスポンスにする(空でも配列)。
	responses := make([]IngressDomainResponse, 0, len(domains))
	for _, domain := range domains {
		responses = append(responses, newIngressDomainResponse(domain))
	}
	return echoContext.JSON(http.StatusOK, ListIngressDomainsResponse{BaseDomain: ingressDomainController.useCase.BaseDomain(), Domains: responses})
}

// Add は、POST /projects/:projectId/ingresses/:ingressId/domains のハンドラー。ホスト名を追加する(すぐに有効になる)。
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
	domain, _, addError := ingressDomainController.useCase.Add(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"), request.Subdomain)
	if addError != nil {
		return addError
	}
	return echoContext.JSON(http.StatusCreated, newIngressDomainResponse(domain))
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
