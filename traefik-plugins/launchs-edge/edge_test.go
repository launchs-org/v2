package launchs_edge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// serve は、プラグインに、リクエストを 1 つ通して、次のハンドラーが受け取ったヘッダーを返す。
//
// 引数:
//   - testingT: テストの状態。
//   - config: プラグインの設定。
//   - remoteAddr: 接続元のアドレス。
//   - headers: リクエストのヘッダー。
//
// 戻り値:
//   - 次のハンドラーが受け取った、リクエストのヘッダー。
func serve(testingT *testing.T, config *Config, remoteAddr string, headers map[string]string) http.Header {
	// 受け取ったヘッダーを覚える、次のハンドラーを作る。
	testingT.Helper()
	var received http.Header
	next := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) { received = request.Header.Clone() })
	handler, newError := New(context.Background(), next, config, "edge")
	if newError != nil {
		testingT.Fatal(newError)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	request.RemoteAddr = remoteAddr
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	handler.ServeHTTP(httptest.NewRecorder(), request)
	return received
}

// TestEdge は、信頼する接続元と、しない接続元での、ヘッダーの整え方を確認する。
func TestEdge(testingT *testing.T) {
	config := &Config{TrustedCIDRs: []string{"10.240.0.0/16", "fd00::/8"}}
	cloudflared := "10.240.3.131:51000"
	testingT.Run("信頼する接続元: Cf-Connecting-Ip が、X-Real-Ip と X-Forwarded-For の先頭になり、Proto と Port が、Cf-Visitor になる", func(subTest *testing.T) {
		received := serve(subTest, config, cloudflared, map[string]string{
			"Cf-Connecting-Ip": "124.145.4.39", "Cf-Visitor": `{"scheme":"https"}`,
			"X-Forwarded-For": "10.240.3.131", "X-Forwarded-Proto": "http", "X-Forwarded-Port": "80", "X-Real-Ip": "10.240.3.131",
		})
		if received.Get("X-Real-Ip") != "124.145.4.39" || received.Get("X-Forwarded-For") != "124.145.4.39, 10.240.3.131" ||
			received.Get("X-Forwarded-Proto") != "https" || received.Get("X-Forwarded-Port") != "443" {
			subTest.Errorf("本当の IP と https のはず: %v", received)
		}
	})
	testingT.Run("信頼する接続元: Cf-Visitor が http なら、Proto は http、Port は 80", func(subTest *testing.T) {
		received := serve(subTest, config, cloudflared, map[string]string{"Cf-Connecting-Ip": "1.2.3.4", "Cf-Visitor": `{"scheme":"http"}`, "X-Forwarded-Port": "80"})
		if received.Get("X-Forwarded-Proto") != "http" || received.Get("X-Forwarded-Port") != "80" {
			subTest.Errorf("http のはず: %v", received)
		}
	})
	testingT.Run("信頼する接続元: Cf-Visitor が無ければ、Proto と Port は、元のまま", func(subTest *testing.T) {
		received := serve(subTest, config, cloudflared, map[string]string{"Cf-Connecting-Ip": "1.2.3.4", "X-Forwarded-Proto": "http", "X-Forwarded-Port": "8000"})
		if received.Get("X-Forwarded-Proto") != "http" || received.Get("X-Forwarded-Port") != "8000" {
			subTest.Errorf("元のままのはず: %v", received)
		}
	})
	testingT.Run("信頼する接続元: Cf-Visitor が壊れていても、止まらず、元のまま", func(subTest *testing.T) {
		received := serve(subTest, config, cloudflared, map[string]string{"Cf-Connecting-Ip": "1.2.3.4", "Cf-Visitor": "{broken", "X-Forwarded-Proto": "http"})
		if received.Get("X-Real-Ip") != "1.2.3.4" || received.Get("X-Forwarded-Proto") != "http" {
			subTest.Errorf("IP だけ整うはず: %v", received)
		}
	})
	testingT.Run("信頼する接続元: Cf-Connecting-Ip が不正なら、X-Forwarded-For の先頭を使う", func(subTest *testing.T) {
		received := serve(subTest, config, cloudflared, map[string]string{"Cf-Connecting-Ip": "not-an-ip", "X-Forwarded-For": "9.9.9.9, 10.240.3.131"})
		if received.Get("X-Real-Ip") != "9.9.9.9" || received.Get("X-Forwarded-For") != "9.9.9.9, 10.240.3.131" {
			subTest.Errorf("先頭のはず: %v", received)
		}
	})
	testingT.Run("信頼する接続元: どちらも無ければ、接続元の IP", func(subTest *testing.T) {
		received := serve(subTest, config, cloudflared, nil)
		if received.Get("X-Real-Ip") != "10.240.3.131" {
			subTest.Errorf("接続元のはず: %v", received)
		}
	})
	testingT.Run("信頼する接続元: IPv6 も扱える", func(subTest *testing.T) {
		received := serve(subTest, config, "[fd00::5]:4000", map[string]string{"Cf-Connecting-Ip": "2001:db8::1"})
		if received.Get("X-Real-Ip") != "2001:db8::1" {
			subTest.Errorf("IPv6 のはず: %v", received)
		}
	})
	testingT.Run("信頼しない接続元: Cf-* と True-Client-Ip を捨て、X-Real-Ip は、接続元の IP になる(なりすませない)", func(subTest *testing.T) {
		received := serve(subTest, config, "192.168.100.50:5555", map[string]string{
			"Cf-Connecting-Ip": "6.6.6.6", "Cf-Visitor": `{"scheme":"https"}`, "True-Client-Ip": "6.6.6.6", "Cf-Ipcountry": "XX", "X-Real-Ip": "6.6.6.6",
		})
		if received.Get("Cf-Connecting-Ip") != "" || received.Get("Cf-Visitor") != "" || received.Get("True-Client-Ip") != "" || received.Get("Cf-Ipcountry") != "" || received.Get("X-Real-Ip") != "192.168.100.50" {
			subTest.Errorf("捨てて、接続元の IP のはず: %v", received)
		}
	})
	testingT.Run("信頼しない接続元は、Proto と Port を、Cf-Visitor で書き換えられない", func(subTest *testing.T) {
		received := serve(subTest, config, "192.168.100.50:5555", map[string]string{"Cf-Visitor": `{"scheme":"https"}`, "X-Forwarded-Proto": "http", "X-Forwarded-Port": "80"})
		if received.Get("X-Forwarded-Proto") != "http" || received.Get("X-Forwarded-Port") != "80" {
			subTest.Errorf("元のままのはず: %v", received)
		}
	})
	testingT.Run("範囲を指定しなければ、誰も信頼しない", func(subTest *testing.T) {
		received := serve(subTest, &Config{}, cloudflared, map[string]string{"Cf-Connecting-Ip": "6.6.6.6"})
		if received.Get("X-Real-Ip") != "10.240.3.131" || received.Get("Cf-Connecting-Ip") != "" {
			subTest.Errorf("信頼しないはず: %v", received)
		}
	})
	testingT.Run("不正な範囲は、飛ばして、正しい範囲は使う。設定が nil でも、止まらない", func(subTest *testing.T) {
		received := serve(subTest, &Config{TrustedCIDRs: []string{"bad", "", "10.240.0.0/16"}}, cloudflared, map[string]string{"Cf-Connecting-Ip": "1.2.3.4"})
		if received.Get("X-Real-Ip") != "1.2.3.4" {
			subTest.Errorf("正しい範囲は、使うはず: %v", received)
		}
		if nilHandler, newError := New(context.Background(), http.NotFoundHandler(), nil, "edge"); newError != nil || nilHandler == nil {
			subTest.Errorf("nil の設定でも、作れるはず: %v", newError)
		}
	})
	testingT.Run("接続元が読めなくても、止まらず、次のハンドラーに渡す", func(subTest *testing.T) {
		received := serve(subTest, config, "garbage", map[string]string{"Cf-Connecting-Ip": "6.6.6.6"})
		if received == nil || received.Get("Cf-Connecting-Ip") != "" {
			subTest.Errorf("通して、Cf-* は、捨てるはず: %v", received)
		}
	})
}
