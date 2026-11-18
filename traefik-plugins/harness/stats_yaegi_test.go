package harness

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestStatsUnderYaegi は、launchs-stats を、本物の Yaegi で読み込み、応答が通ることと、1 行の出力が出ることを確認する。
func TestStatsUnderYaegi(testingT *testing.T) {
	// 標準出力を、パイプに差し替えて、出力を読めるようにする。
	reader, writer, pipeError := os.Pipe()
	if pipeError != nil {
		testingT.Fatal(pipeError)
	}
	originalStdout := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = originalStdout }()

	// プラグインを読み込む。
	plugin, loadError := Load("../launchs-stats", "github.com/launchs-org/launchs-stats")
	if loadError != nil {
		testingT.Fatal(loadError)
	}
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusCreated)
		_, _ = rw.Write([]byte("ok"))
	})
	handler, newError := plugin.New(next, map[string]any{"IngressID": "ingress-yaegi", "IntervalSeconds": 1, "ClientIPHeader": "X-Real-Ip"})
	if newError != nil {
		testingT.Fatal(newError)
	}
	// アクセスを 3 回行う。
	for range 3 {
		request := httptest.NewRequest(http.MethodPost, "/orders/12345", nil)
		request.Header.Set("X-Real-Ip", "198.51.100.7")
		recorded := httptest.NewRecorder()
		handler.ServeHTTP(recorded, request)
		if recorded.Code != http.StatusCreated || recorded.Body.String() != "ok" {
			testingT.Fatalf("応答が変わった: %d %q", recorded.Code, recorded.Body.String())
		}
	}
	// 出力の行を、時間切れ付きで待つ。
	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(reader).ReadString('\n')
		lines <- line
	}()
	select {
	case line := <-lines:
		for _, want := range []string{"LAUNCHS_STATS ", `"ingressId":"ingress-yaegi"`, `"count":3`, `"2xx":3`, `"POST":3`, `"/orders/:id"`, `"198.51.100.7"`, `"bytes":6`} {
			if !strings.Contains(line, want) {
				testingT.Errorf("出力に %q が無い: %s", want, line)
			}
		}
	case <-time.After(5 * time.Second):
		testingT.Fatal("出力が出ない")
	}
}

// TestStatsUpgradePassesThrough は、接続の切り替え(WebSocket)の要求で、Hijack が下まで通ることを、本物のサーバーで確認する。
func TestStatsUpgradePassesThrough(testingT *testing.T) {
	plugin, loadError := Load("../launchs-stats", "github.com/launchs-org/launchs-stats")
	if loadError != nil {
		testingT.Fatal(loadError)
	}
	hijacked := make(chan bool, 1)
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		_, isHijacker := rw.(http.Hijacker)
		hijacked <- isHijacker
	})
	handler, newError := plugin.New(next, map[string]any{"IngressID": "ingress-ws", "IntervalSeconds": 3600})
	if newError != nil {
		testingT.Fatal(newError)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	response, requestError := http.DefaultClient.Do(request)
	if requestError != nil {
		testingT.Fatal(requestError)
	}
	_ = response.Body.Close()
	if !<-hijacked {
		testingT.Error("Upgrade の要求では、Hijacker のまま通るはず")
	}
}
