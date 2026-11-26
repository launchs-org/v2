// Package credential は、DB に保存する秘密(Google の合鍵、webhook の署名の秘密)を、暗号化・復号する。
//
// AES-256-GCM を使う。鍵は、環境変数 EXPORT_CREDENTIAL_KEY(32 バイトを base64 にしたもの)。
// 暗号文は、「ランダムな nonce(12 バイト)+ 暗号文と認証タグ」を、つなげたもの。
package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// keyLength は、AES-256 の鍵の長さ(バイト)。
const keyLength = 32

// ErrDecrypt は、復号できなかったときのエラー(鍵が違う、または、データが壊れている・改ざんされている)。
var ErrDecrypt = errors.New("秘密を復号できませんでした")

// Cipher は、秘密を暗号化・復号する。
type Cipher struct {
	// aead は、AES-GCM。
	aead cipher.AEAD
}

// NewCipher は、base64 の鍵から、Cipher を作る。
//
// 引数:
//   - encodedKey: 32 バイトを base64(標準)にした文字列(例: `openssl rand -base64 32`)。
//
// 戻り値:
//   - 作った Cipher。鍵が空、base64 でない、32 バイトでないときはエラー。
func NewCipher(encodedKey string) (*Cipher, error) {
	// 鍵が空なら、機能は無効(呼び出し側が、空かどうかを先に見る)。
	if encodedKey == "" {
		return nil, errors.New("暗号化の鍵が空です")
	}
	// base64 を戻して、長さを確かめる。
	key, decodeError := base64.StdEncoding.DecodeString(encodedKey)
	if decodeError != nil {
		return nil, fmt.Errorf("暗号化の鍵が base64 ではありません: %w", decodeError)
	}
	if len(key) != keyLength {
		return nil, fmt.Errorf("暗号化の鍵は、%d バイトでなければなりません(今は %d バイト)", keyLength, len(key))
	}
	// AES-GCM を用意する。
	block, blockError := aes.NewCipher(key)
	if blockError != nil {
		return nil, blockError
	}
	aead, aeadError := cipher.NewGCM(block)
	if aeadError != nil {
		return nil, aeadError
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt は、平文を暗号化する。
//
// 引数:
//   - plaintext: 暗号化する文字列。
//
// 戻り値:
//   - nonce + 暗号文。乱数を得られなければエラー。
func (cryptoCipher *Cipher) Encrypt(plaintext string) ([]byte, error) {
	// 毎回違う nonce を作る(同じ nonce の使い回しは、GCM では致命的)。
	nonce := make([]byte, cryptoCipher.aead.NonceSize())
	if _, readError := rand.Read(nonce); readError != nil {
		return nil, fmt.Errorf("乱数を得られませんでした: %w", readError)
	}
	// nonce を先頭に付けて、暗号化した結果をつなげる。
	return cryptoCipher.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt は、Encrypt の結果を、平文に戻す。
//
// 引数:
//   - ciphertext: Encrypt が返したバイト列。
//
// 戻り値:
//   - 平文。鍵が違う、または、壊れている・改ざんされているときは ErrDecrypt。
func (cryptoCipher *Cipher) Decrypt(ciphertext []byte) (string, error) {
	// nonce の分より短ければ、壊れている。
	nonceSize := cryptoCipher.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", ErrDecrypt
	}
	// 先頭の nonce と、残りの暗号文に分けて、復号する(認証タグが合わなければ、エラー)。
	plaintext, openError := cryptoCipher.aead.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
	if openError != nil {
		return "", ErrDecrypt
	}
	return string(plaintext), nil
}
