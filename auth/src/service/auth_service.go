// Package service は、認証のユースケース(サインアップ、ログイン、トークンの更新、ログアウト)を実装する。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"launchs/auth/src/model"
	"launchs/auth/src/password"
	"launchs/auth/src/token"
	"launchs/shared/apperror"
)

const (
	// SignupModeOpen と SignupModeInvite は、サインアップの公開範囲。
	SignupModeOpen   = "open"
	SignupModeInvite = "invite"

	// rateLimitKindLogin と rateLimitKindSignup は、IP ごとのレート制限の種類。
	rateLimitKindLogin  = "login"
	rateLimitKindSignup = "signup"
)

// UserStore は、ユースケースが使う、ユーザーの保存先。
type UserStore interface {
	// Create は、ユーザーを保存する(招待のハッシュがあれば、同じトランザクションで使用済みにする)。
	Create(requestContext context.Context, user *model.User, invitationCodeHash string) error

	// FindByNameLower は、小文字の名前で、ユーザーを探す。
	FindByNameLower(requestContext context.Context, userNameLower string) (model.User, bool, error)

	// FindByID は、ID で、ユーザーを探す。
	FindByID(requestContext context.Context, userID string) (model.User, bool, error)

	// UpdatePasswordHash は、パスワードのハッシュを、新しいものにする。
	UpdatePasswordHash(requestContext context.Context, userID string, passwordHash string) error
}

// RefreshTokenStore は、ユースケースが使う、refresh token の有効・無効の管理。
type RefreshTokenStore interface {
	// Create は、発行した refresh token を、有効なものとして保存する。
	Create(requestContext context.Context, jti string, userID string, expiresAt time.Time) error

	// FindActiveOwner は、jti が有効なら、持ち主のユーザーの ID を返す。
	FindActiveOwner(requestContext context.Context, jti string) (string, bool, error)

	// Revoke は、jti を無効にする。無効にしたら true。
	Revoke(requestContext context.Context, jti string) (bool, error)
}

// LoginFailureStore は、ユースケースが使う、ユーザー名ごとのログイン失敗とロックの管理。
type LoginFailureStore interface {
	// IsLocked は、ユーザー名が、いまロック中かを返す。
	IsLocked(requestContext context.Context, userNameLower string) (bool, error)

	// RecordFailure は、失敗を1回記録し、上限に達したらロックする。
	RecordFailure(requestContext context.Context, userNameLower string, maxFailures int, lockDuration time.Duration) error

	// Reset は、失敗の記録を消す。
	Reset(requestContext context.Context, userNameLower string) error
}

// RateLimitStore は、ユースケースが使う、IP ごとのレート制限。
type RateLimitStore interface {
	// Hit は、試行を1回記録し、許されるかと、許されないときの待ち時間を返す。
	Hit(requestContext context.Context, kind string, ip string, limit int, window time.Duration) (bool, time.Duration, error)
}

// LoginEventStore は、ユースケースが使う、ログイン履歴の保存先。
type LoginEventStore interface {
	// Create は、履歴を1件保存する。
	Create(requestContext context.Context, event model.LoginEvent) error

	// ListByUser は、ユーザーの履歴を、新しい順に取得する。
	ListByUser(requestContext context.Context, userID string, limit int) ([]model.LoginEvent, error)
}

// PasswordHasher は、ユースケースが使う、パスワードのハッシュの作成と検証。
type PasswordHasher interface {
	// Hash は、パスワードのハッシュを作る。
	Hash(plainPassword string) (string, error)

	// Verify は、パスワードがハッシュと一致するか、ハッシュを作り直すべきか、を返す。
	Verify(plainPassword string, encodedHash string) (bool, bool, error)
}

// Settings は、認証のユースケースの設定。回数と時間は、仮の値で、環境変数で変えられる。
type Settings struct {
	// SignupMode は、サインアップの公開範囲(open か invite)。
	SignupMode string

	// LoginMaxFailures と LoginLockDuration は、ユーザー名ごとの、ロックする失敗の回数と、ロックの時間。
	LoginMaxFailures  int
	LoginLockDuration time.Duration

	// LoginIPLimit と LoginIPWindow は、IP ごとのログイン試行の上限と、時間枠。
	LoginIPLimit  int
	LoginIPWindow time.Duration

	// SignupIPLimit と SignupIPWindow は、IP ごとのサインアップの上限と、時間枠。
	SignupIPLimit  int
	SignupIPWindow time.Duration
}

// RateLimitedError は、レート制限に達したときのエラー。何秒待てばよいかを持つ(429 の Retry-After に使う)。
type RateLimitedError struct {
	// RetryAfter は、待つ時間。
	RetryAfter time.Duration
}

// Error は、エラーの文字列を返す。
//
// 戻り値:
//   - エラーの説明。
func (rateLimitedError *RateLimitedError) Error() string {
	// 待つ時間を含める。
	return "リクエストが多すぎます。しばらくしてから、もう一度試してください: " + rateLimitedError.RetryAfter.String()
}

// AuthService は、認証のユースケース。
type AuthService struct {
	userStore         UserStore
	refreshTokenStore RefreshTokenStore
	loginFailureStore LoginFailureStore
	rateLimitStore    RateLimitStore
	loginEventStore   LoginEventStore
	appLogger         *slog.Logger
	hasher            PasswordHasher
	tokenService      *token.Service
	settings          Settings
	dummyHash         string
}

// NewAuthService は、AuthService を作る。
//
// ユーザーが存在しないときの、応答の時間をそろえるための、ダミーのハッシュを、ここで1つ作る。
//
// 引数:
//   - userStore: ユーザーの保存先。
//   - refreshTokenStore: refresh token の管理。
//   - loginFailureStore: ログイン失敗とロックの管理。
//   - rateLimitStore: IP ごとのレート制限。
//   - loginEventStore: ログイン履歴の保存先。
//   - appLogger: ログの出力先(履歴を保存できなかったときの警告に使う)。
//   - hasher: パスワードのハッシュ。
//   - tokenService: トークンの発行と検証。
//   - settings: 設定。
//
// 戻り値:
//   - 作成した AuthService。ダミーのハッシュを作れなければエラー。
func NewAuthService(userStore UserStore, refreshTokenStore RefreshTokenStore, loginFailureStore LoginFailureStore, rateLimitStore RateLimitStore, loginEventStore LoginEventStore, appLogger *slog.Logger, hasher PasswordHasher, tokenService *token.Service, settings Settings) (*AuthService, error) {
	// ダミーのハッシュを作る(中身は、誰にも知られない、ランダムなもの)。
	dummyHash, hashError := hasher.Hash(uuid.NewString())
	if hashError != nil {
		return nil, hashError
	}
	return &AuthService{
		userStore: userStore, refreshTokenStore: refreshTokenStore, loginFailureStore: loginFailureStore,
		rateLimitStore: rateLimitStore, loginEventStore: loginEventStore, appLogger: appLogger, hasher: hasher, tokenService: tokenService, settings: settings, dummyHash: dummyHash,
	}, nil
}

// checkRateLimit は、IP ごとのレート制限を確かめる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - kind: 種類("login" か "signup")。
//   - clientIP: クライアントの IP。
//   - limit: 時間枠の中で許す回数。
//   - window: 時間枠。
//
// 戻り値:
//   - 上限を超えたときは RateLimitedError。データベースのエラーは、そのまま。
func (authService *AuthService) checkRateLimit(requestContext context.Context, kind string, clientIP string, limit int, window time.Duration) error {
	// 記録して、判定する。
	allowed, retryAfter, hitError := authService.rateLimitStore.Hit(requestContext, kind, clientIP, limit, window)
	if hitError != nil {
		return hitError
	}
	if !allowed {
		return &RateLimitedError{RetryAfter: retryAfter}
	}
	return nil
}

// maxUserAgentLength は、履歴に保存する User-Agent の長さの上限(バイト)。
const maxUserAgentLength = 256

// record は、ログイン履歴を1件残す。
//
// 履歴の保存に失敗しても、ログインやサインアップは、失敗にしない(警告だけ残す)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザーの ID(存在しないユーザー名なら空)。
//   - userNameLower: 試行で指定された名前(小文字)。
//   - kind: 種類(signup か login)。
//   - failureReason: 失敗の理由(成功なら空)。
//   - clientIP: クライアントの IP。
//   - userAgent: クライアントの User-Agent。
func (authService *AuthService) record(requestContext context.Context, userID string, userNameLower string, kind string, failureReason string, clientIP string, userAgent string) {
	// 履歴の内容を組み立てる(User-Agent は、長いときは切り詰める)。
	if len(userAgent) > maxUserAgentLength {
		userAgent = userAgent[:maxUserAgentLength]
	}
	event := model.LoginEvent{UserNameLower: userNameLower, Kind: kind, Result: model.LoginEventSuccess, IP: clientIP, UserAgent: userAgent}
	if userID != "" {
		event.UserID = &userID
	}
	if failureReason != "" {
		event.Result, event.Reason = model.LoginEventFailure, &failureReason
	}
	// 保存する(失敗は、警告だけ)。
	if createError := authService.loginEventStore.Create(requestContext, event); createError != nil {
		authService.appLogger.Warn("ログイン履歴を保存できなかった", "kind", kind, "error", createError)
	}
}

// issueRefreshToken は、refresh token を発行して、有効なものとして保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - user: 持ち主のユーザー。
//
// 戻り値:
//   - refresh token の文字列。
func (authService *AuthService) issueRefreshToken(requestContext context.Context, user model.User) (string, error) {
	// 発行して、jti を DB に保存する。
	issued, issueError := authService.tokenService.IssueRefresh(user.ID, user.UserName)
	if issueError != nil {
		return "", apperror.Wrap(apperror.KindInternal, "トークンを発行できませんでした", issueError)
	}
	if saveError := authService.refreshTokenStore.Create(requestContext, issued.ID, user.ID, issued.ExpiresAt); saveError != nil {
		return "", saveError
	}
	return issued.Token, nil
}

// Signup は、ユーザーを登録して、refresh token を返す(サインアップ後は、そのままログイン状態になる)。
//
// 流れ: IP のレート制限 → 入力の検査(ユーザー名、パスワードの強さ) → 招待制なら招待コードの確認 → ハッシュ → 保存 → refresh token の発行。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - clientIP: クライアントの IP。
//   - userAgent: クライアントの User-Agent(履歴に残す)。
//   - userName: ユーザー名。
//   - plainPassword: パスワード。
//   - invitationCode: 招待コード(招待制のときだけ使う)。
//
// 戻り値:
//   - refresh token。
//   - レート制限は RateLimitedError。入力が不正なら invalid_request、名前の重複は conflict。
func (authService *AuthService) Signup(requestContext context.Context, clientIP string, userAgent string, userName string, plainPassword string, invitationCode string) (string, error) {
	// 1. IP のレート制限(失敗した試行も数える)。
	if limitError := authService.checkRateLimit(requestContext, rateLimitKindSignup, clientIP, authService.settings.SignupIPLimit, authService.settings.SignupIPWindow); limitError != nil {
		return "", limitError
	}
	// 2. 入力を検査する。
	if nameError := model.ValidateUserName(userName); nameError != nil {
		return "", nameError
	}
	if passwordError := password.Check(userName, plainPassword); passwordError != nil {
		return "", passwordError
	}
	// 3. 招待制のときは、招待コードが要る。
	invitationCodeHash := ""
	if authService.settings.SignupMode == SignupModeInvite {
		if strings.TrimSpace(invitationCode) == "" {
			return "", apperror.New(apperror.KindInvalidRequest, "招待コード(invitationCode)が必要です")
		}
		invitationCodeHash = HashInvitationCode(invitationCode)
	}
	// 4. ハッシュを作って、保存する(招待は、同じトランザクションで使用済みにする)。
	passwordHash, hashError := authService.hasher.Hash(plainPassword)
	if hashError != nil {
		return "", apperror.Wrap(apperror.KindInternal, "パスワードをハッシュできませんでした", hashError)
	}
	user := model.User{
		ID: model.UserIDPrefix + uuid.NewString(), UserName: userName, UserNameLower: model.NormalizeUserName(userName),
		PasswordHash: passwordHash, Status: model.UserStatusActive, Role: model.RoleUser,
	}
	if createError := authService.userStore.Create(requestContext, &user, invitationCodeHash); createError != nil {
		return "", createError
	}
	// 5. 履歴に残して、refresh token を発行する。
	authService.record(requestContext, user.ID, user.UserNameLower, model.LoginEventKindSignup, "", clientIP, userAgent)
	return authService.issueRefreshToken(requestContext, user)
}

// HashInvitationCode は、招待コードを、保存用のハッシュ(SHA-256 の16進数)にする。
//
// 引数:
//   - invitationCode: 招待コード。
//
// 戻り値:
//   - ハッシュ。
func HashInvitationCode(invitationCode string) string {
	// 前後の空白を除いて、SHA-256 にする。
	digest := sha256.Sum256([]byte(strings.TrimSpace(invitationCode)))
	return hex.EncodeToString(digest[:])
}

// errLoginFailed は、ログインできなかったときの、共通の内部エラー(理由を区別しない)。
var errLoginFailed = apperror.New(apperror.KindUnauthorized, "userName または password が正しくありません")

// Login は、ユーザー名とパスワードで認証して、refresh token を返す。
//
// 存在しない・パスワードの誤り・ロック中・BAN は、全て同じ 401 で、応答の時間も、(パスワードの照合をするため)近くなる。
// 失敗は、存在しないユーザー名にも、同じように数える。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - clientIP: クライアントの IP。
//   - userAgent: クライアントの User-Agent(履歴に残す)。
//   - userName: ユーザー名。
//   - plainPassword: パスワード。
//
// 戻り値:
//   - refresh token。
//   - レート制限は RateLimitedError。認証できなければ unauthorized(理由は区別しない)。
func (authService *AuthService) Login(requestContext context.Context, clientIP string, userAgent string, userName string, plainPassword string) (string, error) {
	// 1. IP のレート制限。
	if limitError := authService.checkRateLimit(requestContext, rateLimitKindLogin, clientIP, authService.settings.LoginIPLimit, authService.settings.LoginIPWindow); limitError != nil {
		return "", limitError
	}
	// 2. ユーザー名の形が不正なものは、記録せずに、同じ 401 にする(長い名前で、記録を膨らませないため)。ダミーの照合で、時間をそろえる。
	if model.ValidateUserName(userName) != nil || len(plainPassword) > password.MaxLength {
		_, _, _ = authService.hasher.Verify("dummy", authService.dummyHash)
		authService.record(requestContext, "", "", model.LoginEventKindLogin, model.LoginReasonInvalidInput, clientIP, userAgent)
		return "", errLoginFailed
	}
	userNameLower := model.NormalizeUserName(userName)
	// 3. ロック中は、パスワードを確かめずに、同じ 401(ダミーの照合で、時間をそろえる)。
	locked, lockError := authService.loginFailureStore.IsLocked(requestContext, userNameLower)
	if lockError != nil {
		return "", lockError
	}
	user, found, findError := authService.userStore.FindByNameLower(requestContext, userNameLower)
	if findError != nil {
		return "", findError
	}
	// 4. パスワードを確かめる(ユーザーが無い、ロック中でも、ダミーのハッシュで照合する)。
	hashToVerify := authService.dummyHash
	if found {
		hashToVerify = user.PasswordHash
	}
	matched, needsRehash, verifyError := authService.hasher.Verify(plainPassword, hashToVerify)
	if verifyError != nil {
		return "", apperror.Wrap(apperror.KindInternal, "パスワードを確かめられませんでした", verifyError)
	}
	if locked {
		authService.record(requestContext, user.ID, userNameLower, model.LoginEventKindLogin, model.LoginReasonLocked, clientIP, userAgent)
		return "", errLoginFailed
	}
	// 5. 失敗: 失敗を記録して(存在しない名前にも)、同じ 401。BAN 中は、パスワードが合っても、同じ 401(失敗には数えない)。
	if !found || !matched {
		if recordError := authService.loginFailureStore.RecordFailure(requestContext, userNameLower, authService.settings.LoginMaxFailures, authService.settings.LoginLockDuration); recordError != nil {
			return "", recordError
		}
		// 履歴には、理由を残す(利用者には、区別して見せない)。
		failureReason := model.LoginReasonWrongPassword
		if !found {
			failureReason = model.LoginReasonUnknownUser
		}
		authService.record(requestContext, user.ID, userNameLower, model.LoginEventKindLogin, failureReason, clientIP, userAgent)
		return "", errLoginFailed
	}
	if !user.IsActive() {
		authService.record(requestContext, user.ID, userNameLower, model.LoginEventKindLogin, model.LoginReasonBanned, clientIP, userAgent)
		return "", errLoginFailed
	}
	// 6. 成功: 失敗の記録を消し、ハッシュのパラメータが古ければ作り直し、refresh token を発行する。
	if resetError := authService.loginFailureStore.Reset(requestContext, userNameLower); resetError != nil {
		return "", resetError
	}
	if needsRehash {
		if newHash, hashError := authService.hasher.Hash(plainPassword); hashError == nil {
			// 作り直しの失敗は、ログインを妨げない(次の機会に、もう一度試す)。
			_ = authService.userStore.UpdatePasswordHash(requestContext, user.ID, newHash)
		}
	}
	authService.record(requestContext, user.ID, userNameLower, model.LoginEventKindLogin, "", clientIP, userAgent)
	return authService.issueRefreshToken(requestContext, user)
}

// errInvalidRefreshToken は、refresh token が使えないときの、共通のエラー。
var errInvalidRefreshToken = apperror.New(apperror.KindUnauthorized, "トークンが無効です")

// Refresh は、refresh token から、access token を発行する(refresh token は、更新しない)。
//
// 確かめること: 署名・期限・種類(refresh)、jti が有効(ログアウトされていない)、ユーザーが BAN されていない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - refreshToken: refresh token の文字列。
//
// 戻り値:
//   - access token。使えない refresh token は、全て unauthorized。
func (authService *AuthService) Refresh(requestContext context.Context, refreshToken string) (string, error) {
	// 署名・期限・種類を確かめる。
	claims, parseError := authService.tokenService.Parse(refreshToken, token.TypeRefresh)
	if parseError != nil {
		return "", errInvalidRefreshToken
	}
	// jti が有効で、持ち主が、トークンの sub と一致することを確かめる。
	ownerID, active, activeError := authService.refreshTokenStore.FindActiveOwner(requestContext, claims.ID)
	if activeError != nil {
		return "", activeError
	}
	if !active || ownerID != claims.Subject {
		return "", errInvalidRefreshToken
	}
	// ユーザーが、いま使える状態であることを確かめる(名前と役割は、DB の最新の値を使う)。
	user, found, findError := authService.userStore.FindByID(requestContext, claims.Subject)
	if findError != nil {
		return "", findError
	}
	if !found || !user.IsActive() {
		return "", errInvalidRefreshToken
	}
	// access token を発行する。
	issued, issueError := authService.tokenService.IssueAccess(user.ID, user.UserName, user.Role)
	if issueError != nil {
		return "", apperror.Wrap(apperror.KindInternal, "トークンを発行できませんでした", issueError)
	}
	return issued.Token, nil
}

// Me は、access token の持ち主の情報を返す。
//
// access token は、有効期限だけで管理する(失効はしない)ので、トークンの claims から返す(DB は見ない)。
//
// 引数:
//   - accessToken: access token の文字列。
//
// 戻り値:
//   - ユーザーの ID と、ユーザー名。使えない access token は unauthorized。
func (authService *AuthService) Me(accessToken string) (string, string, error) {
	// 署名・期限・種類を確かめる。
	claims, parseError := authService.tokenService.Parse(accessToken, token.TypeAccess)
	if parseError != nil {
		return "", "", errInvalidRefreshToken
	}
	return claims.Subject, claims.Name, nil
}

// Logout は、refresh token を無効にする。以後、その refresh token では、access token を取れない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - refreshToken: refresh token の文字列。
//
// 戻り値:
//   - すでにログアウト済み、期限切れ、形式が不正なものは unauthorized。
func (authService *AuthService) Logout(requestContext context.Context, refreshToken string) error {
	// 署名・期限・種類を確かめる。
	claims, parseError := authService.tokenService.Parse(refreshToken, token.TypeRefresh)
	if parseError != nil {
		return errInvalidRefreshToken
	}
	// jti を無効にする(すでに無効なら、0行で、無効なトークン扱い)。
	revoked, revokeError := authService.refreshTokenStore.Revoke(requestContext, claims.ID)
	if revokeError != nil {
		return revokeError
	}
	if !revoked {
		return errInvalidRefreshToken
	}
	return nil
}

// LoginHistory は、access token の持ち主の、ログイン履歴を、新しい順に返す。
//
// そのユーザーへの試行(サインアップ、ログインの成功と失敗)だけを返す。失敗の理由も、本人には見せる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - accessToken: access token の文字列。
//   - limit: 返す件数の上限。
//
// 戻り値:
//   - 履歴。使えない access token は unauthorized。
func (authService *AuthService) LoginHistory(requestContext context.Context, accessToken string, limit int) ([]model.LoginEvent, error) {
	// 署名・期限・種類を確かめる。
	claims, parseError := authService.tokenService.Parse(accessToken, token.TypeAccess)
	if parseError != nil {
		return nil, errInvalidRefreshToken
	}
	// 持ち主の履歴を取る。
	return authService.loginEventStore.ListByUser(requestContext, claims.Subject, limit)
}

// IsRateLimited は、エラーが、レート制限のものかを調べ、そうなら待つ時間を返す。
//
// 引数:
//   - checkedError: 調べるエラー。
//
// 戻り値:
//   - 待つ時間と、レート制限のエラーだったか。
func IsRateLimited(checkedError error) (time.Duration, bool) {
	// RateLimitedError に変換できるか。
	var rateLimitedError *RateLimitedError
	if errors.As(checkedError, &rateLimitedError) {
		return rateLimitedError.RetryAfter, true
	}
	return 0, false
}
