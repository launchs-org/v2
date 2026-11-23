package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"launchs/auth/src/model"
	"launchs/auth/src/password"
	"launchs/auth/src/token"
	"launchs/shared/apperror"
	"launchs/shared/pagination"
)

// AdminUserStore は、管理のユースケースが使う、ユーザーの保存先。
type AdminUserStore interface {
	// Create は、ユーザーを保存する(招待は使わないので、ハッシュは空)。
	Create(requestContext context.Context, user *model.User, invitationCodeHash string) error

	// FindByID は、ID で、ユーザーを探す。
	FindByID(requestContext context.Context, userID string) (model.User, bool, error)

	// List は、ユーザーを新しい順に取得する。
	List(requestContext context.Context, limit int, after *pagination.Position) ([]model.User, *pagination.Position, error)

	// Ban は、ユーザーを停止し、refresh token を全て無効にする。
	Ban(requestContext context.Context, actorID string, userID string) error

	// Unban は、停止中のユーザーを戻す。
	Unban(requestContext context.Context, userID string) error

	// PromoteToAdmin は、小文字のユーザー名で、管理者にする。
	PromoteToAdmin(requestContext context.Context, userNameLower string) error

	// HasAdmin は、管理者(状態を問わない)が1人でもいるかを返す。
	HasAdmin(requestContext context.Context) (bool, error)
}

// AdminInvitationStore は、管理のユースケースが使う、招待の保存先。
type AdminInvitationStore interface {
	// Create は、招待を保存する。
	Create(requestContext context.Context, invitation *model.Invitation) error

	// List は、招待を新しい順に取得する。
	List(requestContext context.Context, limit int, after *pagination.Position) ([]model.Invitation, *pagination.Position, error)

	// Revoke は、招待を取り消す。
	Revoke(requestContext context.Context, invitationID string) error
}

// AdminFailureStore は、管理のユースケースが使う、ログイン失敗の記録の消去。
type AdminFailureStore interface {
	// Reset は、ユーザー名(小文字)の失敗の記録とロックを消す。
	Reset(requestContext context.Context, userNameLower string) error
}

// AdminLoginEventStore は、管理のユースケースが使う、ログイン履歴の読み出し。
type AdminLoginEventStore interface {
	// ListAll は、全ユーザーの履歴を新しい順に取得する(結果とユーザー名で絞り込める)。
	ListAll(requestContext context.Context, limit int, after *pagination.Position, result string, userNameKeyword string) ([]model.LoginEvent, *pagination.Position, error)
}

// AdminAuditStore は、管理操作の監査ログの保存先。
type AdminAuditStore interface {
	// Create は、監査ログを 1 件追記する。
	Create(requestContext context.Context, actorID string, action string, target string) error
}

// invitationCodeBytes は、招待コードの元になる乱数の長さ(バイト)。256ビットで、推測できない。
const invitationCodeBytes = 32

// AdminService は、アカウント管理のユースケース。管理の操作は、全て、使える状態の管理者だけが行える。
type AdminService struct {
	userStore       AdminUserStore
	invitationStore AdminInvitationStore
	failureStore    AdminFailureStore
	loginEventStore AdminLoginEventStore
	auditStore      AdminAuditStore
	appLogger       *slog.Logger
	hasher          PasswordHasher
	tokenService    *token.Service
}

// NewAdminService は、AdminService を作る。
//
// 引数:
//   - userStore: ユーザーの保存先。
//   - invitationStore: 招待の保存先。
//   - failureStore: ログイン失敗の記録の消去。
//   - appLogger: 管理操作の記録の出力先。
//   - hasher: パスワードのハッシュ。
//   - tokenService: トークンの検証。
//
// 戻り値:
//   - 作成した AdminService。
func NewAdminService(userStore AdminUserStore, invitationStore AdminInvitationStore, failureStore AdminFailureStore, appLogger *slog.Logger, hasher PasswordHasher, tokenService *token.Service) *AdminService {
	// 依存するものを持たせて返す。
	return &AdminService{userStore: userStore, invitationStore: invitationStore, failureStore: failureStore, appLogger: appLogger, hasher: hasher, tokenService: tokenService}
}

// Authorize は、access token の持ち主が、使える状態の管理者であることを確かめる。
//
// access token の role は、最大15分、古い可能性があるので、使わない。管理の操作は、毎回、DB の最新の状態で確かめる
// (管理者でなくなった人、停止された人が、残りの有効期間で、操作できないようにするため)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - accessToken: access token の文字列。
//
// 戻り値:
//   - 管理者のユーザー ID。使えない access token は unauthorized。管理者でない、または停止中なら forbidden。
func (adminService *AdminService) Authorize(requestContext context.Context, accessToken string) (string, error) {
	// 署名・期限・種類を確かめる。
	claims, parseError := adminService.tokenService.Parse(accessToken, token.TypeAccess)
	if parseError != nil {
		return "", errInvalidRefreshToken
	}
	// 持ち主を、最新の状態で読む。
	user, found, findError := adminService.userStore.FindByID(requestContext, claims.Subject)
	if findError != nil {
		return "", findError
	}
	// 存在しない、管理者でない、停止中のユーザーは、権限が無い。
	if !found || !user.CanAdminister() {
		return "", apperror.New(apperror.KindForbidden, "この操作は、管理者だけが行えます")
	}
	return user.ID, nil
}

// record は、管理の操作を、ログに残す(誰が、何を、誰・何に対して行ったか)。
//
// 引数:
//   - actorID: 操作した管理者の ID。
//   - action: 操作の名前。
//   - target: 対象の ID。
func (adminService *AdminService) record(actorID string, action string, target string) {
	// 監査のために、info のログに残す。
	adminService.appLogger.Info("管理操作", "actor", actorID, "action", action, "target", target)
	// 監査ログにも追記する(保存先が設定されているとき。保存できなくても、操作は止めず、警告を残す)。
	if adminService.auditStore != nil {
		auditContext, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
		defer cancel()
		if auditError := adminService.auditStore.Create(auditContext, actorID, action, target); auditError != nil {
			adminService.appLogger.Warn("監査ログを保存できなかった", "action", action, "error", auditError)
		}
	}
}

// ListUsers は、ユーザーを、新しい順に返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 1ページの件数。
//   - after: 前のページの最後の位置(最初は nil)。
//
// 戻り値:
//   - ユーザーの一覧と、次のページの位置。
func (adminService *AdminService) ListUsers(requestContext context.Context, limit int, after *pagination.Position) ([]model.User, *pagination.Position, error) {
	// 保存先から取得する。
	return adminService.userStore.List(requestContext, limit, after)
}

// GetUser は、ユーザーを1人返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザーの ID。
//
// 戻り値:
//   - ユーザー。無ければ not_found。
func (adminService *AdminService) GetUser(requestContext context.Context, userID string) (model.User, error) {
	// 探す。
	user, found, findError := adminService.userStore.FindByID(requestContext, userID)
	if findError != nil {
		return model.User{}, findError
	}
	if !found {
		return model.User{}, apperror.New(apperror.KindNotFound, "ユーザーが見つかりません")
	}
	return user, nil
}

// BanUser は、ユーザーを停止する(refresh token も、全て無効になる。発行済みの access token は、最大15分、使える)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - actorID: 操作する管理者の ID。
//   - userID: 停止するユーザーの ID。
//
// 戻り値:
//   - ユーザーが無ければ not_found。自分自身なら invalid_request。すでに停止中なら conflict。
func (adminService *AdminService) BanUser(requestContext context.Context, actorID string, userID string) error {
	// 停止して、記録する。
	if banError := adminService.userStore.Ban(requestContext, actorID, userID); banError != nil {
		return banError
	}
	adminService.record(actorID, "ban", userID)
	return nil
}

// UnbanUser は、停止中のユーザーを戻す(ログインし直せば使える)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - actorID: 操作する管理者の ID。
//   - userID: ユーザーの ID。
//
// 戻り値:
//   - ユーザーが無ければ not_found。停止中でなければ conflict。
func (adminService *AdminService) UnbanUser(requestContext context.Context, actorID string, userID string) error {
	// 戻して、記録する。
	if unbanError := adminService.userStore.Unban(requestContext, userID); unbanError != nil {
		return unbanError
	}
	adminService.record(actorID, "unban", userID)
	return nil
}

// UnlockUser は、ユーザーのログインのロック(失敗の回数を含む)を解除する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - actorID: 操作する管理者の ID。
//   - userID: ユーザーの ID。
//
// 戻り値:
//   - ユーザーが無ければ not_found。
func (adminService *AdminService) UnlockUser(requestContext context.Context, actorID string, userID string) error {
	// ユーザーを探して、その名前の失敗の記録を消す。
	user, getError := adminService.GetUser(requestContext, userID)
	if getError != nil {
		return getError
	}
	if resetError := adminService.failureStore.Reset(requestContext, user.UserNameLower); resetError != nil {
		return resetError
	}
	adminService.record(actorID, "unlock", userID)
	return nil
}

// NewInvitationCode は、推測できない招待コードを作る。
//
// 戻り値:
//   - コード(URL に使える文字だけ)。乱数を得られなければ internal_error。
func NewInvitationCode() (string, error) {
	// 暗号用の乱数から作る。
	randomBytes := make([]byte, invitationCodeBytes)
	if _, readError := rand.Read(randomBytes); readError != nil {
		return "", apperror.Wrap(apperror.KindInternal, "招待コードを作れませんでした", readError)
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

// CreateInvitation は、招待を発行する。コードは、このとき1度だけ返す(DB には、ハッシュだけを保存する)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - actorID: 発行する管理者の ID。
//   - lifetime: 有効期間(0 なら既定の7日。上限30日)。
//   - note: メモ(200バイトまで)。
//
// 戻り値:
//   - 保存した招待と、コード。期間やメモが不正なら invalid_request。
func (adminService *AdminService) CreateInvitation(requestContext context.Context, actorID string, lifetime time.Duration, note string) (model.Invitation, string, error) {
	// 期間とメモを確かめる。
	if lifetime == 0 {
		lifetime = model.DefaultInvitationLifetime
	}
	if lifetime < 0 || lifetime > model.MaxInvitationLifetime {
		return model.Invitation{}, "", apperror.New(apperror.KindInvalidRequest, fmt.Sprintf("有効期間は、1時間以上、%d日以内でなければなりません", int(model.MaxInvitationLifetime.Hours()/24)))
	}
	if len(note) > model.MaxInvitationNoteLength {
		return model.Invitation{}, "", apperror.New(apperror.KindInvalidRequest, "note が長すぎます")
	}
	// コードを作り、ハッシュだけを保存する。
	code, codeError := NewInvitationCode()
	if codeError != nil {
		return model.Invitation{}, "", codeError
	}
	invitation := model.Invitation{
		ID: model.InvitationIDPrefix + uuid.NewString(), CodeHash: HashInvitationCode(code), CreatedBy: &actorID,
		Note: note, ExpiresAt: time.Now().Add(lifetime), CreatedAt: time.Now(),
	}
	if createError := adminService.invitationStore.Create(requestContext, &invitation); createError != nil {
		return model.Invitation{}, "", createError
	}
	adminService.record(actorID, "invitation_create", invitation.ID)
	return invitation, code, nil
}

// ListInvitations は、招待を、新しい順に返す(コードは含まない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 1ページの件数。
//   - after: 前のページの最後の位置(最初は nil)。
//
// 戻り値:
//   - 招待の一覧と、次のページの位置。
func (adminService *AdminService) ListInvitations(requestContext context.Context, limit int, after *pagination.Position) ([]model.Invitation, *pagination.Position, error) {
	// 保存先から取得する。
	return adminService.invitationStore.List(requestContext, limit, after)
}

// auditWriteTimeout は、監査ログの保存を待つ時間の上限。
const auditWriteTimeout = 3 * time.Second

// SetAuditStore は、監査ログの保存先を設定する(起動のときに 1 回)。
//
// 引数:
//   - auditStore: 監査ログの保存先。
func (adminService *AdminService) SetAuditStore(auditStore AdminAuditStore) {
	// 保存先を持たせる。
	adminService.auditStore = auditStore
}

// SetLoginEventStore は、ログイン履歴の読み出し先を設定する(起動のときに 1 回。コンストラクターの引数を増やさないための後付け)。
//
// 引数:
//   - loginEventStore: ログイン履歴の読み出し先。
func (adminService *AdminService) SetLoginEventStore(loginEventStore AdminLoginEventStore) {
	// 保存先を持たせる。
	adminService.loginEventStore = loginEventStore
}

// ListLoginEvents は、全ユーザーのログイン履歴を、新しい順に返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 取得する件数の上限。
//   - after: 前のページの最後の位置。最初のページは nil。
//   - result: 結果で絞り込む(success か failure。空なら絞り込まない)。
//   - userNameKeyword: ユーザー名で絞り込む文字列(空なら絞り込まない)。
//
// 戻り値:
//   - 履歴の一覧と、次のページの位置。読み出し先が無ければ internal_error。
func (adminService *AdminService) ListLoginEvents(requestContext context.Context, limit int, after *pagination.Position, result string, userNameKeyword string) ([]model.LoginEvent, *pagination.Position, error) {
	// 読み出し先が設定されていなければ、サーバーの設定の誤り。
	if adminService.loginEventStore == nil {
		return nil, nil, apperror.New(apperror.KindInternal, "ログイン履歴の読み出し先が設定されていません")
	}
	// 結果の指定は、決まった値だけを受け付ける。
	if result != "" && result != "success" && result != "failure" {
		return nil, nil, apperror.New(apperror.KindInvalidRequest, "result は success か failure で指定してください")
	}
	// 保存先から取得する。
	return adminService.loginEventStore.ListAll(requestContext, limit, after, result, userNameKeyword)
}

// RevokeInvitation は、有効な招待を取り消す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - actorID: 操作する管理者の ID。
//   - invitationID: 招待の ID。
//
// 戻り値:
//   - 招待が無ければ not_found。有効でなければ conflict。
func (adminService *AdminService) RevokeInvitation(requestContext context.Context, actorID string, invitationID string) error {
	// 取り消して、記録する。
	if revokeError := adminService.invitationStore.Revoke(requestContext, invitationID); revokeError != nil {
		return revokeError
	}
	adminService.record(actorID, "invitation_revoke", invitationID)
	return nil
}

// CreateAdmin は、管理者のアカウントを作る(コマンドから使う。招待も、レート制限も、通らない)。
//
// 通常のサインアップと同じ、ユーザー名とパスワードの検査を行う。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userName: ユーザー名。
//   - plainPassword: パスワード。
//
// 戻り値:
//   - 作成したユーザー。入力が不正なら invalid_request、名前の重複は conflict。
func (adminService *AdminService) CreateAdmin(requestContext context.Context, userName string, plainPassword string) (model.User, error) {
	// 入力を検査する。
	if nameError := model.ValidateUserName(userName); nameError != nil {
		return model.User{}, nameError
	}
	if passwordError := password.Check(userName, plainPassword); passwordError != nil {
		return model.User{}, passwordError
	}
	// ハッシュを作って、管理者として保存する。
	passwordHash, hashError := adminService.hasher.Hash(plainPassword)
	if hashError != nil {
		return model.User{}, apperror.Wrap(apperror.KindInternal, "パスワードをハッシュできませんでした", hashError)
	}
	user := model.User{
		ID: model.UserIDPrefix + uuid.NewString(), UserName: userName, UserNameLower: model.NormalizeUserName(userName),
		PasswordHash: passwordHash, Status: model.UserStatusActive, Role: model.RoleAdmin,
	}
	if createError := adminService.userStore.Create(requestContext, &user, ""); createError != nil {
		return model.User{}, createError
	}
	adminService.record("command", "admin_create", user.ID)
	return user, nil
}

// BootstrapAdmin は、管理者が1人もいないときだけ、最初の管理者を作る(起動時に呼ぶ。何度呼んでも安全)。
//
// Kubernetes では、コマンドを手で実行しにくいので、Secret から渡した名前とパスワードで、起動時に作る。
//   - 管理者が1人でもいれば(停止中を含む)、何もしない。
//   - 複数の auth が同時に起動して、同時に作ろうとしても、片方が名前の重複で失敗するだけで、管理者は1人だけ作られる(失敗した方は、管理者がいることを確かめて、成功とする)。
//   - 同じ名前の一般ユーザーがすでにいるときは、管理者を作らず、エラーにする(`admin promote` で昇格する)。
//   - パスワードの検査は、通常のサインアップと同じ。弱ければ、エラー(起動を止めて、設定の間違いに気づけるようにする)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userName: 最初の管理者のユーザー名。
//   - plainPassword: パスワード。
//
// 戻り値:
//   - 作成したら true。すでに管理者がいて、何もしなかったら false。
//   - 入力が不正、同じ名前の一般ユーザーがいる、データベースのエラーのときのエラー。
func (adminService *AdminService) BootstrapAdmin(requestContext context.Context, userName string, plainPassword string) (bool, error) {
	// 管理者がいれば、何もしない。
	exists, existsError := adminService.userStore.HasAdmin(requestContext)
	if existsError != nil {
		return false, existsError
	}
	if exists {
		return false, nil
	}
	// 管理者を作る。
	if _, createError := adminService.CreateAdmin(requestContext, userName, plainPassword); createError != nil {
		// 同時に作られた(別の auth が先に作った)なら、管理者がいるので、成功とする。
		if apperror.KindOf(createError) == apperror.KindConflict {
			if nowExists, recheckError := adminService.userStore.HasAdmin(requestContext); recheckError == nil && nowExists {
				return false, nil
			}
			return false, apperror.Wrap(apperror.KindConflict, "同じ名前のユーザーがすでにいます(管理者にするには、admin promote を使う)", createError)
		}
		return false, createError
	}
	return true, nil
}

// PromoteToAdmin は、すでにあるユーザーを、管理者にする(コマンドから使う)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userName: ユーザー名(大文字小文字は区別しない)。
//
// 戻り値:
//   - ユーザーが無ければ not_found。すでに管理者、または停止中なら conflict。
func (adminService *AdminService) PromoteToAdmin(requestContext context.Context, userName string) error {
	// 昇格して、記録する。
	if promoteError := adminService.userStore.PromoteToAdmin(requestContext, model.NormalizeUserName(userName)); promoteError != nil {
		return promoteError
	}
	adminService.record("command", "admin_promote", model.NormalizeUserName(userName))
	return nil
}
