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

	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			originalAttribute := slog.String(testCase.key, "元の値")

			actualAttribute := redactSensitiveAttribute(nil, originalAttribute)

			actualValue := actualAttribute.Value.String()
			if testCase.expectRedacted && actualValue != redactedValue {
				subTest.Errorf("伏せ字になっていない: %q", actualValue)
			}
			if !testCase.expectRedacted && actualValue != "元の値" {
				subTest.Errorf("機密でないのに値が変わった: %q", actualValue)
			}
			if actualAttribute.Key != testCase.key {
				subTest.Errorf("フィールドの名前が変わった: 期待 %q、実際 %q", testCase.key, actualAttribute.Key)
			}
		})
	}
}

// TestNewRedactsSensitiveFields は、New で作ったロガーが、機密のフィールドの値を出力しないことを確認する。
func TestNewRedactsSensitiveFields(testingT *testing.T) {
	var output bytes.Buffer
	testLogger := New(&output, "backend", slog.LevelInfo)

	testLogger.Info("ログインした", "user_id", "user-1", "password", "super-secret-password")

	outputText := output.String()
	if strings.Contains(outputText, "super-secret-password") {
		testingT.Errorf("パスワードがログに出ている: %s", outputText)
	}
	if !strings.Contains(outputText, redactedValue) {
		testingT.Errorf("伏せ字が出ていない: %s", outputText)
	}
	if !strings.Contains(outputText, "user-1") {
		testingT.Errorf("機密でないフィールドが消えている: %s", outputText)
	}
}
