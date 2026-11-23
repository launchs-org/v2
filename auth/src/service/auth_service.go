// Package service は、認証のユースケース(サインアップ、ログイン、トークンの更新、ログアウト)を実装する。
package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"launchs/auth/src/model"
	"launchs/auth/src/password"
	"launchs/auth/src/token"
	"launchs/shared/apperror"
)

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
	ipBlockStore      IPBlockStore
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
//   - turnstileToken: 人間であることの確認のトークン(確認が有効なときだけ使う)。
//
// 戻り値:
//   - refresh token。
//   - レート制限は RateLimitedError。確認に失敗したら invalid_request(確認のサービスに届かなければ service_unavailable)。入力が不正なら invalid_request、名前の重複は conflict。
func (authService *AuthService) Signup(requestContext context.Context, clientIP string, userAgent string, userName string, plainPassword string, invitationCode string, turnstileToken string) (string, error) {
	// 0. ブロックされた IP は、何も確かめずに拒否する。
	if blockError := authService.checkBlocked(requestContext, model.LoginEventKindSignup, clientIP, userAgent); blockError != nil {
		return "", blockError
	}
	// 1. IP のレート制限(失敗した試行も数える)。
	if limitError := authService.checkRateLimit(requestContext, rateLimitKindSignup, clientIP, authService.settings.SignupIPLimit, authService.settings.SignupIPWindow); limitError != nil {
		return "", limitError
	}
	// 1.5. 人間であることの確認(有効なときだけ)。パスワードの照合などの、重い処理の前に行う。
	if captchaError := authService.verifyCaptcha(requestContext, turnstileToken, clientIP); captchaError != nil {
		return "", captchaError
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
	if errors.Is(hashError, password.ErrBusy) {
		// 計算が混み合っているときは、少し待たせる(429)。
		return "", &RateLimitedError{RetryAfter: busyRetryAfter}
	}
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
//   - turnstileToken: 人間であることの確認のトークン(確認が有効なときだけ使う)。
//
// 戻り値:
//   - refresh token。
//   - レート制限は RateLimitedError。確認に失敗したら invalid_request(確認のサービスに届かなければ service_unavailable)。認証できなければ unauthorized(理由は区別しない)。
func (authService *AuthService) Login(requestContext context.Context, clientIP string, userAgent string, userName string, plainPassword string, turnstileToken string) (string, error) {
	// 0. ブロックされた IP は、何も確かめずに拒否する。
	if blockError := authService.checkBlocked(requestContext, model.LoginEventKindLogin, clientIP, userAgent); blockError != nil {
		return "", blockError
	}
	// 1. IP のレート制限。
	if limitError := authService.checkRateLimit(requestContext, rateLimitKindLogin, clientIP, authService.settings.LoginIPLimit, authService.settings.LoginIPWindow); limitError != nil {
		return "", limitError
	}
	// 1.5. 人間であることの確認(有効なときだけ)。失敗は、ユーザー名の失敗(ロック)にも、ログイン履歴にも、数えない(他人のユーザー名で、ロックさせられないため)。
	if captchaError := authService.verifyCaptcha(requestContext, turnstileToken, clientIP); captchaError != nil {
		return "", captchaError
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
	if errors.Is(verifyError, password.ErrBusy) {
		// 計算が混み合っているときは、少し待たせる(429)。失敗には数えない。
		return "", &RateLimitedError{RetryAfter: busyRetryAfter}
	}
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
// ユーザーの ID と名前は、access token の claims から取る。役割(user か admin)は、昇格や降格がすぐに反映されるよう、DB から最新を読む。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - accessToken: access token の文字列。
//
// 戻り値:
//   - ユーザーの ID、ユーザー名、役割。使えない access token や、存在しないユーザーは unauthorized。
func (authService *AuthService) Me(requestContext context.Context, accessToken string) (string, string, string, error) {
	// 署名・期限・種類を確かめる。
	claims, parseError := authService.tokenService.Parse(accessToken, token.TypeAccess)
	if parseError != nil {
		return "", "", "", errInvalidRefreshToken
	}
	// 役割は、最新の状態を、DB から読む。
	user, found, findError := authService.userStore.FindByID(requestContext, claims.Subject)
	if findError != nil {
		return "", "", "", findError
	}
	// ユーザーが消えているときは、使えない access token として扱う。
	if !found {
		return "", "", "", errInvalidRefreshToken
	}
	return claims.Subject, claims.Name, user.Role, nil
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
