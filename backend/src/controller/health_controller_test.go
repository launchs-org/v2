package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

// TestHealthControllerCheck は、ヘルスチェックが、200 と {"status": "ok"} を JSON で返すことを確認する。
func TestHealthControllerCheck(testingT *testing.T) {
	// テスト用の Echo のサーバーを作る。
	echoServer := echo.New()
	// 検証対象のコントローラーを作る。
	healthController := NewHealthController()
	// ヘルスチェックのルートを登録する。
	echoServer.GET("/healthz", healthController.Check)

	// ヘルスチェックを呼ぶリクエストを作る。
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	// レスポンスを受け取る記録用の入れ物を作る。
	recorder := httptest.NewRecorder()

	// サーバーにリクエストを渡し、ネットワークを通さずに処理させる。
	echoServer.ServeHTTP(recorder, request)

	// ステータスコードが 200 であることを確認する。
	if recorder.Code != http.StatusOK {
		testingT.Errorf("ステータスコードが違う: 期待 %d、実際 %d", http.StatusOK, recorder.Code)
	}
	// Content-Type が JSON であることを確認する。
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		testingT.Errorf("Content-Type が JSON ではない: %q", contentType)
	}

	// ボディを読み込むための変数を用意する。
	var actualResponse HealthCheckResponse
	// ボディを構造体に読み込む。JSON として読めなければ、以降の確認ができないので止める。
	if unmarshalError := json.Unmarshal(recorder.Body.Bytes(), &actualResponse); unmarshalError != nil {
		testingT.Fatalf("ボディが JSON ではない: %v\nボディ: %s", unmarshalError, recorder.Body.String())
	}
	// status が ok であることを確認する。
	if actualResponse.Status != "ok" {
		testingT.Errorf("status が違う: 期待 %q、実際 %q", "ok", actualResponse.Status)
	}
}
