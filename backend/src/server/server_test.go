package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"launchs/shared/apperror"
	"launchs/shared/httpmiddleware"
	"launchs/shared/logger"
)

// TestServerHealthCheck は、組み立てたサーバーで、GET /healthz が 200 と {"status": "ok"} を返し、
// レスポンスにリクエスト ID が付くことを確認する。
func TestServerHealthCheck(testingT *testing.T) {
	// アクセスログの出力先を、メモリ上に用意する。
	var logOutput bytes.Buffer
	// データベース無しでサーバーを組み立てる。
	echoServer := New(logger.New(&logOutput, "backend", slog.LevelInfo), Dependencies{})

	// レスポンスを受け取る記録用の入れ物を作る。
	recorder := httptest.NewRecorder()
	// ヘルスチェックを呼ぶ。
	echoServer.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	// ステータスコードが 200 であることを確認する。
	if recorder.Code != http.StatusOK {
		testingT.Errorf("ステータスコードが違う: 期待 %d、実際 %d", http.StatusOK, recorder.Code)
	}
	// ボディが期待どおりの JSON であることを確認する。
	if strings.TrimSpace(recorder.Body.String()) != `{"status":"ok"}` {
		testingT.Errorf("ボディが違う: %s", recorder.Body.String())
	}
	// リクエスト ID がレスポンスに付いていることを確認する。
	if recorder.Header().Get(httpmiddleware.RequestIDHeaderName) == "" {
		testingT.Errorf("レスポンスにリクエスト ID が付いていない")
	}
}

// TestServerKeepsIncomingRequestID は、リクエストに X-Request-Id が付いているとき、
// その値がレスポンスとアクセスログに使われ、付いていないときは作られることを確認する。
func TestServerKeepsIncomingRequestID(testingT *testing.T) {
	// アクセスログの出力先を、メモリ上に用意する。
	var logOutput bytes.Buffer
	// サーバーを組み立てる。
	echoServer := New(logger.New(&logOutput, "backend", slog.LevelInfo), Dependencies{})

	// リクエストを作る。
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	// クライアントが決めたリクエスト ID を付ける。
	request.Header.Set(httpmiddleware.RequestIDHeaderName, "request-from-client")
	// レスポンスの記録用の入れ物を作る。
	recorder := httptest.NewRecorder()
	// サーバーに処理させる。
	echoServer.ServeHTTP(recorder, request)

	// レスポンスのリクエスト ID が、クライアントの値のままであることを確認する。
	if actualRequestID := recorder.Header().Get(httpmiddleware.RequestIDHeaderName); actualRequestID != "request-from-client" {
		testingT.Errorf("リクエスト ID が引き継がれていない: %q", actualRequestID)
	}
	// アクセスログにも、同じ ID が出ていることを確認する。
	if !strings.Contains(logOutput.String(), `"request_id":"request-from-client"`) {
		testingT.Errorf("アクセスログにリクエスト ID が出ていない: %s", logOutput.String())
	}
}

// TestServerReturnsErrorForUnknownPath は、存在しないパスが、404 と、共通ルールの形式の
// not_found のボディを返すことを確認する。
func TestServerReturnsErrorForUnknownPath(testingT *testing.T) {
	// アクセスログの出力先を、メモリ上に用意する。
	var logOutput bytes.Buffer
	// サーバーを組み立てる。
	echoServer := New(logger.New(&logOutput, "backend", slog.LevelInfo), Dependencies{})

	// レスポンスの記録用の入れ物を作る。
	recorder := httptest.NewRecorder()
	// 登録していないパスを呼ぶ。
	echoServer.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/no-such-path", nil))

	// ステータスコードが 404 であることを確認する。
	if recorder.Code != http.StatusNotFound {
		testingT.Errorf("ステータスコードが違う: 期待 %d、実際 %d", http.StatusNotFound, recorder.Code)
	}
	// ボディを読み込む変数を用意する。
	var actualResponse apperror.ErrorResponse
	// ボディを読み込む。共通の形式の JSON でなければ、以降の確認ができないので止める。
	if unmarshalError := json.Unmarshal(recorder.Body.Bytes(), &actualResponse); unmarshalError != nil {
		testingT.Fatalf("ボディが JSON ではない: %v\nボディ: %s", unmarshalError, recorder.Body.String())
	}
	// code が not_found であることを確認する。
	if actualResponse.Error.Code != "not_found" {
		testingT.Errorf("code が違う: 期待 %q、実際 %q", "not_found", actualResponse.Error.Code)
	}
}
