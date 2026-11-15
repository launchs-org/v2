package jwtverify

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fakeAuth は、テスト用の、JWKS を配信する auth の代わり。鍵の入れ替えや、障害を再現できる。
type fakeAuth struct {
	mutex      sync.Mutex
	keys       map[string]ed25519.PrivateKey
	failing    bool
	fetchCount int
	server     *httptest.Server
}

// newFakeAuth は、鍵を1つ持った fakeAuth を作る(テストの終わりに、サーバーを止める)。
//
// 引数:
//   - testing: テストの状態。
//
// 戻り値:
//   - 作成した fakeAuth。最初の鍵の kid は "key-1"。
func newFakeAuth(testing *testing.T) *fakeAuth {
	// 鍵を1つ作って、JWKS を返すサーバーを立てる。
	auth := &fakeAuth{keys: map[string]ed25519.PrivateKey{}}
	auth.addKey("key-1")
	auth.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// 取得の回数を数えて、障害のときは 500 を返す。
		auth.mutex.Lock()
		defer auth.mutex.Unlock()
		auth.fetchCount++
		if auth.failing {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		// 持っている鍵を、JWKS の形で返す。
		keyList := []map[string]string{}
		for keyID, privateKey := range auth.keys {
			publicKey, _ := privateKey.Public().(ed25519.PublicKey)
			keyList = append(keyList, map[string]string{"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "use": "sig", "kid": keyID, "x": base64.RawURLEncoding.EncodeToString(publicKey)})
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"keys": keyList})
	}))
	testing.Cleanup(auth.server.Close)
	return auth
}

// addKey は、鍵を1つ足す。
//
// 引数:
//   - keyID: 鍵の kid。
func (auth *fakeAuth) addKey(keyID string) {
	// 新しい鍵を作って、持たせる。
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	auth.keys[keyID] = privateKey
}

// removeKey は、鍵を1つ外す。
//
// 引数:
//   - keyID: 外す鍵の kid。
func (auth *fakeAuth) removeKey(keyID string) {
	// 持っている鍵から外す。
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	delete(auth.keys, keyID)
}

// setFailing は、障害(500)の状態を切り替える。
//
// 引数:
//   - failing: 障害にするか。
func (auth *fakeAuth) setFailing(failing bool) {
	// 状態を切り替える。
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	auth.failing = failing
}

// fetches は、これまでの取得の回数を返す。
//
// 戻り値:
//   - 取得の回数。
func (auth *fakeAuth) fetches() int {
	// 回数を読む。
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	return auth.fetchCount
}

// sign は、指定の鍵で、claims に署名したトークンを作る。
//
// 引数:
//   - keyID: 署名に使う鍵の kid(ヘッダに入れる)。
//   - claims: トークンの中身。
//
// 戻り値:
//   - トークンの文字列。
func (auth *fakeAuth) sign(keyID string, claims Claims) string {
	// 持っている鍵で署名する。
	auth.mutex.Lock()
	privateKey := auth.keys[keyID]
	auth.mutex.Unlock()
	unsigned := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	unsigned.Header["kid"] = keyID
	signed, _ := unsigned.SignedString(privateKey)
	return signed
}

// testClock は、テストで動かせる時計。
type testClock struct {
	mutex sync.Mutex
	value time.Time
}

// now は、現在の時刻を返す。
//
// 戻り値:
//   - 時計の時刻。
func (clock *testClock) now() time.Time {
	// 時刻を読む。
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.value
}

// advance は、時計を進める。
//
// 引数:
//   - duration: 進める時間。
func (clock *testClock) advance(duration time.Duration) {
	// 時刻を進める。
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.value = clock.value.Add(duration)
}

// validClaims は、有効な access token の中身を作る。
//
// 引数:
//   - now: 発行の時刻。
//
// 戻り値:
//   - 15分有効な access token の中身。
func validClaims(now time.Time) Claims {
	// 通常の access token と同じ項目を入れる。
	return Claims{
		RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Subject: "userid-alice", ID: "tokenid-1", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute))},
		Name:             "alice", Role: "user", Type: TypeAccess,
	}
}

// newTestVerifier は、fakeAuth と時計を使う Verifier を作る(取得の間隔は、本番と同じ既定値)。
//
// 引数:
//   - auth: JWKS を配信する fakeAuth。
//   - clock: 時計。
//
// 戻り値:
//   - 作成した Verifier。
func newTestVerifier(auth *fakeAuth, clock *testClock) *Verifier {
	// 時計だけを差し替えて、作る。
	return New(Options{URL: auth.server.URL, Now: clock.now})
}

// TestVerifyAcceptsValidAccessToken は、正しい access token を、中身つきで受け入れることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestVerifyAcceptsValidAccessToken(testing *testing.T) {
	// 起動時の取得を済ませた Verifier を用意する。
	auth, clock := newFakeAuth(testing), &testClock{value: time.Now()}
	verifier := newTestVerifier(auth, clock)
	if refreshError := verifier.Refresh(context.Background()); refreshError != nil {
		testing.Fatal(refreshError)
	}
	// 正しいトークンは通り、中身が取り出せる。
	claims, verifyError := verifier.Verify(context.Background(), auth.sign("key-1", validClaims(clock.now())))
	if verifyError != nil || claims.Subject != "userid-alice" || claims.Name != "alice" || claims.Role != "user" {
		testing.Fatalf("通るはず: %+v %v", claims, verifyError)
	}
	// 検証のたびには、取得しない(起動時の1回だけ)。
	if auth.fetches() != 1 {
		testing.Errorf("取得は1回のはず: %d", auth.fetches())
	}
}

// TestVerifyRejectsBadTokens は、偽造・期限切れ・別の発行者・別の種類・必須項目の欠けを、全て同じエラーで拒否することを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestVerifyRejectsBadTokens(testing *testing.T) {
	// 起動時の取得を済ませた Verifier を用意する。
	auth, clock := newFakeAuth(testing), &testClock{value: time.Now()}
	verifier := newTestVerifier(auth, clock)
	_ = verifier.Refresh(context.Background())
	// 変えた項目ごとに、トークンを作る。
	with := func(change func(*Claims)) string {
		claims := validClaims(clock.now())
		change(&claims)
		return auth.sign("key-1", claims)
	}
	publicKey, _ := auth.keys["key-1"].Public().(ed25519.PublicKey)
	hmacForged, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims(clock.now())).SignedString([]byte(publicKey))
	noneForged, _ := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims(clock.now())).SignedString(jwt.UnsafeAllowNoneSignatureType)
	otherAuth := newFakeAuth(testing)
	otherAuth.addKey("key-unknown")
	badTokens := map[string]string{
		"期限切れ":           with(func(claims *Claims) { claims.ExpiresAt = jwt.NewNumericDate(clock.now().Add(-time.Minute)) }),
		"exp が無い":        with(func(claims *Claims) { claims.ExpiresAt = nil }),
		"別の発行者":          with(func(claims *Claims) { claims.Issuer = "someone-else" }),
		"refresh token":  with(func(claims *Claims) { claims.Type = "refresh" }),
		"sub が無い":        with(func(claims *Claims) { claims.Subject = "" }),
		"jti が無い":        with(func(claims *Claims) { claims.ID = "" }),
		"別の鍵で署名(同じ kid)": otherAuth.sign("key-1", validClaims(clock.now())),
		"知らない kid":       otherAuth.sign("key-unknown", validClaims(clock.now())),
		"署名の改ざん":         auth.sign("key-1", validClaims(clock.now())) + "x",
		"HS256 で、公開鍵を秘密にして偽造": hmacForged,
		"alg=none": noneForged,
		"空":        "",
		"ゴミ":       "not-a-jwt",
	}
	// どれも、同じエラーで拒否される。
	for name, badToken := range badTokens {
		if _, verifyError := verifier.Verify(context.Background(), badToken); verifyError != ErrInvalidToken {
			testing.Errorf("%s は ErrInvalidToken のはず: %v", name, verifyError)
		}
	}
}

// TestVerifyRefetchesForUnknownKidWithLimit は、知らない kid のとき取得し直して鍵の入れ替えに追従すること、
// ただし偽の kid を何度送られても、取得は、間隔の制限(1分)を超えて増えないことを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestVerifyRefetchesForUnknownKidWithLimit(testing *testing.T) {
	// 起動時の取得を済ませる(取得1回目)。
	auth, clock := newFakeAuth(testing), &testClock{value: time.Now()}
	verifier := newTestVerifier(auth, clock)
	_ = verifier.Refresh(context.Background())
	// auth が、鍵を入れ替えた(新しい鍵を足した)。
	auth.addKey("key-2")
	newToken := auth.sign("key-2", validClaims(clock.now()))
	// 起動の直後(制限の範囲内)は、取得し直さないので、まだ検証できない。
	if _, verifyError := verifier.Verify(context.Background(), newToken); verifyError == nil || auth.fetches() != 1 {
		testing.Fatalf("制限の範囲内では、取得しないはず: %v %d", verifyError, auth.fetches())
	}
	// 偽の kid を大量に送っても、取得は増えない。
	for attempt := 0; attempt < 50; attempt++ {
		_, _ = verifier.Verify(context.Background(), jwt.New(jwt.SigningMethodEdDSA).Raw)
	}
	if auth.fetches() != 1 {
		testing.Fatalf("取得は増えないはず: %d", auth.fetches())
	}
	// 1分たつと、知らない kid をきっかけに取得し直して、新しい鍵で検証できる。
	clock.advance(61 * time.Second)
	if _, verifyError := verifier.Verify(context.Background(), newToken); verifyError != nil || auth.fetches() != 2 {
		testing.Fatalf("取得し直して、通るはず: %v %d", verifyError, auth.fetches())
	}
}

// TestVerifyDropsRemovedKeysWhenStale は、キャッシュが古くなったら取得し直し、auth が外した鍵を信用しなくなることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestVerifyDropsRemovedKeysWhenStale(testing *testing.T) {
	// 2つの鍵を持つ状態で、取得する。
	auth, clock := newFakeAuth(testing), &testClock{value: time.Now()}
	auth.addKey("key-2")
	verifier := newTestVerifier(auth, clock)
	_ = verifier.Refresh(context.Background())
	// 1時間有効なトークンを、key-1 で作る(外された後も、期限は切れていない)。
	longClaims := validClaims(clock.now())
	longClaims.ExpiresAt = jwt.NewNumericDate(clock.now().Add(time.Hour))
	oldToken := auth.sign("key-1", longClaims)
	if _, verifyError := verifier.Verify(context.Background(), oldToken); verifyError != nil {
		testing.Fatalf("外される前は、通るはず: %v", verifyError)
	}
	// auth が、古い鍵を外した。時間がたって、キャッシュが古くなる(10分を超える)。
	auth.removeKey("key-1")
	clock.advance(11 * time.Minute)
	// 取得し直されて、外された鍵で署名したトークンは、期限内でも、通らなくなる。
	if _, verifyError := verifier.Verify(context.Background(), oldToken); verifyError == nil {
		testing.Error("外された鍵のトークンは、通らないはず")
	}
	if _, stillThere := verifier.lookup("key-1"); stillThere {
		testing.Error("外された鍵は、キャッシュから消えるはず")
	}
	if _, kept := verifier.lookup("key-2"); !kept {
		testing.Error("残っている鍵は、使えるはず")
	}
}

// TestVerifyKeepsWorkingWhenAuthIsDown は、auth に届かなくなっても、キャッシュの鍵で検証を続けることと、
// 起動時に取得できなかったときは、auth が戻ったあと、短い間隔で取得し直して、使えるようになることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestVerifyKeepsWorkingWhenAuthIsDown(testing *testing.T) {
	// 取得した後で、auth が落ちる。キャッシュが古くなっても、取得に失敗するだけで、検証は続く。
	auth, clock := newFakeAuth(testing), &testClock{value: time.Now()}
	verifier := newTestVerifier(auth, clock)
	_ = verifier.Refresh(context.Background())
	auth.setFailing(true)
	clock.advance(30 * time.Minute)
	if _, verifyError := verifier.Verify(context.Background(), auth.sign("key-1", validClaims(clock.now()))); verifyError != nil {
		testing.Errorf("キャッシュの鍵で、検証できるはず: %v", verifyError)
	}
	// 起動時に auth が落ちていた場合: 取得できず、最初は何も通らない。
	downAuth := newFakeAuth(testing)
	downAuth.setFailing(true)
	freshVerifier := newTestVerifier(downAuth, clock)
	if refreshError := freshVerifier.Refresh(context.Background()); refreshError == nil {
		testing.Fatal("取得できないはず")
	}
	token := downAuth.sign("key-1", validClaims(clock.now()))
	if _, verifyError := freshVerifier.Verify(context.Background(), token); verifyError == nil {
		testing.Error("鍵が無いので、通らないはず")
	}
	// auth が戻って、5秒たてば、次のリクエストで取得し直して、使えるようになる(1分は待たない)。
	downAuth.setFailing(false)
	clock.advance(6 * time.Second)
	if _, verifyError := freshVerifier.Verify(context.Background(), token); verifyError != nil {
		testing.Errorf("戻った後は、通るはず: %v", verifyError)
	}
}

// TestFetchKeysIgnoresUnusableKeys は、JWKS に、使えない鍵(別の種類、署名用でない、壊れた x)が混じっていても無視し、使える鍵が無ければエラーにすることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestFetchKeysIgnoresUnusableKeys(testing *testing.T) {
	// 使えない鍵だけを返すサーバーを立てる。
	body := `{"keys":[{"kty":"RSA","kid":"a"},{"kty":"OKP","crv":"Ed25519","kid":"b","x":"!!!"},{"kty":"OKP","crv":"Ed25519","kid":"c","use":"enc","x":"AAAA"},{"kty":"OKP","crv":"Ed25519","x":"AAAA"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { _, _ = writer.Write([]byte(body)) }))
	defer server.Close()
	// 使える鍵が1つも無いので、エラーになる。
	if _, fetchError := fetchKeys(context.Background(), server.Client(), server.URL); fetchError == nil {
		testing.Error("使える鍵が無いので、エラーのはず")
	}
}
