package credential

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// newTestKey は、試験用の鍵(32 バイトを base64 にしたもの)を返す。
func newTestKey(fill byte) string {
	// 同じ値で埋めた 32 バイトを、base64 にする。
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}

// TestCipherRoundTrip は、暗号化して復号すると元に戻り、毎回違う暗号文になることを確かめる。
func TestCipherRoundTrip(testingT *testing.T) {
	cryptoCipher, newError := NewCipher(newTestKey(1))
	if newError != nil {
		testingT.Fatalf("作れるはず: %v", newError)
	}
	first, _ := cryptoCipher.Encrypt("refresh-token-日本語")
	second, _ := cryptoCipher.Encrypt("refresh-token-日本語")
	// 同じ平文でも、nonce が違うので、暗号文は違う。
	if bytes.Equal(first, second) || strings.Contains(string(first), "refresh-token") {
		testingT.Errorf("毎回違う暗号文で、平文が見えないはず")
	}
	// 復号すると、元に戻る。
	if plaintext, decryptError := cryptoCipher.Decrypt(first); decryptError != nil || plaintext != "refresh-token-日本語" {
		testingT.Errorf("元に戻るはず: %q %v", plaintext, decryptError)
	}
}

// TestCipherRejects は、鍵が違う、改ざんされた、短すぎるデータを復号できないことを確かめる。
func TestCipherRejects(testingT *testing.T) {
	cryptoCipher, _ := NewCipher(newTestKey(1))
	otherCipher, _ := NewCipher(newTestKey(2))
	encrypted, _ := cryptoCipher.Encrypt("secret")
	// 別の鍵では、復号できない。
	if _, decryptError := otherCipher.Decrypt(encrypted); !errors.Is(decryptError, ErrDecrypt) {
		testingT.Errorf("別の鍵では復号できないはず: %v", decryptError)
	}
	// 1 バイトでも変えると、復号できない。
	tampered := append([]byte{}, encrypted...)
	tampered[len(tampered)-1] ^= 0xff
	if _, decryptError := cryptoCipher.Decrypt(tampered); !errors.Is(decryptError, ErrDecrypt) {
		testingT.Errorf("改ざんは検出するはず: %v", decryptError)
	}
	// 短すぎるデータは、復号できない。
	if _, decryptError := cryptoCipher.Decrypt([]byte{1, 2, 3}); !errors.Is(decryptError, ErrDecrypt) {
		testingT.Errorf("短いデータは復号できないはず: %v", decryptError)
	}
}

// TestNewCipherRejectsBadKeys は、空、base64 でない、長さが違う鍵を拒否することを確かめる。
func TestNewCipherRejectsBadKeys(testingT *testing.T) {
	for name, key := range map[string]string{"空": "", "base64 でない": "!!!", "短い": base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, newError := NewCipher(key); newError == nil {
			testingT.Errorf("%s は拒否するはず", name)
		}
	}
}
