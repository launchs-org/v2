// Package apitoken は、機械が API を呼ぶための JWT(API トークン)の発行・検証・権限の判定を行う。
//
// 署名は HS512(backend だけが、発行も検証も行うので、共通の秘密で足りる)。秘密は 64 バイト以上(HS512 の鍵の長さ)にし、
// 検証では HS512 だけを受け付ける(アルゴリズムの差し替え、alg=none を拒否する)。
// トークンは、接頭辞 "lkt_" を付けて渡す(利用者のログインの JWT と、見分けるため)。
// 許す操作(scope)と対象(deploys、projects)は claim に入れる。有効・無効(失効)だけを、DB で管理する。
package apitoken

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// Prefix は、API トークンの先頭に付ける印。
	Prefix = "lkt_"

	// TokenType は、claim "type" の値。
	TokenType = "api"

	// Issuer は、claim "iss" の値。
	Issuer = "launchs-org-api"

	// ScopeRead は、読み取り(GET)だけを許す scope。
	ScopeRead = "read"

	// ScopeWrite は、全ての操作を許す scope(読み取りを含む)。
	ScopeWrite = "write"

	// ScopeDeployImage は、deploy のイメージの更新(PUT /deploy/:deployId/image と webhook)だけを許す scope。deploy 用のトークン。
	ScopeDeployImage = "deploy:image"

	// MinimumSecretLength は、署名の秘密の最低の長さ(バイト)。HS512 のハッシュの長さ(64 バイト)に合わせる。
	MinimumSecretLength = 64

	// imageRoutePath は、イメージの更新のルート(Echo に登録したパターン)。
	imageRoutePath = "/api/v1/deploy/:deployId/image"
)

// ErrInvalidToken は、トークンが、署名・期限・形式・種類のどれかで、受け付けられないときのエラー。
var ErrInvalidToken = errors.New("API トークンが正しくありません")

// ErrForbidden は、トークンの権限(scope と対象)では、その操作を許されないときのエラー。
var ErrForbidden = errors.New("この API トークンでは、その操作はできません")

// Claims は、API トークンの claims。
type Claims struct {
	jwt.RegisteredClaims

	// Type は、トークンの種類(常に "api")。
	Type string `json:"type"`

	// Scope は、許す操作(ScopeRead、ScopeWrite、ScopeDeployImage)。
	Scope []string `json:"scope"`

	// Deploys は、操作できる deploy の ID。空なら、絞らない。
	Deploys []string `json:"deploys,omitempty"`

	// Projects は、操作できるプロジェクトの ID。空なら、絞らない。
	Projects []string `json:"projects,omitempty"`
}

// ValidScope は、scope として受け付ける値かを返す。
//
// 引数:
//   - scope: 確かめる値。
//
// 戻り値:
//   - 受け付ける値なら true。
func ValidScope(scope string) bool {
	// 3つのどれかなら、受け付ける。
	return scope == ScopeRead || scope == ScopeWrite || scope == ScopeDeployImage
}

// Signer は、API トークンを発行し、検証する。
type Signer struct {
	secret []byte
	now    func() time.Time
}

// NewSigner は、Signer を作る。
//
// 引数:
//   - secret: 署名の秘密。64 バイト以上。
//   - now: 現在時刻を返す関数(nil なら time.Now)。
//
// 戻り値:
//   - 作成した Signer。秘密が短いときはエラー。
func NewSigner(secret []byte, now func() time.Time) (*Signer, error) {
	// 短い秘密は、総当たりで破られるので、受け付けない。
	if len(secret) < MinimumSecretLength {
		return nil, fmt.Errorf("API トークンの秘密は %d バイト以上にしてください", MinimumSecretLength)
	}
	// 時計の指定が無いときは、現在の時刻を使う。
	if now == nil {
		now = time.Now
	}
	return &Signer{secret: secret, now: now}, nil
}

// Issue は、API トークンを発行する。
//
// 引数:
//   - tokenID: トークンの jti。
//   - userID: 持ち主のユーザー ID(sub)。
//   - scope: 許す操作。
//   - deploys: 操作できる deploy の ID(空なら絞らない)。
//   - projects: 操作できるプロジェクトの ID(空なら絞らない)。
//   - issuedAt: 発行の日時(iat)。同じ値で作り直すと、同じトークンの文字列になる(HS512 は、同じ入力から、同じ署名ができる)。
//   - expiresAt: 期限。nil なら無期限。
//
// 戻り値:
//   - 接頭辞 "lkt_" を付けた、トークンの文字列。
func (signer *Signer) Issue(tokenID string, userID string, scope []string, deploys []string, projects []string, issuedAt time.Time, expiresAt *time.Time) (string, error) {
	// 登録済みの claim を作る(期限が無いときは exp を入れない)。
	registered := jwt.RegisteredClaims{Issuer: Issuer, Subject: userID, ID: tokenID, IssuedAt: jwt.NewNumericDate(issuedAt)}
	if expiresAt != nil {
		registered.ExpiresAt = jwt.NewNumericDate(*expiresAt)
	}
	claims := Claims{RegisteredClaims: registered, Type: TokenType, Scope: scope, Deploys: deploys, Projects: projects}
	// 署名する。
	signed, signError := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString(signer.secret)
	if signError != nil {
		return "", fmt.Errorf("API トークンに署名できませんでした: %w", signError)
	}
	return Prefix + signed, nil
}

// Parse は、API トークンを検証して、claims を返す。
//
// 確かめること: 接頭辞、アルゴリズムが HS512、署名、iss、期限(あるときだけ)、type が api、sub と jti がある。
//
// 引数:
//   - tokenText: 接頭辞つきのトークンの文字列。
//
// 戻り値:
//   - claims。受け付けられないときは ErrInvalidToken。
func (signer *Signer) Parse(tokenText string) (*Claims, error) {
	// 接頭辞が無いものは、API トークンではない。
	rawToken, hasPrefix := strings.CutPrefix(tokenText, Prefix)
	if !hasPrefix {
		return nil, ErrInvalidToken
	}
	// 署名などを確かめる。
	claims := &Claims{}
	parsed, parseError := jwt.ParseWithClaims(rawToken, claims, func(*jwt.Token) (any, error) { return signer.secret, nil },
		jwt.WithValidMethods([]string{"HS512"}), jwt.WithIssuer(Issuer), jwt.WithTimeFunc(signer.now))
	if parseError != nil || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	// 種類と、必須の項目を確かめる。
	if claims.Type != TokenType || claims.Subject == "" || claims.ID == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// Authorize は、トークンの権限で、その操作を許されるかを判定する。
//
// 引数:
//   - claims: 検証済みの claims。
//   - method: HTTP のメソッド。
//   - routePath: Echo に登録したルートのパターン(例: /api/v1/deploy/:deployId/image)。
//   - deployID: パスの deployId(無ければ空)。
//   - projectID: パスの projectId(無ければ空)。
//
// 戻り値:
//   - 許されるなら nil。許されないときは ErrForbidden。
func (claims *Claims) Authorize(method string, routePath string, deployID string, projectID string) error {
	// 操作の種類を許すかを、確かめる。
	if !claims.allowsOperation(method, routePath) {
		return ErrForbidden
	}
	// 対象を絞っていないときは、ここまで。
	if len(claims.Deploys) == 0 && len(claims.Projects) == 0 {
		return nil
	}
	// 絞っているときは、パスの deploy かプロジェクトが、許した範囲にあることを求める。
	if deployID != "" && slices.Contains(claims.Deploys, deployID) {
		return nil
	}
	if projectID != "" && slices.Contains(claims.Projects, projectID) {
		return nil
	}
	return ErrForbidden
}

// allowsOperation は、scope が、そのメソッドとルートの操作を許すかを返す。
//
// 引数:
//   - method: HTTP のメソッド。
//   - routePath: ルートのパターン。
//
// 戻り値:
//   - 許すなら true。
func (claims *Claims) allowsOperation(method string, routePath string) bool {
	// write は、全ての操作を許す。
	if slices.Contains(claims.Scope, ScopeWrite) {
		return true
	}
	// read は、読み取りだけを許す。
	if slices.Contains(claims.Scope, ScopeRead) && (method == "GET" || method == "HEAD") {
		return true
	}
	// deploy:image は、イメージの更新だけを許す。
	return slices.Contains(claims.Scope, ScopeDeployImage) && method == "PUT" && routePath == imageRoutePath
}

// HasScope は、トークンが、その scope を持つかを返す。
//
// 引数:
//   - scope: 確かめる scope。
//
// 戻り値:
//   - 持っていれば true。
func (claims *Claims) HasScope(scope string) bool {
	// scope の一覧に含まれるかを返す。
	return slices.Contains(claims.Scope, scope)
}
