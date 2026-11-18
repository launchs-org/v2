// Package keys は、トークンの署名鍵(Ed25519)の読み込みと、公開鍵(JWKS)の配信用の形への変換を行う。
package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// keyFileExtension は、鍵のファイルの拡張子(ファイル名から拡張子を除いたものが、kid)。
const keyFileExtension = ".pem"

// JSONWebKey は、JWKS に載せる、公開鍵1つ。
type JSONWebKey struct {
	// KeyType は、鍵の種類("OKP")。
	KeyType string `json:"kty"`

	// Curve は、曲線("Ed25519")。
	Curve string `json:"crv"`

	// Algorithm は、署名のアルゴリズム("EdDSA")。
	Algorithm string `json:"alg"`

	// Use は、用途("sig")。
	Use string `json:"use"`

	// KeyID は、鍵の ID(トークンのヘッダの kid)。
	KeyID string `json:"kid"`

	// PublicKey は、公開鍵(base64url)。
	PublicKey string `json:"x"`
}

// KeyStore は、読み込んだ署名鍵の一式。署名には有効な1つを使い、検証には全ての公開鍵を使う。
type KeyStore struct {
	// privateKeys は、kid ごとの秘密鍵。
	privateKeys map[string]ed25519.PrivateKey

	// activeKeyID は、署名に使う鍵の kid。
	activeKeyID string
}

// Load は、ディレクトリの鍵(<kid>.pem、PKCS#8 の Ed25519)を、全て読み込む。
//
// 署名に使う鍵は、activeKeyID。空のときは、kid の辞書順で最後のもの(日付入りの kid なら、最も新しいもの)を使う。
// ディレクトリに鍵が無く、autoGenerate が true のときは、鍵を1つ作って保存する(開発用)。
//
// 引数:
//   - directory: 鍵のディレクトリ。
//   - activeKeyID: 署名に使う鍵の kid(空なら、自動で選ぶ)。
//   - autoGenerate: 鍵が1つも無いとき、作るか。
//   - now: 鍵を作るときの、kid に使う時刻。
//
// 戻り値:
//   - 読み込んだ KeyStore。読めない・不正な鍵、activeKeyID の鍵が無い、鍵が1つも無い、のときはエラー。
func Load(directory string, activeKeyID string, autoGenerate bool, now time.Time) (*KeyStore, error) {
	// ディレクトリの中の鍵のファイルを探す。
	pattern := filepath.Join(directory, "*"+keyFileExtension)
	files, globError := filepath.Glob(pattern)
	if globError != nil {
		return nil, fmt.Errorf("鍵のディレクトリを調べられませんでした: %w", globError)
	}
	// 鍵が無いときは、許されていれば作る。
	if len(files) == 0 {
		if !autoGenerate {
			return nil, fmt.Errorf("鍵のディレクトリ %q に、鍵(%s)がありません", directory, keyFileExtension)
		}
		generatedFile, generateError := generate(directory, now)
		if generateError != nil {
			return nil, generateError
		}
		files = []string{generatedFile}
	}
	// 鍵を読み込む。
	privateKeys := map[string]ed25519.PrivateKey{}
	for _, file := range files {
		privateKey, readError := readPrivateKey(file)
		if readError != nil {
			return nil, readError
		}
		privateKeys[strings.TrimSuffix(filepath.Base(file), keyFileExtension)] = privateKey
	}
	// 署名に使う鍵を決める。
	keyIDs := sortedKeyIDs(privateKeys)
	if activeKeyID == "" {
		activeKeyID = keyIDs[len(keyIDs)-1]
	}
	if _, exists := privateKeys[activeKeyID]; !exists {
		return nil, fmt.Errorf("署名に使う鍵 %q が、ディレクトリにありません", activeKeyID)
	}
	return &KeyStore{privateKeys: privateKeys, activeKeyID: activeKeyID}, nil
}

// generate は、新しい鍵を作って、ディレクトリに保存する(所有者だけが読める権限で)。
//
// 引数:
//   - directory: 鍵のディレクトリ(無ければ作る)。
//   - now: kid に使う時刻("key-" + 年月日)。
//
// 戻り値:
//   - 保存したファイルのパス。
func generate(directory string, now time.Time) (string, error) {
	// ディレクトリを作る。
	if mkdirError := os.MkdirAll(directory, 0o700); mkdirError != nil {
		return "", fmt.Errorf("鍵のディレクトリを作れませんでした: %w", mkdirError)
	}
	// 鍵を作って、PKCS#8 の PEM にする。
	_, privateKey, generateError := ed25519.GenerateKey(rand.Reader)
	if generateError != nil {
		return "", fmt.Errorf("鍵を作れませんでした: %w", generateError)
	}
	encoded, marshalError := x509.MarshalPKCS8PrivateKey(privateKey)
	if marshalError != nil {
		return "", fmt.Errorf("鍵を書き出せませんでした: %w", marshalError)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	// 保存する(すでにあれば、上書きしない)。
	file := filepath.Join(directory, "key-"+now.UTC().Format("2006-01-02")+keyFileExtension)
	handle, openError := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if openError != nil {
		return "", fmt.Errorf("鍵のファイルを作れませんでした: %w", openError)
	}
	defer func() { _ = handle.Close() }()
	if _, writeError := handle.Write(pemBytes); writeError != nil {
		return "", fmt.Errorf("鍵を保存できませんでした: %w", writeError)
	}
	return file, nil
}

// readPrivateKey は、PEM のファイルから、Ed25519 の秘密鍵を読む。
//
// 引数:
//   - file: PEM のファイル。
//
// 戻り値:
//   - 秘密鍵。読めない、PEM でない、Ed25519 でないときはエラー。
func readPrivateKey(file string) (ed25519.PrivateKey, error) {
	// ファイルを読んで、PEM を取り出す。
	content, readError := os.ReadFile(file)
	if readError != nil {
		return nil, fmt.Errorf("鍵のファイルを読めませんでした: %w", readError)
	}
	block, _ := pem.Decode(content)
	if block == nil {
		return nil, fmt.Errorf("鍵のファイル %s が、PEM の形ではありません", file)
	}
	// PKCS#8 として読み、Ed25519 であることを確かめる。
	parsed, parseError := x509.ParsePKCS8PrivateKey(block.Bytes)
	if parseError != nil {
		return nil, fmt.Errorf("鍵のファイル %s を読めませんでした: %w", file, parseError)
	}
	privateKey, isEd25519 := parsed.(ed25519.PrivateKey)
	if !isEd25519 {
		return nil, errors.New("鍵のファイル " + file + " が、Ed25519 の鍵ではありません")
	}
	return privateKey, nil
}

// sortedKeyIDs は、kid を辞書順に並べて返す。
//
// 引数:
//   - privateKeys: kid ごとの秘密鍵。
//
// 戻り値:
//   - 辞書順の kid。
func sortedKeyIDs(privateKeys map[string]ed25519.PrivateKey) []string {
	// kid を集めて並べる。
	keyIDs := make([]string, 0, len(privateKeys))
	for keyID := range privateKeys {
		keyIDs = append(keyIDs, keyID)
	}
	sort.Strings(keyIDs)
	return keyIDs
}

// ActiveKeyID は、署名に使う鍵の kid を返す。
//
// 戻り値:
//   - kid。
func (keyStore *KeyStore) ActiveKeyID() string {
	// 決めてある kid を返す。
	return keyStore.activeKeyID
}

// ActivePrivateKey は、署名に使う秘密鍵を返す。
//
// 戻り値:
//   - 秘密鍵。
func (keyStore *KeyStore) ActivePrivateKey() ed25519.PrivateKey {
	// 有効な kid の鍵を返す。
	return keyStore.privateKeys[keyStore.activeKeyID]
}

// PublicKey は、kid に対応する公開鍵を返す(検証に使う)。
//
// 引数:
//   - keyID: トークンのヘッダの kid。
//
// 戻り値:
//   - 公開鍵と、あったか。
func (keyStore *KeyStore) PublicKey(keyID string) (ed25519.PublicKey, bool) {
	// 秘密鍵から、公開鍵を取り出す。
	privateKey, exists := keyStore.privateKeys[keyID]
	if !exists {
		return nil, false
	}
	publicKey, _ := privateKey.Public().(ed25519.PublicKey)
	return publicKey, true
}

// JSONWebKeys は、全ての鍵の公開鍵を、JWKS の形で返す(kid の順)。
//
// 戻り値:
//   - JWKS の keys に載せる、公開鍵の一覧。
func (keyStore *KeyStore) JSONWebKeys() []JSONWebKey {
	// kid の順に、公開鍵を JWK にする。
	keys := make([]JSONWebKey, 0, len(keyStore.privateKeys))
	for _, keyID := range sortedKeyIDs(keyStore.privateKeys) {
		publicKey, _ := keyStore.PublicKey(keyID)
		keys = append(keys, JSONWebKey{
			KeyType: "OKP", Curve: "Ed25519", Algorithm: "EdDSA", Use: "sig", KeyID: keyID,
			PublicKey: base64.RawURLEncoding.EncodeToString(publicKey),
		})
	}
	return keys
}
