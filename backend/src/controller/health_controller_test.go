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
	echoServer := echo.New()
	healthController := NewHealthController()
	echoServer.GET("/healthz", healthController.Check)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	echoServer.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		testingT.Errorf("ステータスコードが違う: 期待 %d、実際 %d", http.StatusOK, recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		testingT.Errorf("Content-Type が JSON ではない: %q", contentType)
	}

	var actualResponse HealthCheckResponse
	if unmarshalError := json.Unmarshal(recorder.Body.Bytes(), &actualResponse); unmarshalError != nil {
		testingT.Fatalf("ボディが JSON ではない: %v\nボディ: %s", unmarshalError, recorder.Body.String())
	}
	if actualResponse.Status != "ok" {
		testingT.Errorf("status が違う: 期待 %q、実際 %q", "ok", actualResponse.Status)
	}
}
