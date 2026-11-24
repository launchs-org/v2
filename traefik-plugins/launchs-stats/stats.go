// Package launchs_stats は、ingress ごとのアクセスを集計して、一定の間隔で、標準出力に 1 行の JSON として出す Traefik のプラグイン。
// 標準ライブラリだけを使う(Yaegi で動かすため)。設計は dev-docs/ingress-plugins.md §4。
package launchs_stats

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// logPrefix は、出力の行の頭の印。収集側は、この印の行だけを読む。
	logPrefix = "LAUNCHS_STATS "
	// pathCapacity と clientCapacity は、パスとクライアントを数える枠の数。
	pathCapacity   = 100
	clientCapacity = 100
	// topLimit は、1 回の出力に入れる、パスとクライアントの数。
	topLimit = 50
	// maxRequestEntries は、1 回の出力に入れる、1 リクエストごとの記録の数の上限。超えた分は、件数だけ数える。
	maxRequestEntries = 200
	// maxURLLength は、記録する URL(パスとクエリ)の長さの上限(バイト)。
	maxURLLength = 1000
	// uniqueClientCap は、重複なしのクライアントを数える上限(これを超えたら、数えるのをやめる)。
	uniqueClientCap = 10000
)

// latencyBounds は、応答時間の区切り(ミリ秒)。最後に、それ以上の枠が 1 つ付く。
var latencyBounds = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}

// knownMethods は、そのまま数える HTTP メソッド。それ以外は OTHER にまとめる。
var knownMethods = map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "OPTIONS": true}

// requestEntry は、1 リクエストの記録(アクセスログの 1 行)。
type requestEntry struct {
	// T は、リクエストを受けた時刻(Unix のミリ秒)。
	T int64 `json:"t"`
	// M は、メソッド。
	M string `json:"m"`
	// U は、URL(パスとクエリ。長すぎれば、切る)。
	U string `json:"u"`
	// S は、ステータス(WebSocket など、包まないものは 0)。
	S int `json:"s"`
	// D は、応答にかかった時間(ミリ秒)。
	D float64 `json:"d"`
	// IP は、クライアントの IP(分からなければ空)。
	IP string `json:"ip"`
	// B は、応答の大きさ(バイト)。
	B int64 `json:"b"`
}

// Config は、プラグインの設定。
type Config struct {
	// IngressID は、集計の宛先の ingress の ID(必須)。
	IngressID string `json:"ingressId,omitempty"`
	// IntervalSeconds は、出力の間隔(秒)。既定は 10。
	IntervalSeconds int `json:"intervalSeconds,omitempty"`
	// ClientIPHeader は、クライアントの IP を読むヘッダー。既定は X-Real-Ip。
	ClientIPHeader string `json:"clientIPHeader,omitempty"`
}

// CreateConfig は、既定の設定を作る(Traefik が呼ぶ)。
func CreateConfig() *Config {
	// 間隔とヘッダーの既定を入れる。
	return &Config{IntervalSeconds: 10, ClientIPHeader: "X-Real-Ip"}
}

// bucket は、1 つの間隔の集計。
type bucket struct {
	start      time.Time
	count      int64
	status     [5]int64
	methods    map[string]int64
	bytes      int64
	latency    []int64
	sumMs      float64
	paths      *topCounter
	clients    *topCounter
	uniqueSeen map[string]struct{}
	uniqueFull bool
	// requests は、1 リクエストごとの記録(上限まで)。requestsDropped は、上限を超えて記録しなかった数。
	requests        []requestEntry
	requestsDropped int64
}

// newBucket は、空の集計を作る。
func newBucket(start time.Time) *bucket {
	// 各表を空で用意する。
	return &bucket{
		start:      start,
		methods:    map[string]int64{},
		latency:    make([]int64, len(latencyBounds)+1),
		paths:      newTopCounter(pathCapacity),
		clients:    newTopCounter(clientCapacity),
		uniqueSeen: map[string]struct{}{},
	}
}

// collector は、1 つの ingress ID の集計と、出力のゴルーチンを持つ。
type collector struct {
	mutex    sync.Mutex
	current  *bucket
	interval time.Duration
	done     <-chan struct{}
}

// registry は、ingress ID ごとの collector を、プロセスの中で 1 つに保つ(設定が読み直されても、出力を二重にしない)。
var (
	registryMutex sync.Mutex
	registry      = map[string]*collector{}
)

// acquireCollector は、ingress ID の collector を返す。無い、または止まっていれば、作って出力を始める。
func acquireCollector(ctx context.Context, ingressID string, interval time.Duration) *collector {
	// 登録の表は、排他で触る。
	registryMutex.Lock()
	defer registryMutex.Unlock()
	// 生きているものがあれば、それを使う。
	if existing, found := registry[ingressID]; found {
		select {
		case <-existing.done:
			// 止まっているので、作り直す。
		default:
			return existing
		}
	}
	// 新しく作り、出力のゴルーチンを 1 つだけ始める。
	created := &collector{current: newBucket(time.Now()), interval: interval, done: ctx.Done()}
	registry[ingressID] = created
	go created.run(ingressID)
	return created
}

// run は、間隔ごとに集計を出力し、ctx が終わったら止まる。
func (target *collector) run(ingressID string) {
	// 間隔ごとの時計。
	ticker := time.NewTicker(target.interval)
	defer ticker.Stop()
	for {
		select {
		case <-target.done:
			return
		case <-ticker.C:
			target.flush(ingressID)
		}
	}
}

// flush は、今の集計を取り出して(新しい集計に替えて)、出力する。件数 0 なら、何も出さない。
func (target *collector) flush(ingressID string) {
	// 集計を差し替える(排他の中は、短く)。
	target.mutex.Lock()
	finished := target.current
	target.current = newBucket(time.Now())
	target.mutex.Unlock()
	// 空の間隔は、出さない。
	if finished.count == 0 {
		return
	}
	// JSON にして、1 回の書き込みで、1 行を出す。
	line, marshalError := json.Marshal(finished.snapshot(ingressID, target.interval))
	if marshalError != nil {
		return
	}
	_, _ = os.Stdout.Write(append([]byte(logPrefix), append(line, '\n')...))
}

// snapshot は、出力する形にする。
func (finished *bucket) snapshot(ingressID string, interval time.Duration) map[string]any {
	// 応答時間の区切りを、数と並べる。
	return map[string]any{
		"ingressId":       ingressID,
		"start":           finished.start.UTC().Format(time.RFC3339),
		"intervalSec":     int(interval / time.Second),
		"count":           finished.count,
		"status":          map[string]int64{"1xx": finished.status[0], "2xx": finished.status[1], "3xx": finished.status[2], "4xx": finished.status[3], "5xx": finished.status[4]},
		"methods":         finished.methods,
		"bytes":           finished.bytes,
		"latencyBounds":   latencyBounds,
		"latency":         finished.latency,
		"sumMs":           finished.sumMs,
		"paths":           finished.paths.top(topLimit),
		"clients":         finished.clients.top(topLimit),
		"uniqueClients":   len(finished.uniqueSeen),
		"uniqueCapped":    finished.uniqueFull,
		"requests":        finished.requests,
		"requestsDropped": finished.requestsDropped,
	}
}

// record は、1 回のアクセスを集計に足す。
func (target *collector) record(method, path, rawURL, client string, started time.Time, status int, written int64, elapsed time.Duration) {
	// 集計は排他で触る。
	target.mutex.Lock()
	defer target.mutex.Unlock()
	current := target.current
	current.count++
	// ステータスを、百の位でまとめる。
	if status >= 100 && status < 600 {
		current.status[status/100-1]++
	}
	// メソッドは、知っているものだけ(それ以外は、集計では OTHER)。
	aggregateMethod := method
	if !knownMethods[aggregateMethod] {
		aggregateMethod = "OTHER"
	}
	current.methods[aggregateMethod]++
	current.bytes += written
	// 応答時間を、区切りに入れる。
	milliseconds := float64(elapsed) / float64(time.Millisecond)
	current.sumMs += milliseconds
	slot := len(latencyBounds)
	for index, bound := range latencyBounds {
		if milliseconds <= bound {
			slot = index
			break
		}
	}
	current.latency[slot]++
	// 1 リクエストの記録を残す(上限を超えたら、数だけ数える)。
	if len(current.requests) < maxRequestEntries {
		if len(rawURL) > maxURLLength {
			rawURL = rawURL[:maxURLLength]
		}
		current.requests = append(current.requests, requestEntry{T: started.UnixMilli(), M: method, U: rawURL, S: status, D: float64(int64(milliseconds*100)) / 100, IP: client, B: written})
	} else {
		current.requestsDropped++
	}
	// パスを数える。
	current.paths.add(normalizePath(path))
	// クライアントを数える(IP が分からなければ、数えない)。
	if client != "" {
		current.clients.add(client)
		if _, seen := current.uniqueSeen[client]; !seen {
			if len(current.uniqueSeen) < uniqueClientCap {
				current.uniqueSeen[client] = struct{}{}
			} else {
				current.uniqueFull = true
			}
		}
	}
}

// Stats は、集計のミドルウェア。
type Stats struct {
	next      http.Handler
	collector *collector
	header    string
}

// New は、ミドルウェアを作る(Traefik が呼ぶ)。
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	// 設定が無い、または ID が空なら、何も集計せず、素通しにする。
	if config == nil || config.IngressID == "" {
		return next, nil
	}
	// 間隔とヘッダーの既定を補う。
	seconds := config.IntervalSeconds
	if seconds <= 0 {
		seconds = 10
	}
	header := config.ClientIPHeader
	if header == "" {
		header = "X-Real-Ip"
	}
	return &Stats{next: next, collector: acquireCollector(ctx, config.IngressID, time.Duration(seconds)*time.Second), header: header}, nil
}

// ServeHTTP は、リクエストを下に渡し、終わったら集計に足す。失敗しても、アクセスは止めない。
func (stats *Stats) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// 始めた時刻と、クライアントの IP を取る。
	started := time.Now()
	client := stats.clientOf(req)
	// WebSocket と、イベントストリームは、包まない(Yaegi の包みが Flush を通さないため)。接続の開始だけ数える。
	if isUpgrade(req) || strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		stats.next.ServeHTTP(rw, req)
		stats.safeRecord(req, client, started, 0, 0, time.Since(started))
		return
	}
	// 応答を包んで、ステータスと大きさを知る。
	wrapped := &recorder{ResponseWriter: rw, status: http.StatusOK}
	stats.next.ServeHTTP(wrapped, req)
	stats.safeRecord(req, client, started, wrapped.status, wrapped.bytes, time.Since(started))
}

// safeRecord は、panic を握りつぶして、集計に足す。
func (stats *Stats) safeRecord(req *http.Request, client string, started time.Time, status int, written int64, elapsed time.Duration) {
	// 集計の失敗で、アクセスを止めない。
	defer func() { _ = recover() }()
	stats.collector.record(req.Method, req.URL.Path, req.URL.RequestURI(), client, started, status, written, elapsed)
}

// clientOf は、設定のヘッダーからクライアントの IP を読む。無ければ、接続元の IP。
func (stats *Stats) clientOf(req *http.Request) string {
	// ヘッダーの値(最初の要素)。
	value := strings.TrimSpace(strings.Split(req.Header.Get(stats.header), ",")[0])
	if net.ParseIP(value) != nil {
		return value
	}
	// 無ければ、接続元。
	host, _, splitError := net.SplitHostPort(req.RemoteAddr)
	if splitError == nil && net.ParseIP(host) != nil {
		return host
	}
	return ""
}

// isUpgrade は、接続の切り替え(WebSocket など)の要求かを返す。
func isUpgrade(req *http.Request) bool {
	// Upgrade ヘッダーがあれば、そう。
	return req.Header.Get("Upgrade") != ""
}

// recorder は、ステータスと書いたバイト数を覚える包み。
type recorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

// WriteHeader は、ステータスを覚えて、下に渡す。
func (rec *recorder) WriteHeader(code int) {
	// 最初の 1 回だけ覚える。
	if !rec.wroteHeader {
		rec.status = code
		rec.wroteHeader = true
	}
	rec.ResponseWriter.WriteHeader(code)
}

// Write は、書いたバイト数を足して、下に渡す。
func (rec *recorder) Write(data []byte) (int, error) {
	// ヘッダーを書いていなければ、200 として記録済み。
	rec.wroteHeader = true
	written, writeError := rec.ResponseWriter.Write(data)
	rec.bytes += int64(written)
	return written, writeError
}

// Unwrap は、下の応答を返す(ResponseController 用)。
func (rec *recorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}
