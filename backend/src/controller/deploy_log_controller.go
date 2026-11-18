package controller

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/backend/src/service"
	"launchs/shared/apperror"
	"launchs/shared/model"
)

// DeployLogUseCase は、DeployController が呼ぶ、ログの取得のビジネスルール。
//
// 実体は service の DeployLogService。テストでは fake に差し替える。
type DeployLogUseCase interface {
	ListLogPods(requestContext context.Context, userID string, deployID string) ([]model.DeployLogStream, error)
	GetLogs(requestContext context.Context, userID string, deployID string, podUID string, since int64) (service.LogChunk, error)
}

// GetLogs は、deploy の Pod のログを返す(GET /deploy/:deployId/logs?podId=...&since=...)。
//
// ログは、logcollector が DB に保存したもの(Pod の出力から、少し遅れる)。1回に、最大 1000 行(または 1MiB)まで返す。
// 続きは、レスポンスの nextSince を since に渡して取る。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - ログのレスポンス(200)。podId が無い・形が違う、since が不正なら invalid_request(400)、未認証なら unauthorized(401)、
//     deploy が無い・他のユーザーのもの・Pod がその deploy に属したことが無い(保持期間を過ぎて削除済みを含む)なら not_found(404)。
func (deployController *DeployController) GetLogs(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// podId(必須)を読む。「pod-」の後ろが、Pod の UID。
	podUID, hasPrefix := strings.CutPrefix(echoContext.QueryParam("podId"), podIDPrefix)
	if !hasPrefix || podUID == "" {
		return apperror.New(apperror.KindInvalidRequest, "podId は必須です(deploy の詳細の pods か logPods の podId を指定してください)")
	}
	// since(任意)を読む。省略したら 0(最初から)。0 以上の整数でなければ 400。
	var since int64
	if sinceText := echoContext.QueryParam("since"); sinceText != "" {
		parsed, parseError := strconv.ParseInt(sinceText, 10, 64)
		if parseError != nil || parsed < 0 {
			return apperror.New(apperror.KindInvalidRequest, "since は 0 以上の整数でなければなりません")
		}
		since = parsed
	}
	// ビジネスルールに、取得を任せる。
	chunk, getError := deployController.deployLogUseCase.GetLogs(echoContext.Request().Context(), userID, echoContext.Param("deployId"), podUID, since)
	if getError != nil {
		return getError
	}
	return echoContext.JSON(http.StatusOK, newDeployLogsResponse(chunk.Lines, chunk.NextSince))
}
