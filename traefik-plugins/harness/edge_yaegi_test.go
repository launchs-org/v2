package harness

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestEdgeUnderYaegi は、launchs-edge が、Yaegi で読み込めて、信頼する接続元と、しない接続元で、正しく動くことを確認する。
func TestEdgeUnderYaegi(testingT *testing.T) {
	plugin, loadError := Load("../launchs-edge", "github.com/launchs-org/launchs-edge")
	if loadError != nil {
		testingT.Fatalf("Yaegi で読み込めるはず: %v", loadError)
	}
	var received http.Header
	next := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) { received = request.Header.Clone() })
	handler, newError := plugin.New(next, map[string]any{"TrustedCIDRs": []string{"10.240.0.0/16"}})
	if newError != nil {
		testingT.Fatalf("ミドルウェアを作れるはず: %v", newError)
	}
	send := func(remoteAddr string, headers map[string]string) http.Header {
		request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		request.RemoteAddr = remoteAddr
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		handler.ServeHTTP(httptest.NewRecorder(), request)
		return received
	}
	testingT.Run("信頼する接続元: 本当の IP と https になる", func(subTest *testing.T) {
		headers := send("10.240.3.131:51000", map[string]string{"Cf-Connecting-Ip": "124.145.4.39", "Cf-Visitor": `{"scheme":"https"}`, "X-Forwarded-For": "10.240.3.131", "X-Forwarded-Port": "80", "X-Forwarded-Proto": "http"})
		if headers.Get("X-Real-Ip") != "124.145.4.39" || headers.Get("X-Forwarded-For") != "124.145.4.39, 10.240.3.131" || headers.Get("X-Forwarded-Proto") != "https" || headers.Get("X-Forwarded-Port") != "443" {
			subTest.Errorf("Yaegi 上でも、同じ結果のはず: %v", headers)
		}
	})
	testingT.Run("信頼しない接続元: Cf-* を捨てる", func(subTest *testing.T) {
		headers := send("192.168.100.50:5555", map[string]string{"Cf-Connecting-Ip": "6.6.6.6", "Cf-Visitor": `{"scheme":"https"}`})
		if headers.Get("Cf-Connecting-Ip") != "" || headers.Get("X-Real-Ip") != "192.168.100.50" {
			subTest.Errorf("捨てて、接続元の IP のはず: %v", headers)
		}
	})
}
