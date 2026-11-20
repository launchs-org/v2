package controller

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// IngressUseCase は、IngressController が使う、ingress のユースケース。
type IngressUseCase interface {
	Create(requestContext context.Context, userID string, projectID string) (model.Ingress, error)
	List(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.Ingress, *pagination.Position, error)
	Get(requestContext context.Context, userID string, projectID string, ingressID string) (model.Ingress, error)
	Delete(requestContext context.Context, userID string, projectID string, ingressID string) error
	UpdateRules(requestContext context.Context, userID string, projectID string, ingressID string, rules []model.IngressRule) error
	DiscardRules(requestContext context.Context, userID string, projectID string, ingressID string) error
	ApplyRules(requestContext context.Context, userID string, projectID string, ingressID string) error
	UpdateAccessStats(requestContext context.Context, userID string, projectID string, ingressID string, enabled bool) (model.Ingress, error)
}

// UpdateIngressAccessStatsRequest は、アクセスの集計の有効・無効の切り替えのリクエスト。
type UpdateIngressAccessStatsRequest struct {
	// AccessStats は、有効にするなら true(必須。省略は 400)。
	AccessStats *bool `json:"accessStats" validate:"required"`
}

// IngressRuleRequest は、ルール1つのリクエストの形。path の書式と重複は、ユースケースで確かめる。
type IngressRuleRequest struct {
	// Path は、前方一致させる path。
	Path string `json:"path" validate:"required,max=200"`

	// ServiceID は、流す先の service の ID。
	ServiceID string `json:"serviceId" validate:"required"`

	// Port は、流す先の service の tcp のポート番号。
	Port int `json:"port" validate:"required,min=1,max=65535"`
}

// UpdateIngressRulesRequest は、ingress のルールの更新(一括置き換え)のリクエスト。空の配列は、ルールを全て消す。
type UpdateIngressRulesRequest struct {
	// Rules は、新しいルールの全件。
	Rules []IngressRuleRequest `json:"rules" validate:"required,max=50,dive"`
}

// IngressRuleResponse は、レスポンスに含める、ルール1つ。
type IngressRuleResponse struct {
	// Path は、前方一致させる path。
	Path string `json:"path"`

	// ServiceID は、流す先の service の ID。
	ServiceID string `json:"serviceId"`

	// Port は、流す先のポート番号。
	Port int `json:"port"`
}

// IngressResponse は、ingress 1件のレスポンス(作成と取得)。
type IngressResponse struct {
	// Tags は、タグ(昇順。無ければ空の配列)。
	Tags []string `json:"tags"`

	// IngressID は、ingress の ID。
	IngressID string `json:"ingressId"`

	// Host は、払い出されたホスト名。
	Host string `json:"host"`

	// Rules は、現在反映されているルール(空でも、null にはしない)。
	Rules []IngressRuleResponse `json:"rules"`

	// PendingRules は、まだ適用していないルール。変更が無ければ null。
	PendingRules []IngressRuleResponse `json:"pending_rules"`

	// Status は、状態(active、applying、failed、deleting)。
	Status string `json:"status"`

	// AccessStats は、アクセスの集計(launchs-stats)が有効か。
	AccessStats bool `json:"accessStats"`
}

// IngressSummaryResponse は、一覧に含める、ingress の概要。
type IngressSummaryResponse struct {
	// Tags は、タグ(昇順。無ければ空の配列)。
	Tags []string `json:"tags"`

	// IngressID は、ingress の ID。
	IngressID string `json:"ingressId"`

	// Host は、払い出されたホスト名。
	Host string `json:"host"`
}

// ListIngressesResponse は、ingress の一覧のレスポンス。
type ListIngressesResponse struct {
	// Ingresses は、このページの ingress。
	Ingresses []IngressSummaryResponse `json:"ingresses"`

	// NextCursor は、次のページを取る位置。最後のページなら null。
	NextCursor *string `json:"nextCursor"`
}

// IngressController は、ingress の API のハンドラー。
type IngressController struct {
	ingressUseCase IngressUseCase
}

// NewIngressController は、IngressController を作る。
//
// 引数:
//   - ingressUseCase: ingress のユースケース。
//
// 戻り値:
//   - 作成した IngressController。
func NewIngressController(ingressUseCase IngressUseCase) *IngressController {
	// ユースケースを持たせて返す。
	return &IngressController{ingressUseCase: ingressUseCase}
}

// Create は、POST /projects/:projectId/ingresses のハンドラー。ingress を作り、ホスト名を払い出す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 作成した ingress のレスポンス(200)。失敗したときはエラー。
func (ingressController *IngressController) Create(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ingress を作る。
	ingress, createError := ingressController.ingressUseCase.Create(echoContext.Request().Context(), userID, echoContext.Param("projectId"))
	if createError != nil {
		return createError
	}
	return echoContext.JSON(http.StatusOK, newIngressResponse(ingress))
}

// List は、GET /projects/:projectId/ingresses のハンドラー。ingress の一覧を、新しい順に返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 一覧のレスポンス(200)。limit や cursor が不正なら 400、プロジェクトが無ければ 404。
func (ingressController *IngressController) List(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// limit と cursor を読む。
	limit, limitError := pagination.ParseLimit(echoContext.QueryParam("limit"))
	if limitError != nil {
		return limitError
	}
	var after *pagination.Position
	if cursorText := echoContext.QueryParam("cursor"); cursorText != "" {
		position, cursorError := pagination.DecodeCursor(cursorText)
		if cursorError != nil {
			return cursorError
		}
		after = &position
	}
	// 一覧を取得する。
	ingresses, nextPosition, listError := ingressController.ingressUseCase.List(echoContext.Request().Context(), userID, echoContext.Param("projectId"), limit, after)
	if listError != nil {
		return listError
	}
	// レスポンスにする。
	response := ListIngressesResponse{Ingresses: make([]IngressSummaryResponse, 0, len(ingresses))}
	for _, ingress := range ingresses {
		response.Ingresses = append(response.Ingresses, IngressSummaryResponse{IngressID: ingress.ID, Tags: ingress.TagList(), Host: ingress.Host})
	}
	if nextPosition != nil {
		nextCursor := pagination.EncodeCursor(*nextPosition)
		response.NextCursor = &nextCursor
	}
	return echoContext.JSON(http.StatusOK, response)
}

// Get は、GET /projects/:projectId/ingresses/:ingressId のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - ingress のレスポンス(200)。無ければ 404。
func (ingressController *IngressController) Get(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ingress を取得する。
	ingress, getError := ingressController.ingressUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"))
	if getError != nil {
		return getError
	}
	return echoContext.JSON(http.StatusOK, newIngressResponse(ingress))
}

// Delete は、DELETE /projects/:projectId/ingresses/:ingressId のハンドラー。削除を受け付けて、すぐ応答する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除中なら 409。
func (ingressController *IngressController) Delete(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 削除を受け付ける。
	if deleteError := ingressController.ingressUseCase.Delete(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId")); deleteError != nil {
		return deleteError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// UpdateRules は、PUT /projects/:projectId/ingresses/:ingressId のハンドラー。ルールを、pending として保存する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。不正なら 400、無ければ 404、反映中・削除中なら 409。
func (ingressController *IngressController) UpdateRules(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request UpdateIngressRulesRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// ルールに変換して、保存する(path の書式や流す先は、ユースケースで確かめる)。
	rules := make([]model.IngressRule, 0, len(request.Rules))
	for _, ruleRequest := range request.Rules {
		rules = append(rules, model.IngressRule{Path: ruleRequest.Path, ServiceID: ruleRequest.ServiceID, Port: ruleRequest.Port})
	}
	if updateError := ingressController.ingressUseCase.UpdateRules(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"), rules); updateError != nil {
		return updateError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// ApplyRules は、POST /projects/:projectId/ingresses/:ingressId/apply のハンドラー。反映を始めて、すぐ応答する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除中、pending が無い、流す先が不正なら 409。
func (ingressController *IngressController) ApplyRules(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 反映を始める。
	if applyError := ingressController.ingressUseCase.ApplyRules(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId")); applyError != nil {
		return applyError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// DiscardRules は、POST /projects/:projectId/ingresses/:ingressId/discard のハンドラー。pending を破棄する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404、反映中・削除中、または pending が無ければ 409。
func (ingressController *IngressController) DiscardRules(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// pending を破棄する。
	if discardError := ingressController.ingressUseCase.DiscardRules(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId")); discardError != nil {
		return discardError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// newIngressResponse は、ingress から、レスポンスを作る。
//
// 引数:
//   - ingress: レスポンスにする ingress。
//
// 戻り値:
//   - レスポンス。rules は常に配列、pending_rules は無ければ null。
func newIngressResponse(ingress model.Ingress) IngressResponse {
	// 現在のルールは、空でも配列にする。
	rules := ingressRuleResponses(ingress.Rules)
	if rules == nil {
		rules = []IngressRuleResponse{}
	}
	// pending は、無ければ nil のまま(JSON の null)。空の配列は、空の配列として返す。
	var pendingRules []IngressRuleResponse
	if len(ingress.PendingRules) > 0 {
		pendingRules = ingressRuleResponses(ingress.PendingRules)
		if pendingRules == nil {
			pendingRules = []IngressRuleResponse{}
		}
	}
	return IngressResponse{IngressID: ingress.ID, Tags: ingress.TagList(), Host: ingress.Host, Rules: rules, PendingRules: pendingRules, Status: ingress.Status, AccessStats: ingress.AccessStats}
}

// ingressRuleResponses は、JSON のルールの一覧を、レスポンスの形にする。読めないときは nil。
//
// 引数:
//   - raw: ルールの JSON の配列。
//
// 戻り値:
//   - レスポンスのルールの一覧。
func ingressRuleResponses(raw []byte) []IngressRuleResponse {
	// 読み込む。読めなければ、空として扱う。
	var rules []model.IngressRule
	if len(raw) == 0 || json.Unmarshal(raw, &rules) != nil {
		return nil
	}
	// レスポンスの形にする。
	responses := make([]IngressRuleResponse, 0, len(rules))
	for _, rule := range rules {
		responses = append(responses, IngressRuleResponse{Path: rule.Path, ServiceID: rule.ServiceID, Port: rule.Port})
	}
	return responses
}

// UpdateAccessStats は、PATCH /projects/:projectId/ingresses/:ingressId のハンドラー。アクセスの集計の有効・無効を切り替える。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 更新後の ingress のレスポンス(200)。形が不正なら 400、無ければ 404、反映中・削除中なら 409。
func (ingressController *IngressController) UpdateAccessStats(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる。
	var request UpdateIngressAccessStatsRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 切り替える。
	ingress, updateError := ingressController.ingressUseCase.UpdateAccessStats(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"), *request.AccessStats)
	if updateError != nil {
		return updateError
	}
	return echoContext.JSON(http.StatusOK, newIngressResponse(ingress))
}
