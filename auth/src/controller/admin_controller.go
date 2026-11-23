package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"launchs/auth/src/model"
	"launchs/shared/apperror"
	"launchs/shared/jst"
	"launchs/shared/pagination"
)

// AdminUseCase は、AdminController が使う、アカウント管理のユースケース。
type AdminUseCase interface {
	Authorize(requestContext context.Context, accessToken string) (string, error)
	ListUsers(requestContext context.Context, limit int, after *pagination.Position) ([]model.User, *pagination.Position, error)
	GetUser(requestContext context.Context, userID string) (model.User, error)
	BanUser(requestContext context.Context, actorID string, userID string) error
	UnbanUser(requestContext context.Context, actorID string, userID string) error
	UnlockUser(requestContext context.Context, actorID string, userID string) error
	CreateInvitation(requestContext context.Context, actorID string, lifetime time.Duration, note string) (model.Invitation, string, error)
	ListInvitations(requestContext context.Context, limit int, after *pagination.Position) ([]model.Invitation, *pagination.Position, error)
	RevokeInvitation(requestContext context.Context, actorID string, invitationID string) error
	ListLoginEvents(requestContext context.Context, limit int, after *pagination.Position, result string, userNameKeyword string) ([]model.LoginEvent, *pagination.Position, error)
}

// AdminLoginEventResponse は、ログイン履歴の1件。
type AdminLoginEventResponse struct {
	// EventID は、行の ID。
	EventID int64 `json:"eventId"`

	// UserID は、持ち主のユーザーの ID。存在しない名前への試行は null。
	UserID *string `json:"userId"`

	// UserName は、試行で指定された名前(小文字)。
	UserName string `json:"userName"`

	// Kind は、種類(signup か login)。
	Kind string `json:"kind"`

	// Result は、結果(success か failure)。
	Result string `json:"result"`

	// Reason は、失敗の理由。成功なら null。
	Reason *string `json:"reason"`

	// IP は、クライアントの IP。
	IP string `json:"ip"`

	// UserAgent は、クライアントの User-Agent。
	UserAgent string `json:"userAgent"`

	// OccurredAt は、試行の日時(JST)。
	OccurredAt string `json:"occurredAt"`
}

// LoginEventsResponse は、ログイン履歴の一覧のレスポンス。
type LoginEventsResponse struct {
	// Events は、履歴(新しい順)。
	Events []AdminLoginEventResponse `json:"events"`

	// NextCursor は、次のページのカーソル。最後のページなら null。
	NextCursor *string `json:"nextCursor"`
}

// AdminUserResponse は、管理 API が返す、ユーザー1人。パスワードのハッシュは、含めない。
type AdminUserResponse struct {
	// UserID は、ユーザーの ID。
	UserID string `json:"userId"`

	// UserName は、ユーザー名。
	UserName string `json:"userName"`

	// Status は、状態(active か banned)。
	Status string `json:"status"`

	// Role は、役割(user か admin)。
	Role string `json:"role"`

	// CreatedAt は、作成の日時(JST)。
	CreatedAt string `json:"createdAt"`
}

// AdminUsersResponse は、ユーザーの一覧のレスポンス。
type AdminUsersResponse struct {
	// Users は、ユーザーの一覧(新しい順)。
	Users []AdminUserResponse `json:"users"`

	// NextCursor は、次のページのカーソル。最後のページなら null。
	NextCursor *string `json:"nextCursor"`
}

// CreateInvitationRequest は、招待の発行のリクエスト。
type CreateInvitationRequest struct {
	// ExpiresInHours は、有効期間(時間)。省略(0)なら7日。最大720時間(30日)。
	ExpiresInHours int `json:"expiresInHours"`

	// Note は、メモ(誰に渡すか、など)。200バイトまで。
	Note string `json:"note"`
}

// CreateInvitationResponse は、招待の発行のレスポンス。コードは、このときだけ分かる。
type CreateInvitationResponse struct {
	// InvitationID は、招待の ID。
	InvitationID string `json:"invitationId"`

	// Code は、招待コード(サインアップの invitationCode に使う)。再取得はできない。
	Code string `json:"code"`

	// ExpiresAt は、有効期限(JST)。
	ExpiresAt string `json:"expiresAt"`
}

// InvitationResponse は、招待1件(コードは、含めない)。
type InvitationResponse struct {
	// InvitationID は、招待の ID。
	InvitationID string `json:"invitationId"`

	// Note は、メモ。
	Note string `json:"note"`

	// Status は、状態(active、used、expired、revoked)。
	Status string `json:"status"`

	// CreatedBy は、発行した管理者の ID。
	CreatedBy *string `json:"createdBy"`

	// CreatedAt は、発行の日時(JST)。
	CreatedAt string `json:"createdAt"`

	// ExpiresAt は、有効期限(JST)。
	ExpiresAt string `json:"expiresAt"`

	// UsedBy は、使ったユーザーの ID。未使用なら null。
	UsedBy *string `json:"usedBy"`

	// UsedAt は、使った日時(JST)。未使用なら null。
	UsedAt *string `json:"usedAt"`
}

// InvitationsResponse は、招待の一覧のレスポンス。
type InvitationsResponse struct {
	// Invitations は、招待の一覧(新しい順)。
	Invitations []InvitationResponse `json:"invitations"`

	// NextCursor は、次のページのカーソル。最後のページなら null。
	NextCursor *string `json:"nextCursor"`
}

// AdminController は、/auth/admin の HTTP の入出力を担当する。全てのハンドラーは、最初に、管理者であることを確かめる。
type AdminController struct {
	adminUseCase AdminUseCase
}

// NewAdminController は、AdminController を作る。
//
// 引数:
//   - adminUseCase: アカウント管理のユースケース。
//
// 戻り値:
//   - 作成した AdminController。
func NewAdminController(adminUseCase AdminUseCase) *AdminController {
	// 依存するものを持たせて返す。
	return &AdminController{adminUseCase: adminUseCase}
}

// authorize は、Authorization ヘッダの access token が、使える状態の管理者のものであることを確かめる。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 管理者のユーザー ID。トークンが無い・使えないなら unauthorized、管理者でないなら forbidden。
func (adminController *AdminController) authorize(echoContext *echo.Context) (string, error) {
	// トークンを取り出して、管理者かを確かめる。
	accessToken, tokenError := bearerToken(echoContext)
	if tokenError != nil {
		return "", tokenError
	}
	return adminController.adminUseCase.Authorize(echoContext.Request().Context(), accessToken)
}

// readPage は、クエリの limit と cursor を読む。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 件数と、前のページの最後の位置(最初のページは nil)。不正なら invalid_request。
func readPage(echoContext *echo.Context) (int, *pagination.Position, error) {
	// 件数を読む。
	limit, limitError := pagination.ParseLimit(echoContext.QueryParam("limit"))
	if limitError != nil {
		return 0, nil, limitError
	}
	// cursor があれば、位置に戻す。
	var after *pagination.Position
	if cursorText := echoContext.QueryParam("cursor"); cursorText != "" {
		position, cursorError := pagination.DecodeCursor(cursorText)
		if cursorError != nil {
			return 0, nil, cursorError
		}
		after = &position
	}
	return limit, after, nil
}

// encodeNextCursor は、次のページの位置を、レスポンス用のカーソルにする。
//
// 引数:
//   - next: 次のページの位置(最後のページなら nil)。
//
// 戻り値:
//   - カーソル(最後のページなら nil)。
func encodeNextCursor(next *pagination.Position) *string {
	// 最後のページなら、カーソルは無い。
	if next == nil {
		return nil
	}
	cursor := pagination.EncodeCursor(*next)
	return &cursor
}

// newAdminUserResponse は、ユーザーを、管理 API のレスポンスにする。
//
// 引数:
//   - user: ユーザー。
//
// 戻り値:
//   - レスポンス。
func newAdminUserResponse(user model.User) AdminUserResponse {
	// パスワードのハッシュを除いて、日時を JST にする。
	return AdminUserResponse{UserID: user.ID, UserName: user.UserName, Status: user.Status, Role: user.Role, CreatedAt: jst.Format(user.CreatedAt)}
}

// newInvitationResponse は、招待を、レスポンスにする。
//
// 引数:
//   - invitation: 招待。
//
// 戻り値:
//   - レスポンス(状態は、いまの時刻で求める)。
func newInvitationResponse(invitation model.Invitation) InvitationResponse {
	// 使った日時があれば JST の文字列にする。
	var usedAt *string
	if invitation.UsedAt != nil {
		formatted := jst.Format(*invitation.UsedAt)
		usedAt = &formatted
	}
	return InvitationResponse{
		InvitationID: invitation.ID, Note: invitation.Note, Status: invitation.Status(time.Now()), CreatedBy: invitation.CreatedBy,
		CreatedAt: jst.Format(invitation.CreatedAt), ExpiresAt: jst.Format(invitation.ExpiresAt), UsedBy: invitation.UsedBy, UsedAt: usedAt,
	}
}

// ListUsers は、GET /admin/users のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - ユーザーの一覧(200)。トークンが使えなければ 401、管理者でなければ 403、limit や cursor が不正なら 400。
func (adminController *AdminController) ListUsers(echoContext *echo.Context) error {
	// 管理者であることと、ページの指定を確かめる。
	if _, authorizeError := adminController.authorize(echoContext); authorizeError != nil {
		return authorizeError
	}
	limit, after, pageError := readPage(echoContext)
	if pageError != nil {
		return pageError
	}
	// 取得して、レスポンスにする。
	users, next, listError := adminController.adminUseCase.ListUsers(echoContext.Request().Context(), limit, after)
	if listError != nil {
		return listError
	}
	response := AdminUsersResponse{Users: make([]AdminUserResponse, 0, len(users)), NextCursor: encodeNextCursor(next)}
	for _, user := range users {
		response.Users = append(response.Users, newAdminUserResponse(user))
	}
	return echoContext.JSON(http.StatusOK, response)
}

// GetUser は、GET /admin/users/:userId のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - ユーザー(200)。トークンが使えなければ 401、管理者でなければ 403、ユーザーが無ければ 404。
func (adminController *AdminController) GetUser(echoContext *echo.Context) error {
	// 管理者であることを確かめる。
	if _, authorizeError := adminController.authorize(echoContext); authorizeError != nil {
		return authorizeError
	}
	// 取得して、レスポンスにする。
	user, getError := adminController.adminUseCase.GetUser(echoContext.Request().Context(), echoContext.Param("userId"))
	if getError != nil {
		return getError
	}
	return echoContext.JSON(http.StatusOK, newAdminUserResponse(user))
}

// userAction は、ユーザーを対象にする操作(停止、解除、ロック解除)のハンドラーを作る。
//
// 引数:
//   - action: 管理者の ID とユーザーの ID を受け取って、操作するもの。
//
// 戻り値:
//   - ハンドラー(成功したら、空のオブジェクトで 200)。
func (adminController *AdminController) userAction(action func(requestContext context.Context, actorID string, userID string) error) echo.HandlerFunc {
	// ハンドラーを返す。
	return func(echoContext *echo.Context) error {
		// 管理者であることを確かめて、操作する。
		actorID, authorizeError := adminController.authorize(echoContext)
		if authorizeError != nil {
			return authorizeError
		}
		if actionError := action(echoContext.Request().Context(), actorID, echoContext.Param("userId")); actionError != nil {
			return actionError
		}
		return echoContext.JSON(http.StatusOK, struct{}{})
	}
}

// BanUser は、POST /admin/users/:userId/ban のハンドラー。
//
// 戻り値:
//   - ハンドラー。停止できたら 200、自分自身なら 400、ユーザーが無ければ 404、すでに停止中なら 409。
func (adminController *AdminController) BanUser() echo.HandlerFunc {
	// ユースケースの停止につなぐ。
	return adminController.userAction(adminController.adminUseCase.BanUser)
}

// UnbanUser は、POST /admin/users/:userId/unban のハンドラー。
//
// 戻り値:
//   - ハンドラー。戻せたら 200、ユーザーが無ければ 404、停止中でなければ 409。
func (adminController *AdminController) UnbanUser() echo.HandlerFunc {
	// ユースケースの解除につなぐ。
	return adminController.userAction(adminController.adminUseCase.UnbanUser)
}

// UnlockUser は、POST /admin/users/:userId/unlock のハンドラー。
//
// 戻り値:
//   - ハンドラー。解除できたら 200、ユーザーが無ければ 404。
func (adminController *AdminController) UnlockUser() echo.HandlerFunc {
	// ユースケースのロック解除につなぐ。
	return adminController.userAction(adminController.adminUseCase.UnlockUser)
}

// CreateInvitation は、POST /admin/invitations のハンドラー。招待コードは、このレスポンスだけで分かる。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 招待(200)。トークンが使えなければ 401、管理者でなければ 403、期間やメモが不正なら 400。
func (adminController *AdminController) CreateInvitation(echoContext *echo.Context) error {
	// 管理者であることを確かめる。
	actorID, authorizeError := adminController.authorize(echoContext)
	if authorizeError != nil {
		return authorizeError
	}
	// リクエストを読む(ボディが空でも、既定の値で発行できる)。
	var request CreateInvitationRequest
	if echoContext.Request().ContentLength != 0 {
		if bindError := echoContext.Bind(&request); bindError != nil {
			return apperror.Wrap(apperror.KindInvalidRequest, "リクエストのボディを読み取れませんでした", bindError)
		}
	}
	// 発行する。
	invitation, code, createError := adminController.adminUseCase.CreateInvitation(echoContext.Request().Context(), actorID, time.Duration(request.ExpiresInHours)*time.Hour, request.Note)
	if createError != nil {
		return createError
	}
	return echoContext.JSON(http.StatusOK, CreateInvitationResponse{InvitationID: invitation.ID, Code: code, ExpiresAt: jst.Format(invitation.ExpiresAt)})
}

// ListInvitations は、GET /admin/invitations のハンドラー。コードは含まない。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 招待の一覧(200)。トークンが使えなければ 401、管理者でなければ 403、limit や cursor が不正なら 400。
func (adminController *AdminController) ListInvitations(echoContext *echo.Context) error {
	// 管理者であることと、ページの指定を確かめる。
	if _, authorizeError := adminController.authorize(echoContext); authorizeError != nil {
		return authorizeError
	}
	limit, after, pageError := readPage(echoContext)
	if pageError != nil {
		return pageError
	}
	// 取得して、レスポンスにする。
	invitations, next, listError := adminController.adminUseCase.ListInvitations(echoContext.Request().Context(), limit, after)
	if listError != nil {
		return listError
	}
	response := InvitationsResponse{Invitations: make([]InvitationResponse, 0, len(invitations)), NextCursor: encodeNextCursor(next)}
	for _, invitation := range invitations {
		response.Invitations = append(response.Invitations, newInvitationResponse(invitation))
	}
	return echoContext.JSON(http.StatusOK, response)
}

// RevokeInvitation は、DELETE /admin/invitations/:invitationId のハンドラー。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 空のオブジェクト(200)。トークンが使えなければ 401、管理者でなければ 403、招待が無ければ 404、有効でなければ 409。
func (adminController *AdminController) RevokeInvitation(echoContext *echo.Context) error {
	// 管理者であることを確かめて、取り消す。
	actorID, authorizeError := adminController.authorize(echoContext)
	if authorizeError != nil {
		return authorizeError
	}
	if revokeError := adminController.adminUseCase.RevokeInvitation(echoContext.Request().Context(), actorID, echoContext.Param("invitationId")); revokeError != nil {
		return revokeError
	}
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// ListLoginEvents は、GET /admin/login-events のハンドラー。全ユーザーのログイン履歴(失敗も含む)を返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。クエリの result(success か failure)と userName(部分一致)で絞り込める。
//
// 戻り値:
//   - 履歴の一覧(200)。トークンが使えなければ 401、管理者でなければ 403、指定が不正なら 400。
func (adminController *AdminController) ListLoginEvents(echoContext *echo.Context) error {
	// 管理者であることと、ページの指定を確かめる。
	if _, authorizeError := adminController.authorize(echoContext); authorizeError != nil {
		return authorizeError
	}
	limit, after, pageError := readPage(echoContext)
	if pageError != nil {
		return pageError
	}
	// 取得して、レスポンスにする。
	events, next, listError := adminController.adminUseCase.ListLoginEvents(echoContext.Request().Context(), limit, after, echoContext.QueryParam("result"), echoContext.QueryParam("userName"))
	if listError != nil {
		return listError
	}
	response := LoginEventsResponse{Events: make([]AdminLoginEventResponse, 0, len(events)), NextCursor: encodeNextCursor(next)}
	for _, event := range events {
		response.Events = append(response.Events, AdminLoginEventResponse{
			EventID: event.ID, UserID: event.UserID, UserName: event.UserNameLower, Kind: event.Kind, Result: event.Result,
			Reason: event.Reason, IP: event.IP, UserAgent: event.UserAgent, OccurredAt: jst.Format(event.OccurredAt),
		})
	}
	return echoContext.JSON(http.StatusOK, response)
}
