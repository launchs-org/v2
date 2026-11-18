package launchs_stats

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNormalizePath は、パスの整理を確認する。
func TestNormalizePath(testingT *testing.T) {
	for input, want := range map[string]string{
		"":                   "/",
		"/":                  "/",
		"/api/users/123?x=1": "/api/users/:id",
		"/a/550e8400-e29b-41d4-a716-446655440000/b": "/a/:id/b",
		"/blob/0123456789abcdef0123":                "/blob/:id",
		"/static/app.js":                            "/static/app.js",
	} {
		if got := normalizePath(input); got != want {
			testingT.Errorf("%q: 期待 %q、実際 %q", input, want, got)
		}
	}
	long := "/" + string(make([]byte, 300))
	if normalizePath(long) != "(other)" {
		testingT.Error("長すぎるパスは (other) のはず")
	}
}

// TestTopCounterBounded は、枠の数でメモリが決まり、多いキーが残ることを確認する。
func TestTopCounterBounded(testingT *testing.T) {
	counter := newTopCounter(10)
	for round := 0; round < 2000; round++ {
		counter.add("hot")
	}
	for index := 0; index < 5000; index++ {
		counter.add("cold-" + string(rune('a'+index%26)) + string(rune('a'+index/26%26)) + string(rune('a'+index/676%26)))
	}
	if len(counter.counts) > 10 {
		testingT.Fatalf("枠を超えた: %d", len(counter.counts))
	}
	if top := counter.top(1); top[0].Key != "hot" {
		testingT.Errorf("hot が先頭のはず: %v", top)
	}
}

// TestStatsRecords は、アクセスが集計され、応答がそのまま通ることを確認する。
func TestStatsRecords(testingT *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
		_, _ = rw.Write([]byte("hello"))
	})
	handler, newError := New(ctx, next, &Config{IngressID: "ingress-t", IntervalSeconds: 3600, ClientIPHeader: "X-Real-Ip"}, "t")
	if newError != nil {
		testingT.Fatal(newError)
	}
	recorded := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/items/42?q=1", nil)
	request.Header.Set("X-Real-Ip", "203.0.113.5")
	handler.ServeHTTP(recorded, request)
	if recorded.Code != 404 || recorded.Body.String() != "hello" {
		testingT.Fatalf("応答が変わった: %d %q", recorded.Code, recorded.Body.String())
	}
	current := handler.(*Stats).collector.current
	if current.count != 1 || current.status[3] != 1 || current.methods["GET"] != 1 || current.bytes != 5 {
		testingT.Errorf("集計が合わない: %+v", current)
	}
	if current.paths.top(1)[0].Key != "/items/:id" || current.clients.top(1)[0].Key != "203.0.113.5" || len(current.uniqueSeen) != 1 {
		testingT.Errorf("パスとクライアントが合わない: %+v", current)
	}
}

// TestOneCollectorPerIngress は、同じ ID なら、collector が 1 つ(出力が二重にならない)ことを確認する。
func TestOneCollectorPerIngress(testingT *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := acquireCollector(ctx, "ingress-same", time.Hour)
	second := acquireCollector(ctx, "ingress-same", time.Hour)
	if first != second {
		testingT.Error("同じ collector のはず")
	}
}

// TestEmptyConfigPassesThrough は、ID が空なら、素通しになることを確認する。
func TestEmptyConfigPassesThrough(testingT *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	handler, _ := New(context.Background(), next, &Config{}, "t")
	if _, isStats := handler.(*Stats); isStats {
		testingT.Error("素通しのはず")
	}
}
