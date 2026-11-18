// Package jwtverify は、認証コンテナ(auth)が発行した access token を、公開鍵(JWKS)で検証する。
//
// backend など、auth 以外のコンテナが使う。署名鍵(秘密鍵)は持たず、auth の JWKS のエンドポイントから、公開鍵だけを取得して、キャッシュする。
package jwtverify

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// Issuer は、トークンの発行者(iss)。auth の token.Issuer と同じ値にする。
	Issuer = "launchs-org"

	// TypeAccess は、access token の種類(type)。auth の token.TypeAccess と同じ値にする。
	TypeAccess = "access"

	// maxJWKSBytes は、JWKS のレスポンスの大きさの上限。異常に大きなレスポンスで、メモリを使わせないための上限。
	maxJWKSBytes = 1 << 20

	// ed25519KeyLength は、Ed25519 の公開鍵の長さ(バイト)。
	ed25519KeyLength = 32
)

// ErrInvalidToken は、トークンが使えないときのエラー(署名・期限・発行者・種類などの、どの問題でも同じ)。
var ErrInvalidToken = errors.New("トークンが正しくありません")

// Claims は、access token の中身。
type Claims struct {
	jwt.RegisteredClaims

	// Name は、ユーザー名。
	Name string `json:"name"`

	// Role は、役割(user か admin)。
	Role string `json:"role,omitempty"`

	// Type は、トークンの種類(access)。
	Type string `json:"type"`
}

// Options は、Verifier を作るときの設定。
type Options struct {
	// URL は、JWKS のエンドポイント(例: http://auth:8080/auth/.well-known/jwks.json)。
	URL string

	// Logger は、取得の失敗などを記録するロガー(nil なら記録しない)。
	Logger *slog.Logger

	// HTTPClient は、JWKS の取得に使うクライアント(nil なら、タイムアウト5秒の既定のもの)。
	HTTPClient *http.Client

	// Now は、現在の時刻を返す関数(nil なら time.Now)。テストで、時刻を動かすために差し替える。
	Now func() time.Time

	// MinRefetchInterval は、JWKS の取得を試みる間隔の下限(0 なら1分)。偽の kid で、auth を攻撃させないための制限。
	MinRefetchInterval time.Duration

	// EmptyRetryInterval は、まだ1度も取得できていないときの、取得を試みる間隔の下限(0 なら5秒)。auth が遅れて起動したときに、すぐ使えるようにする。
	EmptyRetryInterval time.Duration

	// StaleAfter は、キャッシュを、取得し直すべきとみなす時間(0 なら10分)。入れ替えで外された鍵を、いつまでも信用しないための時間。
	StaleAfter time.Duration
}

// Verifier は、JWKS の公開鍵で、access token を検証する。複数の goroutine から、同時に使える。
type Verifier struct {
	options Options

	// fetchMutex は、取得を1つずつにする(同時に何度も取得しない)。
	fetchMutex sync.Mutex

	// stateMutex は、下の3つの値を守る。
	stateMutex sync.RWMutex

	// keys は、kid ごとの公開鍵のキャッシュ。
	keys map[string]ed25519.PublicKey

	// fetchedAt は、最後に取得に成功した時刻(まだなら、ゼロ値)。
	fetchedAt time.Time

	// lastAttempt は、最後に取得を試みた時刻(成否を問わない)。
	lastAttempt time.Time
}

// New は、Verifier を作る(ここでは、取得しない)。
//
// 引数:
//   - options: 設定。URL は必須。
//
// 戻り値:
//   - 作成した Verifier。
func New(options Options) *Verifier {
	// 省略された設定に、既定値を入れる。
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MinRefetchInterval == 0 {
		options.MinRefetchInterval = time.Minute
	}
	if options.EmptyRetryInterval == 0 {
		options.EmptyRetryInterval = 5 * time.Second
	}
	if options.StaleAfter == 0 {
		options.StaleAfter = 10 * time.Minute
	}
	// 空のキャッシュで、返す。
	return &Verifier{options: options, keys: map[string]ed25519.PublicKey{}}
}

// Refresh は、JWKS を取得して、キャッシュを置き換える(起動時に呼ぶ)。
//
// 取得に失敗しても、いまのキャッシュは、そのまま使い続ける。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - 取得や解釈に失敗したときのエラー。
func (verifier *Verifier) Refresh(requestContext context.Context) error {
	// 取得を1つずつにする。
	verifier.fetchMutex.Lock()
	defer verifier.fetchMutex.Unlock()
	// 試みた時刻を記録して、取得する。
	return verifier.fetchLocked(requestContext)
}

// fetchLocked は、JWKS を取得して、キャッシュを置き換える。fetchMutex を持ったまま呼ぶこと。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - 失敗したときのエラー(このとき、キャッシュは変えない)。
func (verifier *Verifier) fetchLocked(requestContext context.Context) error {
	// 試みた時刻を、先に記録する(失敗しても、間隔の制限が効くように)。
	verifier.stateMutex.Lock()
	verifier.lastAttempt = verifier.options.Now()
	verifier.stateMutex.Unlock()
	// JWKS を取得して、公開鍵に変換する。
	fetchedKeys, fetchError := fetchKeys(requestContext, verifier.options.HTTPClient, verifier.options.URL)
	if fetchError != nil {
		return fetchError
	}
	// 成功したので、キャッシュを丸ごと置き換える(外された鍵は、ここで消える)。
	verifier.stateMutex.Lock()
	verifier.keys = fetchedKeys
	verifier.fetchedAt = verifier.options.Now()
	verifier.stateMutex.Unlock()
	return nil
}

// jsonWebKey は、JWKS の鍵1つ。
type jsonWebKey struct {
	KeyType   string `json:"kty"`
	Curve     string `json:"crv"`
	Algorithm string `json:"alg"`
	Use       string `json:"use"`
	KeyID     string `json:"kid"`
	PublicKey string `json:"x"`
}

// fetchKeys は、JWKS を取得して、kid ごとの公開鍵にする。
//
// 使える鍵(OKP / Ed25519 / EdDSA / 署名用)だけを取り出す。使える鍵が1つも無いときは、エラー。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - httpClient: 取得に使うクライアント。
//   - url: JWKS のエンドポイント。
//
// 戻り値:
//   - kid ごとの公開鍵。
func fetchKeys(requestContext context.Context, httpClient *http.Client, url string) (map[string]ed25519.PublicKey, error) {
	// リクエストを作って、送る。
	request, requestError := http.NewRequestWithContext(requestContext, http.MethodGet, url, nil)
	if requestError != nil {
		return nil, fmt.Errorf("JWKS のリクエストを作れませんでした: %w", requestError)
	}
	response, doError := httpClient.Do(request)
	if doError != nil {
		return nil, fmt.Errorf("JWKS を取得できませんでした: %w", doError)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS の取得が %d を返しました", response.StatusCode)
	}
	// 大きさに上限を付けて、読み込む。
	var keySet struct {
		Keys []jsonWebKey `json:"keys"`
	}
	if decodeError := json.NewDecoder(io.LimitReader(response.Body, maxJWKSBytes)).Decode(&keySet); decodeError != nil {
		return nil, fmt.Errorf("JWKS を読み取れませんでした: %w", decodeError)
	}
	// 使える鍵だけを取り出す。
	publicKeys := map[string]ed25519.PublicKey{}
	for _, key := range keySet.Keys {
		// 種類が違う鍵、署名用でない鍵、kid が無い鍵は、使わない。
		if key.KeyType != "OKP" || key.Curve != "Ed25519" || (key.Algorithm != "" && key.Algorithm != "EdDSA") || (key.Use != "" && key.Use != "sig") || key.KeyID == "" {
			continue
		}
		decoded, decodeError := base64.RawURLEncoding.DecodeString(key.PublicKey)
		if decodeError != nil || len(decoded) != ed25519KeyLength {
			continue
		}
		publicKeys[key.KeyID] = ed25519.PublicKey(decoded)
	}
	if len(publicKeys) == 0 {
		return nil, errors.New("JWKS に、使える鍵がありません")
	}
	return publicKeys, nil
}

// lookup は、キャッシュから、kid の公開鍵を探す。
//
// 引数:
//   - keyID: トークンのヘッダの kid。
//
// 戻り値:
//   - 公開鍵と、あったか。
func (verifier *Verifier) lookup(keyID string) (ed25519.PublicKey, bool) {
	// 読み取りだけなので、共有のロックで読む。
	verifier.stateMutex.RLock()
	defer verifier.stateMutex.RUnlock()
	publicKey, exists := verifier.keys[keyID]
	return publicKey, exists
}

// refreshIfAllowed は、間隔の制限の範囲内なら、JWKS を取得し直す。
//
// 制限: 取得できているときは MinRefetchInterval、まだ1度も取得できていないときは EmptyRetryInterval より前には、試みない。
// 失敗は、ログに残すだけで、呼び出し元には返さない(キャッシュの鍵で、検証を続ける)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
func (verifier *Verifier) refreshIfAllowed(requestContext context.Context) {
	// 取得を1つずつにする(待っている間に、他のリクエストが取得し終えていれば、下の判定で、何もしない)。
	verifier.fetchMutex.Lock()
	defer verifier.fetchMutex.Unlock()
	// 取得できているかと、最後に試みた時刻を読む。
	verifier.stateMutex.RLock()
	hasKeys := !verifier.fetchedAt.IsZero()
	lastAttempt := verifier.lastAttempt
	verifier.stateMutex.RUnlock()
	// 間隔の制限の範囲内なら、試みない。
	interval := verifier.options.MinRefetchInterval
	if !hasKeys {
		interval = verifier.options.EmptyRetryInterval
	}
	if !lastAttempt.IsZero() && verifier.options.Now().Sub(lastAttempt) < interval {
		return
	}
	// 取得する(失敗は、ログだけ)。
	if fetchError := verifier.fetchLocked(requestContext); fetchError != nil && verifier.options.Logger != nil {
		verifier.options.Logger.Warn("JWKS を更新できなかった(キャッシュの鍵で、検証を続ける)", "error", fetchError)
	}
}

// isStale は、キャッシュが、取得し直すべきほど古いかを返す。
//
// 戻り値:
//   - 古ければ true(1度も取得できていないときも true)。
func (verifier *Verifier) isStale() bool {
	// 最後に成功した時刻から、StaleAfter を過ぎているか。
	verifier.stateMutex.RLock()
	defer verifier.stateMutex.RUnlock()
	return verifier.fetchedAt.IsZero() || verifier.options.Now().Sub(verifier.fetchedAt) > verifier.options.StaleAfter
}

// Verify は、access token を検証して、中身を返す。
//
// 確かめること: 署名(ヘッダの kid の公開鍵)、alg が EdDSA、iss、exp が未来、type が access、sub と jti がある。
// 知らない kid のときは、JWKS を取得し直す(間隔の制限つき)。キャッシュが古いときも、取得し直す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - tokenText: access token の文字列。
//
// 戻り値:
//   - トークンの中身。使えないトークンは、理由によらず ErrInvalidToken。
func (verifier *Verifier) Verify(requestContext context.Context, tokenText string) (*Claims, error) {
	// キャッシュが古いときは、先に取得し直す(失敗しても、いまのキャッシュで進める)。
	if verifier.isStale() {
		verifier.refreshIfAllowed(requestContext)
	}
	// 署名などを確かめる。鍵は、kid で探し、無ければ、取得し直してから、もう一度探す。
	claims := &Claims{}
	parsed, parseError := jwt.ParseWithClaims(tokenText, claims, func(unverified *jwt.Token) (any, error) {
		keyID, _ := unverified.Header["kid"].(string)
		if publicKey, exists := verifier.lookup(keyID); exists {
			return publicKey, nil
		}
		verifier.refreshIfAllowed(requestContext)
		if publicKey, exists := verifier.lookup(keyID); exists {
			return publicKey, nil
		}
		return nil, errors.New("知らない kid です")
	}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithIssuer(Issuer), jwt.WithExpirationRequired(), jwt.WithTimeFunc(verifier.options.Now))
	if parseError != nil || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	// 種類と、必須の項目を確かめる(refresh token を、access token として使わせない)。
	if claims.Type != TypeAccess || claims.Subject == "" || claims.ID == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
