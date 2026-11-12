package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"launchs/shared/apperror"
	"launchs/shared/logger"
)

// newServerWithAccessLog は、リクエスト ID とアクセスログのミドルウェアを持ち、
// 成功するルール(GET /ok)と、404 になるルート(GET /missing)を持つサーバーを作る(テスト用)。
//
// 引数:
//   - appLogger: アクセスログとエラーハンドラーが使うロガー。
//
// 戻り値:
//   - 組み立てた Echo のサーバー。
func newServerWithAccessLog(appLogger *slog.Logger) *echo.Echo {
	echoServer := echo.NewWithConfig(echo.Config{HTTPErrorHandler: NewErrorHandler(appLogger)})
	echoServer.Use(middleware.RequestID())
	echoServer.Use(NewAccessLogMiddleware(appLogger))
	echoServer.GET("/ok", func(echoContext *echo.Context) error {
		return echoContext.String(http.StatusOK, "ok")
	})
	echoServer.GET("/missing", func(echoContext *echo.Context) error {
		return apperror.New(apperror.KindNotFound, "無い")
	})
	return echoServer
}

// parseSingleLogLine は、ログの出力が、JSON の1行だけであることを確認して、その中身を返す(テスト用)。
//
// 引数:
//   - testingT: テストの実行を表す値。
//   - logOutput: ロガーの出力。
//
// 戻り値:
//   - 1行の JSON を読み込んだ中身。
func parseSingleLogLine(testingT *testing.T, logOutput *bytes.Buffer) map[string]any {
	testingT.Helper()

	logLines := strings.Split(strings.TrimSpace(logOutput.String()), "\n")
	if len(logLines) != 1 {
		testingT.Fatalf("ログは1リクエストにつき1行のはず: %d 行\n%s", len(logLines), logOutput.String())
	}

	var logLine map[string]any
	if unmarshalError := json.Unmarshal([]byte(logLines[0]), &logLine); unmarshalError != nil {
		testingT.Fatalf("ログが JSON ではない: %v\n%s", unmarshalError, logLines[0])
	}
	return logLine
}

// TestAccessLogMiddlewareLogsOneLinePerRequest は、リクエストごとに1行のログが出て、
// リクエスト ID、メソッド、パス、ステータス、処理時間が含まれることを確認する。
func TestAccessLogMiddlewareLogsOneLinePerRequest(testingT *testing.T) {
	testCases := []struct {
		name           string
		path           string
		expectedStatus int
	}{
		{"成功したリクエスト", "/ok", http.StatusOK},
		{"エラーになったリクエストも、最終的なステータスを出す", "/missing", http.StatusNotFound},
	}

	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			var logOutput bytes.Buffer
			echoServer := newServerWithAccessLog(logger.New(&logOutput, "backend", slog.LevelInfo))

			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			request.Header.Set(requestIDHeaderName, "request-abc")
			echoServer.ServeHTTP(httptest.NewRecorder(), request)

			logLine := parseSingleLogLine(subTest, &logOutput)
			if logLine["request_id"] != "request-abc" {
				subTest.Errorf("request_id が違う: %v", logLine["request_id"])
			}
			if logLine["method"] != http.MethodGet {
				subTest.Errorf("method が違う: %v", logLine["method"])
			}
			if logLine["path"] != testCase.path {
				subTest.Errorf("path が違う: 期待 %q、実際 %v", testCase.path, logLine["path"])
			}
			if logLine["status"] != float64(testCase.expectedStatus) {
				subTest.Errorf("status が違う: 期待 %d、実際 %v", testCase.expectedStatus, logLine["status"])
			}
			if _, hasLatency := logLine["latency_ms"]; !hasLatency {
				subTest.Errorf("latency_ms が無い: %v", logLine)
			}
		})
	}
}
