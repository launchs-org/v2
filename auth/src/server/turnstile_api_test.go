package server

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"launchs/auth/src/service"
	"launchs/shared/apperror"
)

// fakeCaptcha は、テスト用の CaptchaVerifier。"valid-token" だけを通し、受けた引数を記録する。
type fakeCaptcha struct {
	mutex       sync.Mutex
	tokens      []string
	remoteIPs   []string
	unavailable bool
}

// Verify は、引数を記録して、"valid-token" なら通す。unavailable のときは、確認のサービスに届かないことにする。
//
// 引数:
//   - requestContext: 使わない。
//   - token: トークン。
//   - remoteIP: クライアントの IP。
//
// 戻り値:
//   - 通れば nil。
func (captcha *fakeCaptcha) Verify(requestContext context.Context, token string, remoteIP string) error {
	// 受けた引数を記録する。
	captcha.mutex.Lock()
	defer captcha.mutex.Unlock()
	captcha.tokens = append(captcha.tokens, token)
	captcha.remoteIPs = append(captcha.remoteIPs, remoteIP)
	if captcha.unavailable {
		return apperror.New(apperror.KindUnavailable, "人間であることの確認のサービスに接続できません")
	}
	if token == "" {
		return apperror.New(apperror.KindInvalidRequest, "人間であることの確認(turnstileToken)が必要です")
	}
	if token != "valid-token" {
		return apperror.New(apperror.KindInvalidRequest, "人間であることの確認に失敗しました。もう一度試してください")
	}
	return nil
}

// callCount は、検証が呼ばれた回数を返す。
//
// 戻り値:
//   - 回数。
func (captcha *fakeCaptcha) callCount() int {
	// ロックして数える。
	captcha.mutex.Lock()
	defer captcha.mutex.Unlock()
	return len(captcha.tokens)
}

// authBody は、ログインやサインアップの JSON のボディを作る(turnstileToken は、空なら含めない)。
//
// 引数:
//   - userName: ユーザー名。
//   - password: パスワード。
//   - turnstileToken: トークン。
//
// 戻り値:
//   - JSON の文字列。
func authBody(userName string, password string, turnstileToken string) string {
	// トークンがあるときだけ、含める。
	if turnstileToken == "" {
		return `{"userName":"` + userName + `","password":"` + password + `"}`
	}
	return `{"userName":"` + userName + `","password":"` + password + `","turnstileToken":"` + turnstileToken + `"}`
}

// TestPublicConfig は、GET /auth/config が、確認が無効なら enabled=false(サイトキーなし)、有効なら enabled=true とサイトキーを返し、
// 認証なしで呼べて、シークレットを含まないことを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestPublicConfig(testing *testing.T) {
	// 無効な環境。
	disabled := newTestEnvironment(testing, nil)
	response := call(testing, disabled, http.MethodGet, "/auth/config", "", "", "203.0.113.1")
	turnstileConfig, _ := response.body["turnstile"].(map[string]any)
	if response.status != 200 || turnstileConfig["enabled"] != false || turnstileConfig["siteKey"] != nil {
		testing.Fatalf("無効で、サイトキーなしのはず: %d %v", response.status, response.body)
	}
	// 有効な環境。
	enabled := newTestEnvironment(testing, func(settings *service.Settings) { settings.Captcha = &fakeCaptcha{} })
	response = call(testing, enabled, http.MethodGet, "/auth/config", "", "", "203.0.113.1")
	turnstileConfig, _ = response.body["turnstile"].(map[string]any)
	if response.status != 200 || turnstileConfig["enabled"] != true || turnstileConfig["siteKey"] != "test-site-key" || len(response.body) != 1 || len(turnstileConfig) != 2 {
		testing.Fatalf("有効で、サイトキーだけを返すはず: %d %v", response.status, response.body)
	}
	if response.header.Get("Cache-Control") != "no-store" {
		testing.Errorf("no-store のはず: %q", response.header.Get("Cache-Control"))
	}
}

// TestLoginRequiresTurnstileWhenEnabled は、確認が有効なとき、トークンが無い・無効だと 400、正しいと通ること、
// 失敗が、ユーザー名のロックにも、ログイン履歴にも、数えられないこと(他人のユーザー名で、ロックさせられない)を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestLoginRequiresTurnstileWhenEnabled(testing *testing.T) {
	// 確認が有効で、ロックが 3 回の環境に、ユーザーを作っておく。
	captcha := &fakeCaptcha{}
	environment := newTestEnvironment(testing, func(settings *service.Settings) { settings.Captcha = captcha; settings.LoginMaxFailures = 3 })
	if signedUp := call(testing, environment, http.MethodPost, "/auth/signup", authBody("alice", goodPassword, "valid-token"), "", "203.0.113.1"); signedUp.status != 200 {
		testing.Fatalf("サインアップできるはず: %d %v", signedUp.status, signedUp.body)
	}
	// トークンが無い、無効: 400 で、パスワードの照合まで進まない(正しいパスワードでも、通らない)。
	for name, token := range map[string]string{"無い": "", "無効": "bad-token"} {
		response := call(testing, environment, http.MethodPost, "/auth/login", authBody("alice", goodPassword, token), "", "203.0.113.2")
		if response.status != http.StatusBadRequest || response.body["error"].(map[string]any)["code"] != "invalid_request" {
			testing.Errorf("トークンが%sなら 400 のはず: %d %v", name, response.status, response.body)
		}
	}
	// 確認の失敗を何度繰り返しても、alice はロックされない(失敗の記録も、履歴も、増えない)。
	for attempt := 0; attempt < 8; attempt++ {
		_ = call(testing, environment, http.MethodPost, "/auth/login", authBody("alice", "wrong password!!", "bad-token"), "", "203.0.113.3")
	}
	var failures, events int64
	environment.database.Table("login_failures").Count(&failures)
	environment.database.Table("login_events").Where("kind = 'login'").Count(&events)
	if failures != 0 || events != 0 {
		testing.Fatalf("確認の失敗は、ロックにも履歴にも、数えないはず: 失敗 %d、履歴 %d", failures, events)
	}
	// 正しいトークンと、正しいパスワードで、ログインできる(ロックされていない)。IP とトークンが、検証に渡る。
	ok := call(testing, environment, http.MethodPost, "/auth/login", authBody("alice", goodPassword, "valid-token"), "", "203.0.113.4")
	if ok.status != 200 || ok.body["token"] == nil {
		testing.Fatalf("ログインできるはず: %d %v", ok.status, ok.body)
	}
	if captcha.remoteIPs[len(captcha.remoteIPs)-1] != "203.0.113.4" || captcha.tokens[len(captcha.tokens)-1] != "valid-token" {
		testing.Fatalf("IP とトークンが、検証に渡るはず: %v %v", captcha.remoteIPs, captcha.tokens)
	}
	// 確認を通った後のパスワードの誤りは、これまでどおり 401 で、失敗に数える(ロックに向かう)。
	wrong := call(testing, environment, http.MethodPost, "/auth/login", authBody("alice", "wrong password!!", "valid-token"), "", "203.0.113.5")
	if wrong.status != 401 {
		testing.Fatalf("確認を通った後の誤りは 401 のはず: %d", wrong.status)
	}
	environment.database.Table("login_failures").Count(&failures)
	if failures != 1 {
		testing.Fatalf("パスワードの誤りは、数えるはず: %d", failures)
	}
}

// TestSignupRequiresTurnstileWhenEnabled は、確認が有効なとき、サインアップも、トークンが無い・無効だと 400 で、ユーザーが作られないこと、正しいと作られることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestSignupRequiresTurnstileWhenEnabled(testing *testing.T) {
	// 確認が有効な環境。
	environment := newTestEnvironment(testing, func(settings *service.Settings) { settings.Captcha = &fakeCaptcha{} })
	for name, token := range map[string]string{"無い": "", "無効": "bad-token"} {
		response := call(testing, environment, http.MethodPost, "/auth/signup", authBody("bob", goodPassword, token), "", "203.0.113.1")
		if response.status != http.StatusBadRequest {
			testing.Errorf("トークンが%sなら 400 のはず: %d %v", name, response.status, response.body)
		}
	}
	var users int64
	environment.database.Table("users").Count(&users)
	if users != 0 {
		testing.Fatalf("ユーザーは作られないはず: %d", users)
	}
	if ok := call(testing, environment, http.MethodPost, "/auth/signup", authBody("bob", goodPassword, "valid-token"), "", "203.0.113.1"); ok.status != 200 {
		testing.Fatalf("作れるはず: %d %v", ok.status, ok.body)
	}
	environment.database.Table("users").Count(&users)
	if users != 1 {
		testing.Fatalf("ユーザーが1人のはず: %d", users)
	}
}

// TestTurnstileDisabledIgnoresToken は、確認が無効なとき、トークンが無くても通り、あっても無視されることを確かめる(既定の挙動が、変わらない)。
//
// 引数:
//   - testing: テストの状態。
func TestTurnstileDisabledIgnoresToken(testing *testing.T) {
	environment := newTestEnvironment(testing, nil)
	if signedUp := call(testing, environment, http.MethodPost, "/auth/signup", authBody("carol", goodPassword, ""), "", "203.0.113.1"); signedUp.status != 200 {
		testing.Fatalf("トークンなしで、作れるはず: %d", signedUp.status)
	}
	if loggedIn := call(testing, environment, http.MethodPost, "/auth/login", authBody("carol", goodPassword, "whatever"), "", "203.0.113.1"); loggedIn.status != 200 {
		testing.Fatalf("トークンがあっても、無視して通るはず: %d", loggedIn.status)
	}
}

// TestTurnstileServiceUnavailableIs503 は、確認のサービスに届かないとき、503 になり、ユーザー名の失敗にも、履歴にも数えないことを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestTurnstileServiceUnavailableIs503(testing *testing.T) {
	captcha := &fakeCaptcha{unavailable: true}
	environment := newTestEnvironment(testing, func(settings *service.Settings) { settings.Captcha = captcha })
	for _, path := range []string{"/auth/login", "/auth/signup"} {
		response := call(testing, environment, http.MethodPost, path, authBody("dave", goodPassword, "valid-token"), "", "203.0.113.1")
		if response.status != http.StatusServiceUnavailable || response.body["error"].(map[string]any)["code"] != "service_unavailable" {
			testing.Errorf("%s は 503 のはず: %d %v", path, response.status, response.body)
		}
	}
	var failures int64
	environment.database.Table("login_failures").Count(&failures)
	if failures != 0 {
		testing.Fatalf("失敗に数えないはず: %d", failures)
	}
}

// TestTurnstileDoesNotWeakenRateLimit は、確認の失敗も、IP のレート制限に数えられ、上限を超えたら、確認(Cloudflare への問い合わせ)の前に、429 になることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestTurnstileDoesNotWeakenRateLimit(testing *testing.T) {
	captcha := &fakeCaptcha{}
	environment := newTestEnvironment(testing, func(settings *service.Settings) { settings.Captcha = captcha; settings.LoginIPLimit = 3 })
	for attempt := 0; attempt < 3; attempt++ {
		if response := call(testing, environment, http.MethodPost, "/auth/login", authBody("eve", goodPassword, "bad-token"), "", "203.0.113.9"); response.status != 400 {
			testing.Fatalf("上限までは、確認で 400 のはず: %d", response.status)
		}
	}
	limited := call(testing, environment, http.MethodPost, "/auth/login", authBody("eve", goodPassword, "valid-token"), "", "203.0.113.9")
	if limited.status != http.StatusTooManyRequests || limited.header.Get("Retry-After") == "" {
		testing.Fatalf("上限を超えたら 429 のはず: %d", limited.status)
	}
	if captcha.callCount() != 3 {
		testing.Fatalf("429 のときは、確認を呼ばないはず: %d", captcha.callCount())
	}
}

// TestTurnstileDoesNotAffectTokenEndpoints は、確認が有効でも、refresh、me、logout、JWKS には、トークンが要らないことを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestTurnstileDoesNotAffectTokenEndpoints(testing *testing.T) {
	environment := newTestEnvironment(testing, func(settings *service.Settings) { settings.Captcha = &fakeCaptcha{} })
	refreshToken := call(testing, environment, http.MethodPost, "/auth/signup", authBody("frank", goodPassword, "valid-token"), "", "203.0.113.1").body["token"].(string)
	access := call(testing, environment, http.MethodPost, "/auth/refresh", "", refreshToken, "203.0.113.1")
	if access.status != 200 {
		testing.Fatalf("refresh は、確認なしで通るはず: %d", access.status)
	}
	if me := call(testing, environment, http.MethodGet, "/auth/me", "", access.body["token"].(string), "203.0.113.1"); me.status != 200 {
		testing.Fatalf("me は通るはず: %d", me.status)
	}
	if jwks := call(testing, environment, http.MethodGet, "/auth/.well-known/jwks.json", "", "", "203.0.113.1"); jwks.status != 200 {
		testing.Fatalf("JWKS は通るはず: %d", jwks.status)
	}
	if logout := call(testing, environment, http.MethodPost, "/auth/logout", "", refreshToken, "203.0.113.1"); logout.status != 200 {
		testing.Fatalf("logout は通るはず: %d", logout.status)
	}
}
