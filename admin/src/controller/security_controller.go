package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"launchs/admin/src/middleware"
	"launchs/admin/src/repository"
	"launchs/admin/src/service"
	"launchs/shared/apperror"
	"launchs/shared/jst"
	"launchs/shared/pagination"
)

// SecurityUseCase は、SecurityController が呼ぶユースケース。
type SecurityUseCase interface {
	Audit(requestContext context.Context, actor service.Actor, action string, target string, detail string) error
	ListBlocked(requestContext context.Context) ([]repository.BlockedIP, error)
	Block(requestContext context.Context, actor service.Actor, network string, reason string, expiresInHours int) (repository.BlockedIP, error)
	Unblock(requestContext context.Context, actor service.Actor, blockID string) error
	SuspiciousIPs(requestContext context.Context, hours int, minFailures int) ([]repository.SuspiciousIP, error)
	ListAudit(requestContext context.Context, limit int, after *pagination.Position, action string) ([]repository.AuditLog, *pagination.Position, error)
}

// SecurityController は、/admin-api/security の、不審な IP、ブロック、監査ログを担当する。
type SecurityController struct {
	useCase SecurityUseCase
}

// NewSecurityController は、SecurityController を作る。
//
// 引数:
//   - useCase: ユースケース。
//
// 戻り値:
//   - 作成した SecurityController。
func NewSecurityController(useCase SecurityUseCase) *SecurityController {
	// 依存するものを持たせて返す。
	return &SecurityController{useCase: useCase}
}

// actorOf は、操作している管理者(と、接続元の IP)を、リクエストから作る。
//
// 接続元は、ヘッダー X-Real-Ip(プロキシが付ける)があればそれを、なければ接続の相手を使う。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 操作している管理者。
func actorOf(echoContext *echo.Context) service.Actor {
	// 認証のミドルウェアが入れた管理者を取り出す。
	userID, userName := middleware.ActorFrom(echoContext)
	clientIP := echoContext.Request().Header.Get("X-Real-Ip")
	if clientIP == "" {
		clientIP = echoContext.RealIP()
	}
	return service.Actor{ID: userID, Name: userName, IP: clientIP}
}

// blockResponse は、ブロック 1 件を、レスポンスの形にする。
//
// 引数:
//   - block: ブロック。
//
// 戻り値:
//   - レスポンスの map。
func blockResponse(block repository.BlockedIP) map[string]any {
	// 期限は、あれば JST の文字列、無ければ null。
	var expiresAt *string
	if block.ExpiresAt != nil {
		formatted := jst.Format(*block.ExpiresAt)
		expiresAt = &formatted
	}
	return map[string]any{
		"blockId": block.ID, "network": block.Network, "reason": block.Reason, "createdBy": block.CreatedBy, "createdByName": block.CreatedByName,
		"createdAt": jst.Format(block.CreatedAt), "expiresAt": expiresAt,
	}
}

// ListSuspiciousIPs は、GET /admin-api/security/suspicious-ips のハンドラー。ログインの失敗が多い IP を返す(hours、minFailures)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 一覧(200)。
func (securityController *SecurityController) ListSuspiciousIPs(echoContext *echo.Context) error {
	// 期間と最小の失敗数を読む(既定は 24 時間、5 回)。
	hours, hoursError := intQuery(echoContext, "hours", 24)
	if hoursError != nil {
		return hoursError
	}
	minFailures, failuresError := intQuery(echoContext, "minFailures", 5)
	if failuresError != nil {
		return failuresError
	}
	ips, listError := securityController.useCase.SuspiciousIPs(echoContext.Request().Context(), hours, minFailures)
	if listError != nil {
		return listError
	}
	items := make([]map[string]any, 0, len(ips))
	for _, ip := range ips {
		items = append(items, map[string]any{"ip": ip.IP, "failures": ip.Failures, "successes": ip.Successes, "userNames": ip.UserNames, "lastSeen": jst.Format(ip.LastSeen), "blocked": ip.Blocked})
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"ips": items})
}

// intQuery は、整数のクエリを読む(無ければ既定)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//   - name: クエリの名前。
//   - defaultValue: 無いときの値。
//
// 戻り値:
//   - 値。整数でなければ invalid_request。
func intQuery(echoContext *echo.Context, name string, defaultValue int) (int, error) {
	// 無ければ既定を返す。
	text := echoContext.QueryParam(name)
	if text == "" {
		return defaultValue, nil
	}
	value, parseError := strconv.Atoi(text)
	if parseError != nil {
		return 0, apperror.New(apperror.KindInvalidRequest, name+" は整数で指定してください")
	}
	return value, nil
}

// ListBlocked は、GET /admin-api/security/blocked-ips のハンドラー。有効なブロックを返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 一覧(200)。
func (securityController *SecurityController) ListBlocked(echoContext *echo.Context) error {
	// 取得して、レスポンスにする。
	blocks, listError := securityController.useCase.ListBlocked(echoContext.Request().Context())
	if listError != nil {
		return listError
	}
	items := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		items = append(items, blockResponse(block))
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"blocks": items})
}

// blockRequest は、ブロックの追加のリクエスト。
type blockRequest struct {
	// Network は、IP か CIDR。
	Network string `json:"network"`

	// Reason は、理由。
	Reason string `json:"reason"`

	// ExpiresInHours は、期限(時間)。0 か省略は、解除するまで。
	ExpiresInHours int `json:"expiresInHours"`
}

// CreateBlock は、POST /admin-api/security/blocked-ips のハンドラー。IP(範囲)をブロックする。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 追加したブロック(200)。
func (securityController *SecurityController) CreateBlock(echoContext *echo.Context) error {
	// 知らない項目を拒否して、ボディを読む。
	var body blockRequest
	decoder := json.NewDecoder(echoContext.Request().Body)
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(&body); decodeError != nil {
		return apperror.New(apperror.KindInvalidRequest, "ボディが正しくありません: "+decodeError.Error())
	}
	block, blockError := securityController.useCase.Block(echoContext.Request().Context(), actorOf(echoContext), body.Network, body.Reason, body.ExpiresInHours)
	if blockError != nil {
		return blockError
	}
	return echoContext.JSON(http.StatusOK, blockResponse(block))
}

// DeleteBlock は、DELETE /admin-api/security/blocked-ips/:blockId のハンドラー。ブロックを解除する。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。無ければ 404。
func (securityController *SecurityController) DeleteBlock(echoContext *echo.Context) error {
	// 解除する。
	if unblockError := securityController.useCase.Unblock(echoContext.Request().Context(), actorOf(echoContext), echoContext.Param("blockId")); unblockError != nil {
		return unblockError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// ListAudit は、GET /admin-api/security/audit-logs のハンドラー。管理操作の監査ログを返す(action で絞り込み)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 一覧(200)。
func (securityController *SecurityController) ListAudit(echoContext *echo.Context) error {
	// ページを読む。
	limit, after, pageError := readPage(echoContext)
	if pageError != nil {
		return pageError
	}
	logs, next, listError := securityController.useCase.ListAudit(echoContext.Request().Context(), limit, after, echoContext.QueryParam("action"))
	if listError != nil {
		return listError
	}
	items := make([]map[string]any, 0, len(logs))
	for _, entry := range logs {
		items = append(items, map[string]any{
			"time": jst.Format(entry.OccurredAt), "actorId": entry.ActorID, "actorName": entry.ActorName, "action": entry.Action, "target": entry.Target, "detail": entry.Detail, "ip": entry.IP,
		})
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"logs": items, "nextCursor": nextCursor(next)})
}
