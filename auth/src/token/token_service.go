// Package token は、access token と refresh token(どちらも JWT、EdDSA)の発行と検証を行う。
package token

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"launchs/auth/src/keys"
)

const (
	// Issuer は、トークンの iss。
	Issuer = "launchs-org"

	// TypeAccess と TypeRefresh は、トークンの type。
	TypeAccess  = "access"
	TypeRefresh = "refresh"

	// AccessTokenLifetime は、access token の有効期限(15分)。
	AccessTokenLifetime = 15 * time.Minute

	// RefreshTokenLifetime は、refresh token の有効期限(90日。固定)。
	RefreshTokenLifetime = 90 * 24 * time.Hour

	// jtiPrefix は、トークンの jti の接頭辞。
	jtiPrefix = "tokenid-"
)

// ErrInvalidToken は、トークンが、署名・期限・形式・種類のどれかで、受け付けられないときのエラー。
var ErrInvalidToken = errors.New("トークンが正しくありません")

// Claims は、トークンの claims。
type Claims struct {
	jwt.RegisteredClaims

	// Name は、ユーザー名。
	Name string `json:"name"`

	// Role は、役割("user" か "admin")。access token だけに入る。
	Role string `json:"role,omitempty"`

	// Type は、トークンの種類("access" か "refresh")。
	Type string `json:"type"`
}

// Issued は、発行したトークンと、その情報。
type Issued struct {
	// Token は、署名したトークンの文字列。
	Token string

	// ID は、トークンの jti。
	ID string

	// ExpiresAt は、有効期限。
	ExpiresAt time.Time
}

// Service は、トークンを発行し、検証する。
type Service struct {
	keyStore *keys.KeyStore
	now      func() time.Time
}

// NewService は、Service を作る。
//
// 引数:
//   - keyStore: 署名鍵。
//   - now: 現在時刻を返す関数(テストで差し替える)。
//
// 戻り値:
//   - 作成した Service。
func NewService(keyStore *keys.KeyStore, now func() time.Time) *Service {
	// 署名鍵と、時計を持たせる。
	return &Service{keyStore: keyStore, now: now}
}

// IssueAccess は、access token を発行する(15分)。
//
// 引数:
//   - userID: ユーザーの ID(sub)。
//   - userName: ユーザー名(name)。
//   - role: 役割(role)。
//
// 戻り値:
//   - 発行したトークン。
func (service *Service) IssueAccess(userID string, userName string, role string) (Issued, error) {
	// 種類と期限を決めて、発行する。
	return service.issue(userID, userName, role, TypeAccess, AccessTokenLifetime)
}

// IssueRefresh は、refresh token を発行する(90日)。
//
// 引数:
//   - userID: ユーザーの ID(sub)。
//   - userName: ユーザー名(name)。
//
// 戻り値:
//   - 発行したトークン(jti は、DB に保存して、有効・無効を管理する)。
func (service *Service) IssueRefresh(userID string, userName string) (Issued, error) {
	// 種類と期限を決めて、発行する(role は入れない)。
	return service.issue(userID, userName, "", TypeRefresh, RefreshTokenLifetime)
}

// issue は、claims を作って、有効な鍵で署名する(ヘッダの kid に、鍵の ID を入れる)。
//
// 引数:
//   - userID: ユーザーの ID。
//   - userName: ユーザー名。
//   - role: 役割(空なら入れない)。
//   - tokenType: トークンの種類。
//   - lifetime: 有効期間。
//
// 戻り値:
//   - 発行したトークン。
func (service *Service) issue(userID string, userName string, role string, tokenType string, lifetime time.Duration) (Issued, error) {
	// claims を作る(iat と exp は、秒の数値)。
	issuedAt := service.now()
	expiresAt := issuedAt.Add(lifetime)
	tokenID := jtiPrefix + uuid.NewString()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: Issuer, Subject: userID, ID: tokenID,
			IssuedAt: jwt.NewNumericDate(issuedAt), ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		Name: userName, Role: role, Type: tokenType,
	}
	// 署名する。
	unsigned := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	unsigned.Header["kid"] = service.keyStore.ActiveKeyID()
	signed, signError := unsigned.SignedString(service.keyStore.ActivePrivateKey())
	if signError != nil {
		return Issued{}, fmt.Errorf("トークンに署名できませんでした: %w", signError)
	}
	return Issued{Token: signed, ID: tokenID, ExpiresAt: expiresAt}, nil
}

// Parse は、トークンを検証して、claims を返す。
//
// 確かめること: アルゴリズムが EdDSA、kid の鍵で署名が合う、iss が launchs-org、期限が切れていない(必須)、type が期待どおり。
//
// 引数:
//   - tokenText: トークンの文字列。
//   - expectedType: 期待する種類(TypeAccess か TypeRefresh)。
//
// 戻り値:
//   - claims。受け付けられないときは ErrInvalidToken。
func (service *Service) Parse(tokenText string, expectedType string) (*Claims, error) {
	// 鍵は、ヘッダの kid で選ぶ。
	claims := &Claims{}
	parsed, parseError := jwt.ParseWithClaims(tokenText, claims, func(unverified *jwt.Token) (any, error) {
		keyID, _ := unverified.Header["kid"].(string)
		publicKey, exists := service.keyStore.PublicKey(keyID)
		if !exists {
			return nil, errors.New("知らない kid です")
		}
		return publicKey, nil
	}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithIssuer(Issuer), jwt.WithExpirationRequired(), jwt.WithTimeFunc(service.now))
	if parseError != nil || !parsed.Valid {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, parseError)
	}
	// 種類と、sub、jti を確かめる。
	if claims.Type != expectedType || claims.Subject == "" || claims.ID == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
