package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestRedactSensitiveAttribute は、名前に機密を表す語を含むフィールドの値が伏せ字になり、
// それ以外のフィールドはそのままになることを確認する。
func TestRedactSensitiveAttribute(testingT *testing.T) {
	// ケースごとの、フィールドの名前と、伏せ字になるかどうかを並べる。
	testCases := []struct {
		name           string
		key            string
		expectRedacted bool
	}{
		{"password", "password", true},
		{"名前に password を含む", "db_password", true},
		{"大文字小文字を区別しない", "Authorization", true},
		{"access_token", "access_token", true},
		{"refresh_token", "refresh_token", true},
		{"secret", "client_secret", true},
		{"DATABASE_URL", "DATABASE_URL", true},
		{"project_id は機密ではない", "project_id", false},
		{"user_id は機密ではない", "user_id", false},
		{"msg は機密ではない", "msg", false},
	}

	// ケースを1つずつ、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// 値が分かっているフィールドを作る。
			originalAttribute := slog.String(testCase.key, "元の値")

			// テスト対象の redactSensitiveAttribute を呼ぶ。groups は使わないので nil にする。
			actualAttribute := redactSensitiveAttribute(nil, originalAttribute)

			// 結果のフィールドの値を、文字列として取り出す。
			actualValue := actualAttribute.Value.String()
			// 伏せ字になるはずなのに、なっていなければ、失敗にする。
			if testCase.expectRedacted && actualValue != redactedValue {
				subTest.Errorf("伏せ字になっていない: %q", actualValue)
			}
			// 機密でないのに、値が変わっていたら、失敗にする。
			if !testCase.expectRedacted && actualValue != "元の値" {
				subTest.Errorf("機密でないのに値が変わった: %q", actualValue)
			}
			// どちらの場合も、フィールドの名前は変わってはいけない。
			if actualAttribute.Key != testCase.key {
				subTest.Errorf("フィールドの名前が変わった: 期待 %q、実際 %q", testCase.key, actualAttribute.Key)
			}
		})
	}
}

// TestNewRedactsSensitiveFields は、New で作ったロガーが、機密のフィールドの値を出力しないことを確認する。
func TestNewRedactsSensitiveFields(testingT *testing.T) {
	// ログの出力先を、あとで中身を調べられるバッファにする。
	var output bytes.Buffer
	// テスト対象のロガーを作る。
	testLogger := New(&output, "backend", slog.LevelInfo)

	// パスワードを含むフィールドを付けて、ログを1件出力する。
	testLogger.Info("ログインした", "user_id", "user-1", "password", "super-secret-password")

	// 出力された文字列を取り出す。
	outputText := output.String()
	// パスワードの値が出力に残っていたら、失敗にする。
	if strings.Contains(outputText, "super-secret-password") {
		testingT.Errorf("パスワードがログに出ている: %s", outputText)
	}
	// 伏せ字が出力に無ければ、失敗にする。
	if !strings.Contains(outputText, redactedValue) {
		testingT.Errorf("伏せ字が出ていない: %s", outputText)
	}
	// 機密でないフィールドまで消えていたら、失敗にする。
	if !strings.Contains(outputText, "user-1") {
		testingT.Errorf("機密でないフィールドが消えている: %s", outputText)
	}
}
