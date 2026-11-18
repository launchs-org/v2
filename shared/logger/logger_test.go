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
	// ケースごとの、文字列と、期待するレベル・エラーの有無を並べる。
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

	// ケースを1つずつ、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// テスト対象の ParseLevel を呼ぶ。
			actualLevel, actualError := ParseLevel(testCase.text)

			// エラーの有無が期待と違えば、失敗にする。
			if (actualError != nil) != testCase.expectError {
				subTest.Errorf("エラーの有無が違う: 期待 %v、実際 %v", testCase.expectError, actualError)
			}
			// レベルが期待と違えば、失敗にする。
			if actualLevel != testCase.expectedLevel {
				subTest.Errorf("レベルが違う: 期待 %v、実際 %v", testCase.expectedLevel, actualLevel)
			}
		})
	}
}

// TestNewWritesJSONWithComponent は、ロガーが1行の JSON を出力し、
// component とメッセージ、追加したフィールドが含まれることを確認する。
func TestNewWritesJSONWithComponent(testingT *testing.T) {
	// ログの出力先を、あとで中身を調べられるバッファにする。
	var output bytes.Buffer
	// テスト対象のロガーを、info レベルで作る。
	testLogger := New(&output, "backend", slog.LevelInfo)

	// フィールド付きのログを1件出力する。
	testLogger.Info("プロジェクトを作成した", "project_id", "project-123")

	// 出力された JSON を読み込む先を用意する。
	var logLine map[string]any
	// 出力が JSON として読めなければ、続けられないのでテストを止める。
	if unmarshalError := json.Unmarshal(output.Bytes(), &logLine); unmarshalError != nil {
		testingT.Fatalf("出力が JSON ではない: %v\n出力: %s", unmarshalError, output.String())
	}
	// component が違えば、失敗にする。
	if logLine["component"] != "backend" {
		testingT.Errorf("component が違う: 期待 %q、実際 %v", "backend", logLine["component"])
	}
	// メッセージが違えば、失敗にする。
	if logLine["msg"] != "プロジェクトを作成した" {
		testingT.Errorf("msg が違う: %v", logLine["msg"])
	}
	// レベルの表記が違えば、失敗にする。
	if logLine["level"] != "INFO" {
		testingT.Errorf("level が違う: 期待 %q、実際 %v", "INFO", logLine["level"])
	}
	// 追加したフィールドが出力されていなければ、失敗にする。
	if logLine["project_id"] != "project-123" {
		testingT.Errorf("追加したフィールドが無い: %v", logLine)
	}
}

// TestNewFiltersByLevel は、設定したレベルより低いログは出力されず、
// 同じか高いログは出力されることを確認する。
func TestNewFiltersByLevel(testingT *testing.T) {
	// ログの出力先を、あとで中身を調べられるバッファにする。
	var output bytes.Buffer
	// テスト対象のロガーを、warn レベルで作る。
	testLogger := New(&output, "backend", slog.LevelWarn)

	// 4つのレベルで1件ずつ出力する。warn より低い2つは、出力されないはず。
	testLogger.Debug("デバッグ")
	testLogger.Info("情報")
	testLogger.Warn("警告")
	testLogger.Error("エラー")

	// 出力された文字列を取り出す。
	outputText := output.String()
	// warn より低いレベルのメッセージが、出力に無いことを確認する。
	for _, hiddenMessage := range []string{"デバッグ", "情報"} {
		// 出力されていたら、失敗にする。
		if strings.Contains(outputText, hiddenMessage) {
			testingT.Errorf("低いレベルのログが出力されている: %s", hiddenMessage)
		}
	}
	// warn 以上のレベルのメッセージが、出力に有ることを確認する。
	for _, shownMessage := range []string{"警告", "エラー"} {
		// 出力されていなければ、失敗にする。
		if !strings.Contains(outputText, shownMessage) {
			testingT.Errorf("同じか高いレベルのログが出力されていない: %s", shownMessage)
		}
	}
}
