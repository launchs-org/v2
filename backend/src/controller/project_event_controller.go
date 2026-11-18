package controller

import (
	"context"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/jst"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// defaultEventLimit は、Event の一覧で、limit を省略したときの件数。
const defaultEventLimit = 50

// ProjectEventUseCase は、ProjectEventController が呼ぶ、Event の一覧のビジネスルール。
//
// 実体は service の ProjectEventService。テストでは fake に差し替える。
type ProjectEventUseCase interface {
	List(requestContext context.Context, userID string, projectID string, filter model.ProjectEventFilter, limit int, after *pagination.Position) ([]model.ProjectEvent, *pagination.Position, error)
}

// ProjectEventResponse は、Event 一覧の1件。
type ProjectEventResponse struct {
	// EventID は、Event の ID("event-" + 通し番号)。
	EventID string `json:"eventId"`

	// Type は、Normal か Warning。
	Type string `json:"type"`

	// Reason は、理由(Pulling、BackOff、FailedScheduling など)。
	Reason string `json:"reason"`

	// Message は、本文。
	Message string `json:"message"`

	// ObjectKind は、対象の種類(Pod、Deployment など)。
	ObjectKind string `json:"objectKind"`

	// ObjectName は、対象の名前。
	ObjectName string `json:"objectName"`

	// DeployID は、対象が属する deploy の ID。属さなければ null。
	DeployID *string `json:"deployId"`

	// Count は、同じ Event が起きた回数。
	Count int `json:"count"`

	// FirstSeenAt は、最初に起きた時刻(JST)。
	FirstSeenAt string `json:"firstSeenAt"`

	// LastSeenAt は、最後に起きた時刻(JST)。
	LastSeenAt string `json:"lastSeenAt"`
}

// ListProjectEventsResponse は、Event 一覧のレスポンスのボディ。
type ListProjectEventsResponse struct {
	// Events は、Event の一覧(最後に起きた時刻の新しい順)。
	Events []ProjectEventResponse `json:"events"`

	// NextCursor は、次のページを取るためのカーソル。最後のページなら null。
	NextCursor *string `json:"nextCursor"`
}

// ProjectEventController は、/projects/:projectId/events の HTTP の入出力を担当する。
type ProjectEventController struct {
	eventUseCase ProjectEventUseCase
}

// NewProjectEventController は、ProjectEventController を作る。
//
// 引数:
//   - eventUseCase: Event の一覧のビジネスルール。
//
// 戻り値:
//   - 作成した ProjectEventController。
func NewProjectEventController(eventUseCase ProjectEventUseCase) *ProjectEventController {
	// ビジネスルールを持たせて返す。
	return &ProjectEventController{eventUseCase: eventUseCase}
}

// List は、プロジェクトの k8s の Event の一覧を返す(GET /projects/:projectId/events)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 一覧(200)。limit、cursor、type が不正なら invalid_request(400)、未認証なら unauthorized(401)、プロジェクトが無い・他のユーザーのものなら not_found(404)。
func (eventController *ProjectEventController) List(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 件数を読む(省略なら 50。1〜100 の整数だけ)。
	limit := defaultEventLimit
	if limitText := echoContext.QueryParam("limit"); limitText != "" {
		parsed, limitError := pagination.ParseLimit(limitText)
		if limitError != nil {
			return limitError
		}
		limit = parsed
	}
	// 続きの位置を読む。
	var after *pagination.Position
	if cursorText := echoContext.QueryParam("cursor"); cursorText != "" {
		position, cursorError := pagination.DecodeCursor(cursorText)
		if cursorError != nil {
			return cursorError
		}
		after = &position
	}
	// 絞り込みを読む(種類の検査は、ビジネスルールが行う)。
	filter := model.ProjectEventFilter{Type: echoContext.QueryParam("type"), DeployID: echoContext.QueryParam("deployId"), Reason: echoContext.QueryParam("reason")}
	// ビジネスルールに、取得を任せる。
	events, nextPosition, listError := eventController.eventUseCase.List(echoContext.Request().Context(), userID, echoContext.Param("projectId"), filter, limit, after)
	if listError != nil {
		return listError
	}
	// レスポンスにする。
	response := ListProjectEventsResponse{Events: make([]ProjectEventResponse, 0, len(events))}
	for _, event := range events {
		response.Events = append(response.Events, newProjectEventResponse(event))
	}
	if nextPosition != nil {
		nextCursor := pagination.EncodeCursor(*nextPosition)
		response.NextCursor = &nextCursor
	}
	return echoContext.JSON(http.StatusOK, response)
}

// newProjectEventResponse は、Event を、一覧の1件のレスポンスにする。
//
// 引数:
//   - event: Event のモデル。
//
// 戻り値:
//   - 一覧の1件。deploy が空なら、deployId は null。
func newProjectEventResponse(event model.ProjectEvent) ProjectEventResponse {
	// deploy に属さない(空の)ときは、null にする。
	var deployID *string
	if event.DeployID != "" {
		deployID = &event.DeployID
	}
	// 日時を JST の文字列にして、項目を写す。
	return ProjectEventResponse{EventID: "event-" + strconv.FormatInt(event.ID, 10), Type: event.Type, Reason: event.Reason, Message: event.Message, ObjectKind: event.ObjectKind, ObjectName: event.ObjectName, DeployID: deployID, Count: event.Count, FirstSeenAt: jst.Format(event.FirstSeenAt), LastSeenAt: jst.Format(event.LastSeenAt)}
}
