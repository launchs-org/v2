package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestParseLevel は、ログレベルの文字列が正しく変換され、未知の文字列はエラーになることを確認する。
func TestParseLevel(testingT *testing.T) {
	testCases := []struct {
		name          string
		text          string
		expectedLevel slog.Level
		expectError   bool
	}{
		{"debug", "debug", slog.LevelDebug, false},
		{"info", "info", slog.LevelInfo, false},
		{"warn", "warn", slog.LevelWarn, false},
		{"error", "error", slog.LevelError, false},
		{"大文字でも変換できる", "DEBUG", slog.LevelDebug, false},
		{"前後の空白は無視する", "  warn  ", slog.LevelWarn, false},
		{"空文字は info", "", slog.LevelInfo, false},
		{"未知の文字列はエラーで、レベルは info", "verbose", slog.LevelInfo, true},
	}

	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			actualLevel, actualError := ParseLevel(testCase.text)

			if (actualError != nil) != testCase.expectError {
				subTest.Errorf("エラーの有無が違う: 期待 %v、実際 %v", testCase.expectError, actualError)
			}
			if actualLevel != testCase.expectedLevel {
				subTest.Errorf("レベルが違う: 期待 %v、実際 %v", testCase.expectedLevel, actualLevel)
			}
		})
	}
}

// TestNewWritesJSONWithComponent は、ロガーが1行の JSON を出力し、
// component とメッセージ、追加したフィールドが含まれることを確認する。
func TestNewWritesJSONWithComponent(testingT *testing.T) {
	var output bytes.Buffer
	testLogger := New(&output, "backend", slog.LevelInfo)

	testLogger.Info("プロジェクトを作成した", "project_id", "project-123")

	var logLine map[string]any
	if unmarshalError := json.Unmarshal(output.Bytes(), &logLine); unmarshalError != nil {
		testingT.Fatalf("出力が JSON ではない: %v\n出力: %s", unmarshalError, output.String())
	}
	if logLine["component"] != "backend" {
		testingT.Errorf("component が違う: 期待 %q、実際 %v", "backend", logLine["component"])
	}
	if logLine["msg"] != "プロジェクトを作成した" {
		testingT.Errorf("msg が違う: %v", logLine["msg"])
	}
	if logLine["level"] != "INFO" {
		testingT.Errorf("level が違う: 期待 %q、実際 %v", "INFO", logLine["level"])
	}
	if logLine["project_id"] != "project-123" {
		testingT.Errorf("追加したフィールドが無い: %v", logLine)
	}
}

// TestNewFiltersByLevel は、設定したレベルより低いログは出力されず、
// 同じか高いログは出力されることを確認する。
func TestNewFiltersByLevel(testingT *testing.T) {
	var output bytes.Buffer
	testLogger := New(&output, "backend", slog.LevelWarn)

	testLogger.Debug("デバッグ")
	testLogger.Info("情報")
	testLogger.Warn("警告")
	testLogger.Error("エラー")

	outputText := output.String()
	for _, hiddenMessage := range []string{"デバッグ", "情報"} {
		if strings.Contains(outputText, hiddenMessage) {
			testingT.Errorf("低いレベルのログが出力されている: %s", hiddenMessage)
		}
	}
	for _, shownMessage := range []string{"警告", "エラー"} {
		if !strings.Contains(outputText, shownMessage) {
			testingT.Errorf("同じか高いレベルのログが出力されていない: %s", shownMessage)
		}
	}
}
