package logger

import (
	"log/slog"
	"strings"
)

// redactedValue は、機密のフィールドの値の代わりに出力する文字列。
const redactedValue = "[REDACTED]"

// sensitiveKeyWords は、フィールドの名前に含まれていたら、機密として扱う語。
// フィールドの名前は小文字にして比較する。
var sensitiveKeyWords = []string{"password", "secret", "token", "authorization", "database_url"}

// redactSensitiveAttribute は、フィールドの名前に機密を表す語が含まれるとき、
// 値を伏せ字(redactedValue)に置き換える。
//
// パスワード、トークン、環境変数の値(DATABASE_URL など)を、うっかりログに出さないための安全装置。
// 名前に語が含まれるかで判断するので、db_password や access_token も対象になる。
// 逆に、token_count のような機密でないフィールドも伏せ字になることがあるが、安全側に倒している。
//
// slog.HandlerOptions の ReplaceAttr として使う。
//
// 引数:
//   - groups: フィールドが属するグループの名前。使わない。
//   - attribute: 出力しようとしているフィールド。
//
// 戻り値:
//   - 機密でないときは、そのままのフィールド。
//   - 機密のときは、同じ名前で、値を伏せ字にしたフィールド。
func redactSensitiveAttribute(groups []string, attribute slog.Attr) slog.Attr {
	// 大文字小文字を区別せずに比較するため、フィールドの名前を小文字にする。
	lowerKey := strings.ToLower(attribute.Key)
	// 機密を表す語を1つずつ調べる。
	for _, keyWord := range sensitiveKeyWords {
		// 名前に機密を表す語が含まれるかを調べる。
		if strings.Contains(lowerKey, keyWord) {
			// 値を伏せ字にした、同じ名前のフィールドを返す。値は元の型に関わらず文字列になる。
			return slog.String(attribute.Key, redactedValue)
		}
	}
	// 機密でないフィールドは、そのまま返す。
	return attribute
}
