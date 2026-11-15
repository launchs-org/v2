package token

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"launchs/auth/src/keys"
)

// newTestService は、固定の時計と、一時ディレクトリの鍵で、Service を作る。
//
// 引数:
//   - testing: テストの状態。
//   - clock: 時計(テストが進められる)。
//
// 戻り値:
//   - Service と、鍵のディレクトリ。
func newTestService(testing *testing.T, clock *time.Time) (*Service, string) {
	// 鍵を作って、時計つきの Service を作る。
	directory := testing.TempDir()
	keyStore, loadError := keys.Load(directory, "", true, *clock)
	if loadError != nil {
		testing.Fatal(loadError)
	}
	return NewService(keyStore, func() time.Time { return *clock }), directory
}

// decodePart は、JWT の1つの部分(ヘッダか本体)を、base64url から JSON として読む。
//
// 引数:
//   - testing: テストの状態。
//   - part: JWT の1つの部分。
//
// 戻り値:
//   - JSON のオブジェクト。
func decodePart(testing *testing.T, part string) map[string]any {
	// base64url を戻して、JSON として読む。
	decoded, decodeError := base64.RawURLEncoding.DecodeString(part)
	if decodeError != nil {
		testing.Fatal(decodeError)
	}
	parsed := map[string]any{}
	if unmarshalError := json.Unmarshal(decoded, &parsed); unmarshalError != nil {
		testing.Fatal(unmarshalError)
	}
	return parsed
}

// TestIssueAndParse は、access token と refresh token の、形(claims、kid、EdDSA、期限)と、往復(発行 → 検証)を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestIssueAndParse(testing *testing.T) {
	// 時計を固定して、2つのトークンを発行する。
	clock := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	service, _ := newTestService(testing, &clock)
	access, accessError := service.IssueAccess("userid-1", "alice", "admin")
	refresh, refreshError := service.IssueRefresh("userid-1", "alice")
	if accessError != nil || refreshError != nil {
		testing.Fatal(accessError, refreshError)
	}
	// access token: ヘッダは EdDSA と kid、claims は仕様どおり(iat と exp は数値、期限は15分)。
	parts := strings.Split(access.Token, ".")
	header, claims := decodePart(testing, parts[0]), decodePart(testing, parts[1])
	if header["alg"] != "EdDSA" || header["kid"] != "key-2026-11-15" || header["typ"] != "JWT" {
		testing.Errorf("ヘッダが違う: %v", header)
	}
	if claims["iss"] != "launchs-org" || claims["sub"] != "userid-1" || claims["name"] != "alice" || claims["type"] != "access" || claims["role"] != "admin" ||
		claims["iat"] != float64(clock.Unix()) || claims["exp"] != float64(clock.Add(15*time.Minute).Unix()) || !strings.HasPrefix(claims["jti"].(string), "tokenid-") {
		testing.Errorf("claims が違う: %v", claims)
	}
	// refresh token: 期限は90日で、role は入らない。
	refreshClaims := decodePart(testing, strings.Split(refresh.Token, ".")[1])
	if refreshClaims["type"] != "refresh" || refreshClaims["exp"] != float64(clock.Add(90*24*time.Hour).Unix()) || refreshClaims["role"] != nil || refresh.ID != refreshClaims["jti"] {
		testing.Errorf("refresh token の claims が違う: %v", refreshClaims)
	}
	// 往復: 種類が合えば、検証できて、claims が戻る。
	parsedAccess, parseError := service.Parse(access.Token, TypeAccess)
	if parseError != nil || parsedAccess.Subject != "userid-1" || parsedAccess.Name != "alice" || parsedAccess.Role != "admin" || parsedAccess.ID != access.ID {
		testing.Errorf("検証できるはず: %v %+v", parseError, parsedAccess)
	}
	if _, parseError = service.Parse(refresh.Token, TypeRefresh); parseError != nil {
		testing.Errorf("検証できるはず: %v", parseError)
	}
	// jti は、発行ごとに違う。
	again, _ := service.IssueAccess("userid-1", "alice", "admin")
	if again.ID == access.ID {
		testing.Error("jti は、発行ごとに違うはず")
	}
}

// TestParseRejects は、種類の取り違え、期限切れ、改ざん、知らない kid、別の鍵の署名、アルゴリズムの偽装(none、HS256)、壊れた文字列を、拒否することを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestParseRejects(testing *testing.T) {
	// 時計を固定して、トークンを発行する。
	clock := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	service, directory := newTestService(testing, &clock)
	access, _ := service.IssueAccess("userid-1", "alice", "user")
	refresh, _ := service.IssueRefresh("userid-1", "alice")
	// 種類の取り違え(refresh token を access として、access token を refresh として出す)。
	if _, parseError := service.Parse(refresh.Token, TypeAccess); parseError == nil {
		testing.Error("refresh token は access として使えないはず")
	}
	if _, parseError := service.Parse(access.Token, TypeRefresh); parseError == nil {
		testing.Error("access token は refresh として使えないはず")
	}
	// 期限: 15分たつと、access token は使えないが、refresh token は使える(90日まで)。
	clock = clock.Add(15*time.Minute + time.Second)
	if _, parseError := service.Parse(access.Token, TypeAccess); parseError == nil {
		testing.Error("期限切れの access token は使えないはず")
	}
	if _, parseError := service.Parse(refresh.Token, TypeRefresh); parseError != nil {
		testing.Errorf("refresh token は、まだ使えるはず: %v", parseError)
	}
	clock = clock.Add(90 * 24 * time.Hour)
	if _, parseError := service.Parse(refresh.Token, TypeRefresh); parseError == nil {
		testing.Error("期限切れの refresh token は使えないはず")
	}
	clock = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	// 改ざん: 本体を書き換えると、署名が合わない。
	parts := strings.Split(access.Token, ".")
	forgedClaims := decodePart(testing, parts[1])
	forgedClaims["role"] = "admin"
	forgedBody, _ := json.Marshal(forgedClaims)
	if _, parseError := service.Parse(parts[0]+"."+base64.RawURLEncoding.EncodeToString(forgedBody)+"."+parts[2], TypeAccess); parseError == nil {
		testing.Error("改ざんは拒否するはず")
	}
	// 別の鍵(同じ kid)で署名したもの、知らない kid で署名したものは拒否する。
	_, otherKey, _ := ed25519.GenerateKey(rand.Reader)
	for name, keyID := range map[string]string{"同じ kid": "key-2026-11-15", "知らない kid": "key-unknown", "kid なし": ""} {
		forged := jwt.NewWithClaims(jwt.SigningMethodEdDSA, Claims{
			RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Subject: "userid-1", ID: "tokenid-x", ExpiresAt: jwt.NewNumericDate(clock.Add(time.Hour))}, Type: TypeAccess,
		})
		if keyID != "" {
			forged.Header["kid"] = keyID
		}
		signed, _ := forged.SignedString(otherKey)
		if _, parseError := service.Parse(signed, TypeAccess); parseError == nil {
			testing.Errorf("%s: 別の鍵の署名は拒否するはず", name)
		}
	}
	// アルゴリズムの偽装: alg=none、alg=HS256(公開鍵を秘密として署名)は、拒否する。
	noneToken := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Subject: "userid-1", ID: "tokenid-x", ExpiresAt: jwt.NewNumericDate(clock.Add(time.Hour))}, Type: TypeAccess})
	noneToken.Header["kid"] = "key-2026-11-15"
	noneSigned, _ := noneToken.SignedString(jwt.UnsafeAllowNoneSignatureType)
	keyStore, _ := keys.Load(directory, "", false, clock)
	publicKey, _ := keyStore.PublicKey("key-2026-11-15")
	hmacToken := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Subject: "userid-1", ID: "tokenid-x", ExpiresAt: jwt.NewNumericDate(clock.Add(time.Hour))}, Type: TypeAccess})
	hmacToken.Header["kid"] = "key-2026-11-15"
	hmacSigned, _ := hmacToken.SignedString([]byte(publicKey))
	for name, forged := range map[string]string{"alg=none": noneSigned, "alg=HS256": hmacSigned} {
		if _, parseError := service.Parse(forged, TypeAccess); parseError == nil {
			testing.Errorf("%s は拒否するはず", name)
		}
	}
	// iss が違う、exp が無い、sub が無い、壊れた文字列は拒否する。
	for name, claims := range map[string]Claims{
		"iss が違う": {RegisteredClaims: jwt.RegisteredClaims{Issuer: "other", Subject: "userid-1", ID: "tokenid-x", ExpiresAt: jwt.NewNumericDate(clock.Add(time.Hour))}, Type: TypeAccess},
		"exp が無い": {RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Subject: "userid-1", ID: "tokenid-x"}, Type: TypeAccess},
		"sub が無い": {RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, ID: "tokenid-x", ExpiresAt: jwt.NewNumericDate(clock.Add(time.Hour))}, Type: TypeAccess},
		"jti が無い": {RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Subject: "userid-1", ExpiresAt: jwt.NewNumericDate(clock.Add(time.Hour))}, Type: TypeAccess},
	} {
		unsigned := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
		unsigned.Header["kid"] = service.keyStore.ActiveKeyID()
		signed, _ := unsigned.SignedString(service.keyStore.ActivePrivateKey())
		if _, parseError := service.Parse(signed, TypeAccess); parseError == nil {
			testing.Errorf("%s は拒否するはず", name)
		}
	}
	for _, broken := range []string{"", "abc", "a.b.c", access.Token + "x"} {
		if _, parseError := service.Parse(broken, TypeAccess); parseError == nil {
			testing.Errorf("%q は拒否するはず", broken)
		}
	}
}
