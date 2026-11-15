package server

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"launchs/auth/src/clientip"
	"launchs/auth/src/keys"
	"launchs/auth/src/password"
	"launchs/auth/src/repository"
	"launchs/auth/src/service"
	"launchs/auth/src/token"
	"launchs/shared/database/testdatabase"
	"launchs/shared/logger"

	"log/slog"
)

// testEnvironment は、API のテストに必要なものの一式。
type testEnvironment struct {
	database *gorm.DB
	server   http.Handler
	settings service.Settings
	clock    *time.Time
	keyStore *keys.KeyStore
	tokens   *token.Service
}

// newTestEnvironment は、使い捨てのデータベースと、弱いハッシュのパラメータで、認証のサーバーを組み立てる。
//
// 引数:
//   - testing: テストの状態。
//   - mutate: 設定を変えたいときの関数(無ければ nil)。
//
// 戻り値:
//   - 組み立てた環境。
func newTestEnvironment(testing *testing.T, mutate func(*service.Settings)) *testEnvironment {
	// 使い捨てのデータベースと、一時ディレクトリの署名鍵を用意する。
	database := testdatabase.NewWithMigrations(testing, "auth")
	clock := time.Now()
	keyStore, keyError := keys.Load(testing.TempDir(), "", true, clock)
	if keyError != nil {
		testing.Fatal(keyError)
	}
	tokens := token.NewService(keyStore, func() time.Time { return clock })
	settings := service.Settings{
		SignupMode: service.SignupModeOpen, LoginMaxFailures: 5, LoginLockDuration: 15 * time.Minute,
		LoginIPLimit: 20, LoginIPWindow: time.Minute, SignupIPLimit: 10, SignupIPWindow: time.Hour,
	}
	if mutate != nil {
		mutate(&settings)
	}
	authService, serviceError := service.NewAuthService(repository.NewUserRepository(database), repository.NewRefreshTokenRepository(database),
		repository.NewLoginFailureRepository(database), repository.NewRateLimitRepository(database), repository.NewLoginEventRepository(database), logger.New(&bytes.Buffer{}, "auth", slog.LevelInfo),
		password.NewHasher(password.Parameters{MemoryKiB: 8, Iterations: 1, Parallelism: 1}), tokens, settings)
	if serviceError != nil {
		testing.Fatal(serviceError)
	}
	resolver, _ := clientip.NewResolver([]string{"127.0.0.0/8"})
	environment := &testEnvironment{database: database, settings: settings, clock: &clock, keyStore: keyStore, tokens: tokens}
	environment.server = New(logger.New(&bytes.Buffer{}, "auth", slog.LevelInfo), authService, keyStore, resolver)
	return environment
}

// response は、API の応答。
type response struct {
	status int
	header http.Header
	body   map[string]any
}

// call は、API を呼び出す(接続元は、信頼するプロキシの 127.0.0.1 にして、X-Forwarded-For で IP を指定する)。
//
// 引数:
//   - testing: テストの状態。
//   - environment: テストの環境。
//   - method: HTTP のメソッド。
//   - path: パス(/auth を含む)。
//   - body: ボディ(空なら無し)。
//   - bearer: Authorization の Bearer トークン(空なら無し)。
//   - ip: クライアントの IP。
//
// 戻り値:
//   - 応答。
func call(testing *testing.T, environment *testEnvironment, method string, path string, body string, bearer string, ip string) response {
	// リクエストを組み立てる。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:5000"
	request.Header.Set("X-Forwarded-For", ip)
	request.Header.Set("User-Agent", "test-agent/1.0")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	recorder := httptest.NewRecorder()
	environment.server.ServeHTTP(recorder, request)
	parsed := map[string]any{}
	_ = json.Unmarshal(recorder.Body.Bytes(), &parsed)
	return response{status: recorder.Code, header: recorder.Header(), body: parsed}
}

// signup は、サインアップして、結果を返す。
//
// 引数:
//   - testing: テストの状態。
//   - environment: テストの環境。
//   - userName: ユーザー名。
//   - password: パスワード。
//   - ip: クライアントの IP。
//
// 戻り値:
//   - 応答。
func signup(testing *testing.T, environment *testEnvironment, userName string, password string, ip string) response {
	// JSON のボディで、呼び出す。
	return call(testing, environment, http.MethodPost, "/auth/signup", `{"userName":"`+userName+`","password":"`+password+`"}`, "", ip)
}

// login は、ログインして、結果を返す。
//
// 引数:
//   - testing: テストの状態。
//   - environment: テストの環境。
//   - userName: ユーザー名。
//   - password: パスワード。
//   - ip: クライアントの IP。
//
// 戻り値:
//   - 応答。
func login(testing *testing.T, environment *testEnvironment, userName string, password string, ip string) response {
	// JSON のボディで、呼び出す。
	return call(testing, environment, http.MethodPost, "/auth/login", `{"userName":"`+userName+`","password":"`+password+`"}`, "", ip)
}

// goodPassword は、強さの検査を通る、テスト用のパスワード。
const goodPassword = "correct horse battery"

// TestSignupAndLoginFlow は、サインアップ → access token の取得 → 自分の情報 → ログアウト → 無効な refresh token、の一連の流れを確かめる。
func TestSignupAndLoginFlow(testing *testing.T) {
	environment := newTestEnvironment(testing, nil)
	// サインアップすると、refresh token が返る(ログイン状態になる)。
	signedUp := signup(testing, environment, "Alice", goodPassword, "203.0.113.1")
	refreshToken, _ := signedUp.body["token"].(string)
	if signedUp.status != 200 || refreshToken == "" {
		testing.Fatalf("200 と token のはず: %d %v", signedUp.status, signedUp.body)
	}
	// その refresh token から、access token を取れる。refresh token は更新されない(何度でも取れる)。
	first := call(testing, environment, http.MethodPost, "/auth/refresh", "", refreshToken, "203.0.113.1")
	second := call(testing, environment, http.MethodPost, "/auth/refresh", "", refreshToken, "203.0.113.1")
	accessToken, _ := first.body["token"].(string)
	if first.status != 200 || second.status != 200 || accessToken == "" || len(first.body) != 1 {
		testing.Fatalf("access token だけが返るはず: %d %v", first.status, first.body)
	}
	// access token の claims は、仕様どおり(role は user)。
	claims := decodeClaims(testing, accessToken)
	if claims["type"] != "access" || claims["name"] != "Alice" || claims["role"] != "user" || !strings.HasPrefix(claims["sub"].(string), "userid-") {
		testing.Errorf("claims が違う: %v", claims)
	}
	// 自分の情報。
	me := call(testing, environment, http.MethodGet, "/auth/me", "", accessToken, "203.0.113.1")
	if me.status != 200 || me.body["userId"] != claims["sub"] || me.body["userName"] != "Alice" {
		testing.Errorf("自分の情報のはず: %d %v", me.status, me.body)
	}
	// ログインでも、refresh token が返る(別の jti)。
	loggedIn := login(testing, environment, "alice", goodPassword, "203.0.113.1")
	if loggedIn.status != 200 || loggedIn.body["token"] == "" || loggedIn.body["token"] == refreshToken {
		testing.Errorf("別の refresh token が返るはず: %d", loggedIn.status)
	}
	// ログアウトすると、その refresh token では、もう access token を取れない。もう一度のログアウトは 401。
	logout := call(testing, environment, http.MethodPost, "/auth/logout", "", refreshToken, "203.0.113.1")
	afterLogout := call(testing, environment, http.MethodPost, "/auth/refresh", "", refreshToken, "203.0.113.1")
	logoutAgain := call(testing, environment, http.MethodPost, "/auth/logout", "", refreshToken, "203.0.113.1")
	if logout.status != 200 || len(logout.body) != 0 || afterLogout.status != 401 || logoutAgain.status != 401 {
		testing.Errorf("200、401、401 のはず: %d %d %d", logout.status, afterLogout.status, logoutAgain.status)
	}
	// ログアウト前に発行した access token は、有効期限まで使える。別の refresh token は、影響を受けない。
	if stillValid := call(testing, environment, http.MethodGet, "/auth/me", "", accessToken, "203.0.113.1"); stillValid.status != 200 {
		testing.Errorf("access token は、期限まで使えるはず: %d", stillValid.status)
	}
	if other := call(testing, environment, http.MethodPost, "/auth/refresh", "", loggedIn.body["token"].(string), "203.0.113.1"); other.status != 200 {
		testing.Errorf("別の refresh token は使えるはず: %d", other.status)
	}
}

// decodeClaims は、JWT の本体を、JSON として読む(署名は確かめない)。
//
// 引数:
//   - testing: テストの状態。
//   - tokenText: JWT の文字列。
//
// 戻り値:
//   - claims。
func decodeClaims(testing *testing.T, tokenText string) map[string]any {
	// 本体(2つ目の部分)を、base64url から JSON にする。
	decoded, decodeError := base64.RawURLEncoding.DecodeString(strings.Split(tokenText, ".")[1])
	if decodeError != nil {
		testing.Fatal(decodeError)
	}
	claims := map[string]any{}
	_ = json.Unmarshal(decoded, &claims)
	return claims
}

// TestSignupValidation は、サインアップの入力の検査(ユーザー名、パスワード)と、重複(大文字小文字を区別しない)を確かめる。
func TestSignupValidation(testing *testing.T) {
	environment := newTestEnvironment(testing, nil)
	for name, testCase := range map[string]struct{ userName, password string }{
		"ユーザー名が空":       {"", goodPassword},
		"ユーザー名に _":      {"a_b", goodPassword},
		"ユーザー名に空白":      {"a b", goodPassword},
		"ユーザー名が65文字":    {strings.Repeat("a", 65), goodPassword},
		"パスワードが11文字":    {"alice", "abcdefghijk"},
		"よくあるパスワード":     {"alice", "password123456"},
		"ユーザー名を含むパスワード": {"alice", "xx-alice-xx-yy"},
	} {
		if rejected := signup(testing, environment, testCase.userName, testCase.password, "203.0.113.1"); rejected.status != 400 || errorCode(rejected) != "invalid_request" {
			testing.Errorf("%s: 400 のはず: %d %v", name, rejected.status, rejected.body)
		}
	}
	// ユーザー名が64文字なら通る。
	if accepted := signup(testing, environment, strings.Repeat("a", 64), goodPassword, "203.0.113.1"); accepted.status != 200 {
		testing.Errorf("64文字は通るはず: %d", accepted.status)
	}
	// JSON でないボディは 400。
	if broken := call(testing, environment, http.MethodPost, "/auth/signup", "not json", "", "203.0.113.1"); broken.status != 400 {
		testing.Errorf("400 のはず: %d", broken.status)
	}
	// 重複(大文字小文字を区別しない)は 409。
	signup(testing, environment, "Bob", goodPassword, "203.0.113.1")
	if duplicate := signup(testing, environment, "bOB", goodPassword, "203.0.113.1"); duplicate.status != 409 || errorCode(duplicate) != "conflict" {
		testing.Errorf("409 のはず: %d %v", duplicate.status, duplicate.body)
	}
	// パスワードは、DB に平文で入らず、argon2id のハッシュで入る。
	var storedHash string
	environment.database.Raw("SELECT password_hash FROM users WHERE user_name_lower = 'bob'").Scan(&storedHash)
	if !strings.HasPrefix(storedHash, "$argon2id$") || strings.Contains(storedHash, goodPassword) {
		testing.Errorf("ハッシュで保存されるはず: %q", storedHash)
	}
}

// errorCode は、応答のエラーの code を返す。
//
// 引数:
//   - result: 応答。
//
// 戻り値:
//   - code。
func errorCode(result response) string {
	// error.code を取り出す。
	errorObject, _ := result.body["error"].(map[string]any)
	code, _ := errorObject["code"].(string)
	return code
}

// TestLoginFailuresAreIndistinguishable は、存在しない・パスワードの誤り・ロック中・BAN が、全て同じ 401(同じ本文)で、
// 5回の失敗でロックされ(正しいパスワードでも)、時間が過ぎると解け、成功すると回数が戻ることを確かめる。
func TestLoginFailuresAreIndistinguishable(testing *testing.T) {
	environment := newTestEnvironment(testing, nil)
	signup(testing, environment, "alice", goodPassword, "203.0.113.1")
	// 存在しない・誤りは、同じ 401 で、同じメッセージ。
	nothing := login(testing, environment, "nobody", goodPassword, "203.0.113.2")
	wrong := login(testing, environment, "alice", "wrong password!!", "203.0.113.2")
	if nothing.status != 401 || wrong.status != 401 || nothing.body["error"].(map[string]any)["message"] != wrong.body["error"].(map[string]any)["message"] {
		testing.Fatalf("同じ 401 のはず: %d %d %v %v", nothing.status, wrong.status, nothing.body, wrong.body)
	}
	// 失敗が4回(上の誤りが1回、あと3回)のあと、成功すると、回数が戻る。
	for attempt := 0; attempt < 3; attempt++ {
		login(testing, environment, "alice", "wrong password!!", "203.0.113.2")
	}
	if ok := login(testing, environment, "alice", goodPassword, "203.0.113.2"); ok.status != 200 {
		testing.Fatalf("4回の失敗のあとでも、成功するはず: %d", ok.status)
	}
	// 回数が戻ったので、また5回失敗するまでは、ロックされない。5回目でロック。
	for attempt := 0; attempt < 4; attempt++ {
		login(testing, environment, "alice", "wrong password!!", "203.0.113.3")
	}
	if ok := login(testing, environment, "alice", goodPassword, "203.0.113.3"); ok.status != 200 {
		testing.Fatalf("回数が戻っているので、4回の失敗では、ロックされないはず: %d", ok.status)
	}
	for attempt := 0; attempt < 5; attempt++ {
		login(testing, environment, "alice", "wrong password!!", "203.0.113.4")
	}
	locked := login(testing, environment, "alice", goodPassword, "203.0.113.4")
	if locked.status != 401 || locked.body["error"].(map[string]any)["message"] != wrong.body["error"].(map[string]any)["message"] {
		testing.Fatalf("ロック中は、正しいパスワードでも、同じ 401 のはず: %d %v", locked.status, locked.body)
	}
	// ロックは、ユーザー名(大文字小文字を区別しない)ごと。別の IP からでも、ロックされている。
	if byName := login(testing, environment, "ALICE", goodPassword, "198.51.100.7"); byName.status != 401 {
		testing.Errorf("名前ごとのロックのはず: %d", byName.status)
	}
	// ロックの時間が過ぎると、解ける。
	environment.database.Exec("UPDATE login_failures SET locked_until = now() - interval '1 second'")
	if unlocked := login(testing, environment, "alice", goodPassword, "198.51.100.8"); unlocked.status != 200 {
		testing.Errorf("時間が過ぎたら、解けるはず: %d", unlocked.status)
	}
	// 存在しない名前も、同じようにロックされる(存在を推測させない)。
	for attempt := 0; attempt < 5; attempt++ {
		login(testing, environment, "ghost", goodPassword, "198.51.100.9")
	}
	var ghostLocked int64
	environment.database.Table("login_failures").Where("user_name_lower = 'ghost' AND locked_until > now()").Count(&ghostLocked)
	if ghostLocked != 1 {
		testing.Error("存在しない名前も、ロックされるはず")
	}
	// BAN 中は、正しいパスワードでも、同じ 401。失敗には数えない。BAN で、refresh token も使えなくなる。
	refresh := login(testing, environment, "alice", goodPassword, "198.51.100.10").body["token"].(string)
	environment.database.Exec("UPDATE users SET status = 'banned' WHERE user_name_lower = 'alice'")
	banned := login(testing, environment, "alice", goodPassword, "198.51.100.11")
	if banned.status != 401 || banned.body["error"].(map[string]any)["message"] != wrong.body["error"].(map[string]any)["message"] {
		testing.Errorf("BAN 中は、同じ 401 のはず: %d %v", banned.status, banned.body)
	}
	if refreshed := call(testing, environment, http.MethodPost, "/auth/refresh", "", refresh, "198.51.100.11"); refreshed.status != 401 {
		testing.Errorf("BAN されたユーザーの refresh token は、使えないはず: %d", refreshed.status)
	}
	var failureRows int64
	environment.database.Table("login_failures").Where("user_name_lower = 'alice'").Count(&failureRows)
	if failureRows != 0 {
		testing.Error("BAN 中のログインは、失敗に数えないはず")
	}
	// ユーザー名の形が不正なものは、記録せずに、同じ 401。JSON が壊れていれば 400。
	if invalid := login(testing, environment, strings.Repeat("a", 65), goodPassword, "198.51.100.12"); invalid.status != 401 {
		testing.Errorf("不正な名前は、同じ 401 のはず: %d", invalid.status)
	}
	if broken := call(testing, environment, http.MethodPost, "/auth/login", "not json", "", "198.51.100.12"); broken.status != 400 {
		testing.Errorf("400 のはず: %d", broken.status)
	}
}

// TestRateLimits は、IP ごとのレート制限(ログインとサインアップ)が、429 と Retry-After を返し、IP ごとに独立していることを確かめる。
func TestRateLimits(testing *testing.T) {
	environment := newTestEnvironment(testing, func(settings *service.Settings) {
		settings.LoginIPLimit, settings.SignupIPLimit = 3, 2
	})
	// ログイン: 3回までは 401(認証の失敗)、4回目から 429(Retry-After つき)。別の IP は、影響を受けない。
	for attempt := 1; attempt <= 3; attempt++ {
		if attempted := login(testing, environment, "nobody", goodPassword, "203.0.113.1"); attempted.status != 401 {
			testing.Fatalf("%d回目は 401 のはず: %d", attempt, attempted.status)
		}
	}
	limited := login(testing, environment, "nobody", goodPassword, "203.0.113.1")
	if limited.status != 429 || errorCode(limited) != "too_many_requests" || limited.header.Get("Retry-After") == "" {
		testing.Fatalf("429 と Retry-After のはず: %d %v", limited.status, limited.header)
	}
	if other := login(testing, environment, "nobody", goodPassword, "203.0.113.2"); other.status != 401 {
		testing.Errorf("別の IP は 429 にならないはず: %d", other.status)
	}
	// サインアップ: 2回までは通り、3回目から 429。ログインの制限とは、別に数える。
	for attempt, name := range []string{"user-a", "user-b"} {
		if accepted := signup(testing, environment, name, goodPassword, "203.0.113.9"); accepted.status != 200 {
			testing.Fatalf("%d回目は 200 のはず: %d", attempt+1, accepted.status)
		}
	}
	signupLimited := signup(testing, environment, "user-c", goodPassword, "203.0.113.9")
	if signupLimited.status != 429 || signupLimited.header.Get("Retry-After") == "" {
		testing.Errorf("429 のはず: %d", signupLimited.status)
	}
	if loginStillOK := login(testing, environment, "nobody", goodPassword, "203.0.113.9"); loginStillOK.status != 401 {
		testing.Errorf("サインアップの制限は、ログインに影響しないはず: %d", loginStillOK.status)
	}
	// X-Forwarded-For を偽っても、信頼するプロキシの外から来たリクエストでは、接続元の IP で数える。
	for attempt := 0; attempt < 5; attempt++ {
		untrusted := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"userName":"nobody","password":"`+goodPassword+`"}`))
		untrusted.RemoteAddr = "198.51.100.50:1"
		untrusted.Header.Set("Content-Type", "application/json")
		untrusted.Header.Set("X-Forwarded-For", "10.0.0."+string(rune('1'+attempt)))
		recorder := httptest.NewRecorder()
		environment.server.ServeHTTP(recorder, untrusted)
		if attempt == 4 && recorder.Code != 429 {
			testing.Errorf("偽の X-Forwarded-For では、制限を逃れられないはず: %d", recorder.Code)
		}
	}
}

// TestTokenEndpointsRejectBadTokens は、/refresh、/me、/logout が、Authorization が無い・形が違う・種類が違う・期限切れのトークンを、401 にすることを確かめる。
func TestTokenEndpointsRejectBadTokens(testing *testing.T) {
	environment := newTestEnvironment(testing, nil)
	refreshToken := signup(testing, environment, "alice", goodPassword, "203.0.113.1").body["token"].(string)
	accessToken := call(testing, environment, http.MethodPost, "/auth/refresh", "", refreshToken, "203.0.113.1").body["token"].(string)
	for name, testCase := range map[string]struct{ method, path, bearer string }{
		"refresh: トークン無し":           {http.MethodPost, "/auth/refresh", ""},
		"refresh: access token を出す": {http.MethodPost, "/auth/refresh", accessToken},
		"refresh: 壊れたトークン":          {http.MethodPost, "/auth/refresh", "abc.def.ghi"},
		"me: トークン無し":                {http.MethodGet, "/auth/me", ""},
		"me: refresh token を出す":     {http.MethodGet, "/auth/me", refreshToken},
		"logout: トークン無し":            {http.MethodPost, "/auth/logout", ""},
		"logout: access token を出す":  {http.MethodPost, "/auth/logout", accessToken},
	} {
		if rejected := call(testing, environment, testCase.method, testCase.path, "", testCase.bearer, "203.0.113.1"); rejected.status != 401 || errorCode(rejected) != "unauthorized" {
			testing.Errorf("%s: 401 のはず: %d", name, rejected.status)
		}
	}
	// Authorization の形が Bearer でない(Basic)のも、401。
	request := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	request.Header.Set("Authorization", "Basic "+accessToken)
	recorder := httptest.NewRecorder()
	environment.server.ServeHTTP(recorder, request)
	if recorder.Code != 401 {
		testing.Errorf("Basic は 401 のはず: %d", recorder.Code)
	}
	// 期限切れの access token は 401(時計を15分以上進める)。
	*environment.clock = environment.clock.Add(15*time.Minute + time.Second)
	if expired := call(testing, environment, http.MethodGet, "/auth/me", "", accessToken, "203.0.113.1"); expired.status != 401 {
		testing.Errorf("期限切れは 401 のはず: %d", expired.status)
	}
	// refresh token は、まだ使える(90日)。期限切れ(90日後)は 401。
	if fresh := call(testing, environment, http.MethodPost, "/auth/refresh", "", refreshToken, "203.0.113.1"); fresh.status != 200 {
		testing.Errorf("refresh token は、まだ使えるはず: %d", fresh.status)
	}
	*environment.clock = environment.clock.Add(90 * 24 * time.Hour)
	if expired := call(testing, environment, http.MethodPost, "/auth/refresh", "", refreshToken, "203.0.113.1"); expired.status != 401 {
		testing.Errorf("期限切れの refresh token は 401 のはず: %d", expired.status)
	}
}

// TestJWKSAndBackendVerification は、JWKS が仕様どおりの形で、公開鍵で access token を検証できる(backend が行うのと同じ方法)ことを確かめる。
func TestJWKSAndBackendVerification(testing *testing.T) {
	environment := newTestEnvironment(testing, nil)
	jwks := call(testing, environment, http.MethodGet, "/auth/.well-known/jwks.json", "", "", "203.0.113.1")
	keyList, _ := jwks.body["keys"].([]any)
	if jwks.status != 200 || len(keyList) != 1 || jwks.header.Get("Cache-Control") != "public, max-age=300" {
		testing.Fatalf("200 と1つの鍵、キャッシュのヘッダのはず: %d %v", jwks.status, jwks.header)
	}
	jsonWebKey := keyList[0].(map[string]any)
	if jsonWebKey["kty"] != "OKP" || jsonWebKey["crv"] != "Ed25519" || jsonWebKey["alg"] != "EdDSA" || jsonWebKey["use"] != "sig" || jsonWebKey["kid"] != environment.keyStore.ActiveKeyID() {
		testing.Errorf("JWK の形が違う: %v", jsonWebKey)
	}
	// 公開鍵だけで(秘密鍵なしで)、access token を検証できる。
	publicKey, _ := base64.RawURLEncoding.DecodeString(jsonWebKey["x"].(string))
	refreshToken := signup(testing, environment, "alice", goodPassword, "203.0.113.1").body["token"].(string)
	accessToken := call(testing, environment, http.MethodPost, "/auth/refresh", "", refreshToken, "203.0.113.1").body["token"].(string)
	parts := strings.Split(accessToken, ".")
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	if len(publicKey) != 32 || !verifyEd25519(publicKey, []byte(parts[0]+"."+parts[1]), signature) {
		testing.Error("JWKS の公開鍵で、署名を検証できるはず")
	}
	// 公開エンドポイント: JWKS は認証不要。ヘルスチェックも。
	if health := call(testing, environment, http.MethodGet, "/healthz", "", "", "203.0.113.1"); health.status != 200 {
		testing.Errorf("200 のはず: %d", health.status)
	}
}

// TestInviteOnlySignup は、招待制のとき、招待コードが要り、使えるのは1回だけで、使えない招待では、ユーザーが作られないことを確かめる。
func TestInviteOnlySignup(testing *testing.T) {
	environment := newTestEnvironment(testing, func(settings *service.Settings) { settings.SignupMode = service.SignupModeInvite })
	environment.database.Exec("INSERT INTO invitations (code_hash, expires_at) VALUES (?, now() + interval '1 day')", service.HashInvitationCode("secret-code"))
	// 招待コード無し、違うコードは 400。
	if noCode := signup(testing, environment, "alice", goodPassword, "203.0.113.1"); noCode.status != 400 {
		testing.Errorf("招待コード無しは 400 のはず: %d", noCode.status)
	}
	if wrongCode := call(testing, environment, http.MethodPost, "/auth/signup", `{"userName":"alice","password":"`+goodPassword+`","invitationCode":"wrong"}`, "", "203.0.113.1"); wrongCode.status != 400 {
		testing.Errorf("違うコードは 400 のはず: %d", wrongCode.status)
	}
	// 正しいコードで、サインアップできる(前後の空白は、無視する)。同じコードは、もう使えない。
	accepted := call(testing, environment, http.MethodPost, "/auth/signup", `{"userName":"alice","password":"`+goodPassword+`","invitationCode":" secret-code "}`, "", "203.0.113.1")
	reused := call(testing, environment, http.MethodPost, "/auth/signup", `{"userName":"bob","password":"`+goodPassword+`","invitationCode":"secret-code"}`, "", "203.0.113.1")
	if accepted.status != 200 || reused.status != 400 {
		testing.Errorf("200 と 400 のはず: %d %d", accepted.status, reused.status)
	}
	var userCount int64
	environment.database.Table("users").Count(&userCount)
	if userCount != 1 {
		testing.Errorf("ユーザーは1人のはず: %d", userCount)
	}
}

// TestPasswordRehashOnLogin は、ハッシュのパラメータが古いユーザーが、ログインに成功したとき、ハッシュが新しいパラメータで作り直されることを確かめる。
func TestPasswordRehashOnLogin(testing *testing.T) {
	environment := newTestEnvironment(testing, nil)
	signup(testing, environment, "alice", goodPassword, "203.0.113.1")
	// 古い(弱い)パラメータのハッシュに、差し替える。
	oldHash, _ := password.NewHasher(password.Parameters{MemoryKiB: 8, Iterations: 2, Parallelism: 1}).Hash(goodPassword)
	environment.database.Exec("UPDATE users SET password_hash = ? WHERE user_name_lower = 'alice'", oldHash)
	// ログインすると、今のパラメータ(t=1)のハッシュになる。
	if ok := login(testing, environment, "alice", goodPassword, "203.0.113.1"); ok.status != 200 {
		testing.Fatalf("ログインできるはず: %d", ok.status)
	}
	var newHash string
	environment.database.Raw("SELECT password_hash FROM users WHERE user_name_lower = 'alice'").Scan(&newHash)
	if !strings.Contains(newHash, "m=8,t=1,p=1") {
		testing.Errorf("ハッシュが作り直されるはず: %q", newHash)
	}
	if again := login(testing, environment, "alice", goodPassword, "203.0.113.1"); again.status != 200 {
		testing.Errorf("作り直したあとも、ログインできるはず: %d", again.status)
	}
}

// verifyEd25519 は、Ed25519 の署名を、公開鍵で検証する。
//
// 引数:
//   - publicKey: 公開鍵(32バイト)。
//   - message: 署名された内容。
//   - signature: 署名。
//
// 戻り値:
//   - 正しければ true。
func verifyEd25519(publicKey []byte, message []byte, signature []byte) bool {
	// 標準ライブラリで検証する。
	return ed25519.Verify(ed25519.PublicKey(publicKey), message, signature)
}

// TestLoginHistory は、サインアップとログインの成功・失敗が履歴に残り(失敗の理由つき)、本人にだけ、新しい順に見え、
// 他のユーザーや存在しない名前の試行が混ざらず、レート制限に当たった試行は残らず、長い User-Agent が切り詰められることを確かめる。
func TestLoginHistory(testing *testing.T) {
	environment := newTestEnvironment(testing, func(settings *service.Settings) { settings.LoginMaxFailures = 2; settings.LoginIPLimit = 8 })
	refreshToken := signup(testing, environment, "alice", goodPassword, "203.0.113.1").body["token"].(string)
	signup(testing, environment, "bob", goodPassword, "203.0.113.2")
	// alice への試行: 誤り、成功、(2回目の誤りでロック)誤り、誤り、ロック中の正しいパスワード。
	login(testing, environment, "alice", "wrong password!!", "203.0.113.3")
	login(testing, environment, "alice", goodPassword, "203.0.113.4")
	login(testing, environment, "alice", "wrong password!!", "203.0.113.5")
	login(testing, environment, "alice", "wrong password!!", "203.0.113.5")
	login(testing, environment, "alice", goodPassword, "203.0.113.6")
	// 存在しない名前への試行と、bob への試行は、alice の履歴に混ざらない。
	login(testing, environment, "ghost", goodPassword, "203.0.113.7")
	login(testing, environment, "bob", "wrong password!!", "203.0.113.7")
	accessToken := call(testing, environment, http.MethodPost, "/auth/refresh", "", refreshToken, "203.0.113.1").body["token"].(string)
	history := call(testing, environment, http.MethodGet, "/auth/login-history", "", accessToken, "203.0.113.1")
	events, _ := history.body["events"].([]any)
	if history.status != 200 || len(events) != 6 {
		testing.Fatalf("alice の履歴は6件のはず(サインアップ1、ログイン5): %d %d", history.status, len(events))
	}
	// 新しい順: ロック中、誤り、誤り、成功、誤り、サインアップ。
	expected := []struct{ kind, result, reason string }{
		{"login", "failure", "locked"}, {"login", "failure", "wrong_password"}, {"login", "failure", "wrong_password"},
		{"login", "success", ""}, {"login", "failure", "wrong_password"}, {"signup", "success", ""},
	}
	for index, want := range expected {
		event := events[index].(map[string]any)
		reason, _ := event["reason"].(string)
		if event["kind"] != want.kind || event["result"] != want.result || reason != want.reason {
			testing.Errorf("%d件目が違う: %v(期待 %+v)", index, event, want)
		}
	}
	first := events[0].(map[string]any)
	if first["ip"] != "203.0.113.6" || first["userAgent"] != "test-agent/1.0" || !strings.HasSuffix(first["at"].(string), "+09:00") {
		testing.Errorf("ip、User-Agent、JST の日時が入るはず: %v", first)
	}
	// 成功のときの reason は null。limit で件数を絞れる。範囲外は 400。
	if events[3].(map[string]any)["reason"] != nil {
		testing.Error("成功の reason は null のはず")
	}
	limited := call(testing, environment, http.MethodGet, "/auth/login-history?limit=2", "", accessToken, "203.0.113.1")
	if limitedEvents, _ := limited.body["events"].([]any); len(limitedEvents) != 2 {
		testing.Errorf("2件のはず: %v", limited.body)
	}
	if bad := call(testing, environment, http.MethodGet, "/auth/login-history?limit=0", "", accessToken, "203.0.113.1"); bad.status != 400 {
		testing.Errorf("limit が範囲外なら 400 のはず: %d", bad.status)
	}
	// bob は、自分の履歴だけが見える。
	bobAccess := call(testing, environment, http.MethodPost, "/auth/refresh", "", login(testing, environment, "bob", goodPassword, "203.0.113.8").body["token"].(string), "203.0.113.8").body["token"].(string)
	bobEvents, _ := call(testing, environment, http.MethodGet, "/auth/login-history", "", bobAccess, "203.0.113.8").body["events"].([]any)
	if len(bobEvents) != 3 {
		testing.Errorf("bob の履歴は3件のはず(サインアップ、誤り、成功): %d", len(bobEvents))
	}
	// トークンが無い、refresh token は 401。
	if noToken := call(testing, environment, http.MethodGet, "/auth/login-history", "", "", "203.0.113.1"); noToken.status != 401 {
		testing.Errorf("401 のはず: %d", noToken.status)
	}
	if wrongType := call(testing, environment, http.MethodGet, "/auth/login-history", "", refreshToken, "203.0.113.1"); wrongType.status != 401 {
		testing.Errorf("refresh token は 401 のはず: %d", wrongType.status)
	}
	// BAN 中の試行は、banned の理由で残る。
	environment.database.Exec("UPDATE users SET status = 'banned' WHERE user_name_lower = 'bob'")
	login(testing, environment, "bob", goodPassword, "203.0.113.9")
	var bannedCount int64
	environment.database.Table("login_events").Where("user_name_lower = 'bob' AND reason = 'banned'").Count(&bannedCount)
	if bannedCount != 1 {
		testing.Errorf("banned が1件残るはず: %d", bannedCount)
	}
	// 存在しない名前への試行は、user_id なしで残る。
	var ghostCount int64
	environment.database.Table("login_events").Where("user_name_lower = 'ghost' AND user_id IS NULL AND reason = 'unknown_user'").Count(&ghostCount)
	if ghostCount != 1 {
		testing.Errorf("user_id なしで、1件残るはず: %d", ghostCount)
	}
}

// TestLoginHistorySkipsRateLimitedAndTruncates は、レート制限に当たった試行は履歴に残さないことと、長い User-Agent を切り詰めることを確かめる。
func TestLoginHistorySkipsRateLimitedAndTruncates(testing *testing.T) {
	environment := newTestEnvironment(testing, func(settings *service.Settings) { settings.LoginIPLimit = 2 })
	signup(testing, environment, "alice", goodPassword, "203.0.113.1")
	// 長い User-Agent で、3回ログインする(3回目は 429)。
	for attempt := 0; attempt < 3; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"userName":"alice","password":"`+goodPassword+`"}`))
		request.RemoteAddr = "127.0.0.1:1"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-For", "203.0.113.9")
		request.Header.Set("User-Agent", strings.Repeat("x", 1000))
		environment.server.ServeHTTP(httptest.NewRecorder(), request)
	}
	var count int64
	var longest int
	environment.database.Table("login_events").Where("kind = 'login'").Count(&count)
	environment.database.Raw("SELECT max(length(user_agent)) FROM login_events").Scan(&longest)
	if count != 2 || longest != 256 {
		testing.Errorf("レート制限に当たった1件は残らず、User-Agent は256バイトのはず: %d %d", count, longest)
	}
}
