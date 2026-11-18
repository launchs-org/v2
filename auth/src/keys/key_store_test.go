package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadGeneratesAndReloads は、鍵が無いとき(許されていれば)作り、再起動しても同じ鍵を読み、許されていなければエラーにすることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestLoadGeneratesAndReloads(testing *testing.T) {
	// 鍵が無く、作ることも許されていないと、エラー。
	directory := testing.TempDir()
	if _, loadError := Load(directory, "", false, time.Now()); loadError == nil {
		testing.Error("鍵が無ければエラーのはず")
	}
	// 作ることを許すと、鍵が1つ作られ、kid が日付になり、ファイルは所有者だけが読める。
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	created, createError := Load(directory, "", true, now)
	if createError != nil || created.ActiveKeyID() != "key-2026-11-15" {
		testing.Fatalf("kid が日付になるはず: %v", createError)
	}
	info, statError := os.Stat(filepath.Join(directory, "key-2026-11-15.pem"))
	if statError != nil || info.Mode().Perm() != 0o600 {
		testing.Errorf("権限は 0600 のはず: %v %v", info, statError)
	}
	// 読み直すと、同じ鍵になる(新しい鍵は作られない)。
	reloaded, reloadError := Load(directory, "", true, now.Add(48*time.Hour))
	if reloadError != nil || reloaded.ActiveKeyID() != "key-2026-11-15" || len(reloaded.JSONWebKeys()) != 1 {
		testing.Fatalf("同じ鍵のはず: %v", reloadError)
	}
	if string(created.ActivePrivateKey()) != string(reloaded.ActivePrivateKey()) {
		testing.Error("秘密鍵が同じはず")
	}
}

// TestLoadActiveKeyAndJWKS は、鍵が複数あるとき、署名に使う鍵を選べること、JWKS に全ての公開鍵が、kid の順で載ることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestLoadActiveKeyAndJWKS(testing *testing.T) {
	// 2つの鍵を作る(別の日付で、2回作る)。
	directory := testing.TempDir()
	if _, firstError := Load(directory, "", true, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); firstError != nil {
		testing.Fatal(firstError)
	}
	if _, secondError := generate(directory, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); secondError != nil {
		testing.Fatal(secondError)
	}
	// 指定しないと、辞書順で最後(新しい日付)が、署名に使われる。
	automatic, autoError := Load(directory, "", false, time.Now())
	if autoError != nil || automatic.ActiveKeyID() != "key-2026-11-01" {
		testing.Fatalf("新しい方が選ばれるはず: %v", autoError)
	}
	// 指定すると、その鍵が使われる。無い kid はエラー。
	explicit, explicitError := Load(directory, "key-2026-10-01", false, time.Now())
	if explicitError != nil || explicit.ActiveKeyID() != "key-2026-10-01" {
		testing.Fatalf("指定した鍵が選ばれるはず: %v", explicitError)
	}
	if _, missingError := Load(directory, "key-nothing", false, time.Now()); missingError == nil {
		testing.Error("無い kid はエラーのはず")
	}
	// JWKS には、全ての公開鍵が、kid の順で載り、公開鍵が、秘密鍵と対になっている。
	jsonWebKeys := explicit.JSONWebKeys()
	if len(jsonWebKeys) != 2 || jsonWebKeys[0].KeyID != "key-2026-10-01" || jsonWebKeys[1].KeyID != "key-2026-11-01" {
		testing.Fatalf("2つの鍵が、kid の順に載るはず: %+v", jsonWebKeys)
	}
	first := jsonWebKeys[0]
	if first.KeyType != "OKP" || first.Curve != "Ed25519" || first.Algorithm != "EdDSA" || first.Use != "sig" {
		testing.Errorf("JWK の形が違う: %+v", first)
	}
	decoded, decodeError := base64.RawURLEncoding.DecodeString(first.PublicKey)
	expectedPublic, _ := explicit.PublicKey("key-2026-10-01")
	if decodeError != nil || string(decoded) != string(expectedPublic) || len(decoded) != ed25519.PublicKeySize {
		testing.Errorf("公開鍵が違う: %v", decodeError)
	}
	// 知らない kid の公開鍵は、無い。
	if _, exists := explicit.PublicKey("key-nothing"); exists {
		testing.Error("知らない kid は無いはず")
	}
}

// TestLoadRejectsBrokenKeys は、PEM でない、Ed25519 でない鍵のファイルを、エラーにすることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestLoadRejectsBrokenKeys(testing *testing.T) {
	// PEM でないファイル。
	notPEM := testing.TempDir()
	_ = os.WriteFile(filepath.Join(notPEM, "bad.pem"), []byte("not a pem"), 0o600)
	if _, loadError := Load(notPEM, "", false, time.Now()); loadError == nil {
		testing.Error("PEM でなければエラーのはず")
	}
	// RSA の鍵(PKCS#8 だが、Ed25519 ではない)。
	rsaDirectory := testing.TempDir()
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	rsaBytes, _ := x509.MarshalPKCS8PrivateKey(rsaKey)
	_ = os.WriteFile(filepath.Join(rsaDirectory, "rsa.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rsaBytes}), 0o600)
	if _, loadError := Load(rsaDirectory, "", false, time.Now()); loadError == nil {
		testing.Error("Ed25519 でない鍵はエラーのはず")
	}
	// PEM だが、PKCS#8 でない中身。
	brokenBody := testing.TempDir()
	_ = os.WriteFile(filepath.Join(brokenBody, "bad.pem"), []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"), 0o600)
	if _, loadError := Load(brokenBody, "", false, time.Now()); loadError == nil {
		testing.Error("読めない鍵はエラーのはず")
	}
}
