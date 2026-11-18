package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/pagination"
	"launchs/shared/repository"
)

// AccessLogUseCase は、AccessLogController が使う、アクセスログのユースケース。
type AccessLogUseCase interface {
	List(requestContext context.Context, userID string, projectID string, ingressID string, filter repository.AccessLogFilter, limit int, after *pagination.Position) ([]repository.AccessLog, *pagination.Position, error)
}

// AccessLogsResponse は、アクセスログの一覧のレスポンス。
type AccessLogsResponse struct {
	// Logs は、このページのログ(新しい順)。
	Logs []repository.AccessLog `json:"logs"`

	// NextCursor は、次のページを取る位置。最後のページなら null。
	NextCursor *string `json:"nextCursor"`
}

// AccessLogController は、アクセスログの API のハンドラー。
type AccessLogController struct {
	accessLogUseCase AccessLogUseCase
}

// NewAccessLogController は、AccessLogController を作る。
//
// 引数:
//   - accessLogUseCase: アクセスログのユースケース。
//
// 戻り値:
//   - 作成した AccessLogController。
func NewAccessLogController(accessLogUseCase AccessLogUseCase) *AccessLogController {
	// ユースケースを持たせて返す。
	return &AccessLogController{accessLogUseCase: accessLogUseCase}
}

// List は、GET /projects/:projectId/ingresses/:ingressId/access-logs のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。クエリ limit、cursor、status、method、ip、q を読む。
//
// 戻り値:
//   - ログの一覧(200)。クエリが不正なら 400、ingress が無ければ 404。
func (accessLogController *AccessLogController) List(echoContext *echo.Context) error {
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
	// 絞り込みを読んで、取得する。
	filter := repository.AccessLogFilter{Status: echoContext.QueryParam("status"), Method: echoContext.QueryParam("method"), ClientIP: echoContext.QueryParam("ip"), Query: echoContext.QueryParam("q")}
	logs, nextPosition, listError := accessLogController.accessLogUseCase.List(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("ingressId"), filter, limit, after)
	if listError != nil {
		return listError
	}
	// レスポンスにする(空でも配列にする)。
	response := AccessLogsResponse{Logs: logs}
	if response.Logs == nil {
		response.Logs = []repository.AccessLog{}
	}
	if nextPosition != nil {
		nextCursor := pagination.EncodeCursor(*nextPosition)
		response.NextCursor = &nextCursor
	}
	return echoContext.JSON(http.StatusOK, response)
}
