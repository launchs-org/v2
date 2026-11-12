package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"launchs/backend/src/middlewares"
	"launchs/shared/apperror"
	"launchs/shared/logger"
)

// TestServerHealthCheck は、組み立てたサーバーで、GET /healthz が 200 と {"status": "ok"} を返し、
// レスポンスにリクエスト ID が付くことを確認する。
func TestServerHealthCheck(testingT *testing.T) {
	var logOutput bytes.Buffer
	echoServer := New(logger.New(&logOutput, "backend", slog.LevelInfo), Dependencies{})

	recorder := httptest.NewRecorder()
	echoServer.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		testingT.Errorf("ステータスコードが違う: 期待 %d、実際 %d", http.StatusOK, recorder.Code)
	}
	if strings.TrimSpace(recorder.Body.String()) != `{"status":"ok"}` {
		testingT.Errorf("ボディが違う: %s", recorder.Body.String())
	}
	if recorder.Header().Get(middlewares.RequestIDHeaderName) == "" {
		testingT.Errorf("レスポンスにリクエスト ID が付いていない")
	}
}

// TestServerKeepsIncomingRequestID は、リクエストに X-Request-Id が付いているとき、
// その値がレスポンスとアクセスログに使われ、付いていないときは作られることを確認する。
func TestServerKeepsIncomingRequestID(testingT *testing.T) {
	var logOutput bytes.Buffer
	echoServer := New(logger.New(&logOutput, "backend", slog.LevelInfo), Dependencies{})

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set(middlewares.RequestIDHeaderName, "request-from-client")
	recorder := httptest.NewRecorder()
	echoServer.ServeHTTP(recorder, request)

	if actualRequestID := recorder.Header().Get(middlewares.RequestIDHeaderName); actualRequestID != "request-from-client" {
		testingT.Errorf("リクエスト ID が引き継がれていない: %q", actualRequestID)
	}
	if !strings.Contains(logOutput.String(), `"request_id":"request-from-client"`) {
		testingT.Errorf("アクセスログにリクエスト ID が出ていない: %s", logOutput.String())
	}
}

// TestServerReturnsErrorForUnknownPath は、存在しないパスが、404 と、共通ルールの形式の
// not_found のボディを返すことを確認する。
func TestServerReturnsErrorForUnknownPath(testingT *testing.T) {
	var logOutput bytes.Buffer
	echoServer := New(logger.New(&logOutput, "backend", slog.LevelInfo), Dependencies{})

	recorder := httptest.NewRecorder()
	echoServer.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/no-such-path", nil))

	if recorder.Code != http.StatusNotFound {
		testingT.Errorf("ステータスコードが違う: 期待 %d、実際 %d", http.StatusNotFound, recorder.Code)
	}
	var actualResponse apperror.ErrorResponse
	if unmarshalError := json.Unmarshal(recorder.Body.Bytes(), &actualResponse); unmarshalError != nil {
		testingT.Fatalf("ボディが JSON ではない: %v\nボディ: %s", unmarshalError, recorder.Body.String())
	}
	if actualResponse.Error.Code != "not_found" {
		testingT.Errorf("code が違う: 期待 %q、実際 %q", "not_found", actualResponse.Error.Code)
	}
}
