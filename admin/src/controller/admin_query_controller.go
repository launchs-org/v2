package controller

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/admin/src/repository"
	"launchs/admin/src/service"
	"launchs/shared/apperror"
	"launchs/shared/jst"
	"launchs/shared/pagination"
)

// AdminQueryUseCase は、AdminQueryController が呼ぶユースケース。
type AdminQueryUseCase interface {
	ListDeploys(requestContext context.Context, limit int, after *pagination.Position, filter repository.AdminDeployFilter) ([]repository.AdminDeploy, *pagination.Position, error)
	ListAccessLogs(requestContext context.Context, limit int, after *pagination.Position, filter repository.AdminAccessLogFilter) ([]repository.AdminAccessLog, *pagination.Position, error)
	GetQuotaDefaults(requestContext context.Context) (repository.QuotaDefaults, error)
	SetQuotaDefaults(requestContext context.Context, defaults repository.QuotaDefaults) (repository.QuotaDefaults, error)
	Settings() []service.SettingItem
}

// AdminQueryController は、/admin-api の、一覧・既定のクォータ・設定を担当する。
type AdminQueryController struct {
	useCase AdminQueryUseCase
	auditor Auditor
}

// Auditor は、管理操作を監査ログに残すもの(service.SecurityService が満たす)。
type Auditor interface {
	// Audit は、管理操作を、監査ログに追記する。
	Audit(requestContext context.Context, actor service.Actor, action string, target string, detail string) error
}

// NewAdminQueryController は、AdminQueryController を作る。
//
// 引数:
//   - useCase: ユースケース。
//   - auditor: 管理操作を監査ログに残すもの。
//
// 戻り値:
//   - 作成した AdminQueryController。
func NewAdminQueryController(useCase AdminQueryUseCase, auditor Auditor) *AdminQueryController {
	// 依存するものを持たせて返す。
	return &AdminQueryController{useCase: useCase, auditor: auditor}
}

// readPage は、クエリの limit と cursor を読む。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 件数、位置(最初は nil)。不正なら invalid_request。
func readPage(echoContext *echo.Context) (int, *pagination.Position, error) {
	// 件数を読む。
	limit, limitError := pagination.ParseLimit(echoContext.QueryParam("limit"))
	if limitError != nil {
		return 0, nil, limitError
	}
	// カーソルがあれば、位置に戻す。
	cursor := echoContext.QueryParam("cursor")
	if cursor == "" {
		return limit, nil, nil
	}
	position, cursorError := pagination.DecodeCursor(cursor)
	if cursorError != nil {
		return 0, nil, cursorError
	}
	return limit, &position, nil
}

// nextCursor は、次のページの位置を、レスポンスのカーソルにする(最後のページなら nil)。
//
// 引数:
//   - next: 次のページの位置。
//
// 戻り値:
//   - カーソル。
func nextCursor(next *pagination.Position) *string {
	// 最後のページなら、無し。
	if next == nil {
		return nil
	}
	cursor := pagination.EncodeCursor(*next)
	return &cursor
}

// ListDeploys は、GET /admin-api/deploys のハンドラー。全ユーザーの deploy を返す(status、userId、q で絞り込み)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 一覧(200)。
func (adminQueryController *AdminQueryController) ListDeploys(echoContext *echo.Context) error {
	// ページと絞り込みを読む。
	limit, after, pageError := readPage(echoContext)
	if pageError != nil {
		return pageError
	}
	filter := repository.AdminDeployFilter{Status: echoContext.QueryParam("status"), UserID: echoContext.QueryParam("userId"), Keyword: echoContext.QueryParam("q")}
	deploys, next, listError := adminQueryController.useCase.ListDeploys(echoContext.Request().Context(), limit, after, filter)
	if listError != nil {
		return listError
	}
	// 日時を JST の文字列にして返す。
	items := make([]map[string]any, 0, len(deploys))
	for _, deploy := range deploys {
		items = append(items, map[string]any{
			"deployId": deploy.ID, "projectId": deploy.ProjectID, "projectName": deploy.ProjectName, "userId": deploy.UserID,
			"imageRef": deploy.ImageRef, "spec": deploy.Spec, "replicas": deploy.Replicas, "status": deploy.Status, "createdAt": jst.Format(deploy.CreatedAt),
		})
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"deploys": items, "nextCursor": nextCursor(next)})
}

// ListAccessLogs は、GET /admin-api/access-logs のハンドラー。全 ingress のアクセスログを返す(status、ip、q で絞り込み)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 一覧(200)。
func (adminQueryController *AdminQueryController) ListAccessLogs(echoContext *echo.Context) error {
	// ページと絞り込みを読む。
	limit, after, pageError := readPage(echoContext)
	if pageError != nil {
		return pageError
	}
	filter := repository.AdminAccessLogFilter{Status: echoContext.QueryParam("status"), ClientIP: echoContext.QueryParam("ip"), Keyword: echoContext.QueryParam("q")}
	logs, next, listError := adminQueryController.useCase.ListAccessLogs(echoContext.Request().Context(), limit, after, filter)
	if listError != nil {
		return listError
	}
	// 時刻を JST の文字列にして返す。
	items := make([]map[string]any, 0, len(logs))
	for _, accessLog := range logs {
		items = append(items, map[string]any{
			"time": jst.FormatMillis(accessLog.RequestedAt), "host": accessLog.Host, "ingressId": accessLog.IngressID, "method": accessLog.Method, "url": accessLog.URL,
			"status": accessLog.Status, "durationMs": accessLog.DurationMs, "clientIp": accessLog.ClientIP, "bytes": accessLog.Bytes,
		})
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"logs": items, "nextCursor": nextCursor(next)})
}

// GetQuotaDefaults は、GET /admin-api/quota-defaults のハンドラー。全体のクォータ(既定)を返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 既定(200)。
func (adminQueryController *AdminQueryController) GetQuotaDefaults(echoContext *echo.Context) error {
	// 取得して、そのまま返す。
	defaults, getError := adminQueryController.useCase.GetQuotaDefaults(echoContext.Request().Context())
	if getError != nil {
		return getError
	}
	return echoContext.JSON(http.StatusOK, defaults)
}

// PutQuotaDefaults は、PUT /admin-api/quota-defaults のハンドラー。全体のクォータ(既定)を置き換える。
//
// 知らない項目名は、書き間違いを防ぐため 400。全ての数の項目が必要(置き換え)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 置き換えた後の既定(200)。
func (adminQueryController *AdminQueryController) PutQuotaDefaults(echoContext *echo.Context) error {
	// 知らない項目を拒否して、ボディを読む。
	var defaults repository.QuotaDefaults
	decoder := json.NewDecoder(echoContext.Request().Body)
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(&defaults); decodeError != nil {
		return apperror.New(apperror.KindInvalidRequest, "ボディが正しくありません: "+decodeError.Error())
	}
	// 置き換える。
	updated, setError := adminQueryController.useCase.SetQuotaDefaults(echoContext.Request().Context(), defaults)
	if setError != nil {
		return setError
	}
	// 変更を、監査ログに残す(変更後の値を、補足にする)。
	detail, _ := json.Marshal(updated)
	if auditError := adminQueryController.auditor.Audit(echoContext.Request().Context(), actorOf(echoContext), "quota_defaults_update", "quota_defaults", string(detail)); auditError != nil {
		return auditError
	}
	return echoContext.JSON(http.StatusOK, updated)
}

// GetSettings は、GET /admin-api/settings のハンドラー。システムの設定(秘密でないもの)を返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 設定の一覧(200)。
func (adminQueryController *AdminQueryController) GetSettings(echoContext *echo.Context) error {
	// 一覧を返す。
	return echoContext.JSON(http.StatusOK, map[string]any{"settings": adminQueryController.useCase.Settings()})
}
