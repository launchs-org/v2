// Package launchs_edge は、Traefik のミドルウェア(Yaegi のプラグイン)。entryPoint 全体に 1 つ入れて、アプリに、
// 本当のクライアント IP と、正しい X-Forwarded-Proto / X-Forwarded-Port を届ける(dev-docs/ingress-plugins.md §3)。
//
// 標準ライブラリだけを使う(Yaegi は、外部パッケージを読み込めない)。全リクエストが通るので、小さく、安全に保つ:
// パニックしても、リクエストを止めず、そのまま通す。
package launchs_edge

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

// Config は、プラグインの設定(Middleware の spec.plugin.launchs-edge)。
type Config struct {
	// TrustedCIDRs は、信頼する接続元の範囲(例: cloudflared の Pod の範囲)。この範囲から来たときだけ、Cf-* などの値を使う。
	// 空、または、全てが不正なら、どの接続元も信頼しない(ヘッダーを、整理するだけ)。
	TrustedCIDRs []string `json:"trustedCIDRs,omitempty"`
}

// CreateConfig は、既定の設定を作る(Traefik が呼ぶ)。
//
// 戻り値:
//   - 空の設定。
func CreateConfig() *Config {
	// 信頼する範囲は、利用者が指定する。既定は、空。
	return &Config{}
}

// Edge は、ミドルウェア本体。
type Edge struct {
	next    http.Handler
	name    string
	trusted []*net.IPNet
}

// New は、ミドルウェアを作る(Traefik が呼ぶ)。
//
// 不正な範囲は、読み飛ばす(設定の誤りで、全てのルートを、壊さないため)。エラーは返さない。
//
// 引数:
//   - ctx: Traefik の context(使わない)。
//   - next: 次のハンドラー。
//   - config: 設定。
//   - name: ミドルウェアの名前。
//
// 戻り値:
//   - ミドルウェア。エラーは、常に nil。
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	// 範囲を読み込む。読めないものは、飛ばす。
	trusted := []*net.IPNet{}
	if config != nil {
		for _, text := range config.TrustedCIDRs {
			_, network, parseError := net.ParseCIDR(strings.TrimSpace(text))
			if parseError == nil {
				trusted = append(trusted, network)
			}
		}
	}
	return &Edge{next: next, name: name, trusted: trusted}, nil
}

// ServeHTTP は、ヘッダーを整えて、次のハンドラーに渡す。
//
// 引数:
//   - responseWriter: 応答の書き込み先。
//   - request: リクエスト(ヘッダーを書き換える)。
func (edge *Edge) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	// ヘッダーの整理は、パニックしても、リクエストを止めない(整理に失敗したら、そのまま通す)。
	edge.rewrite(request)
	edge.next.ServeHTTP(responseWriter, request)
}

// rewrite は、リクエストのヘッダーを整える。パニックは、ここで止める。
//
// 引数:
//   - request: リクエスト。
func (edge *Edge) rewrite(request *http.Request) {
	// パニックしても、呼び出し元に伝えない。
	defer func() { _ = recover() }()
	peer := peerIP(request.RemoteAddr)
	// 接続元が、信頼する範囲の外なら、なりすましの元になる値を捨てて、接続元の IP を、クライアント IP にする。
	if peer == nil || !edge.isTrusted(peer) {
		for _, name := range spoofableHeaders {
			request.Header.Del(name)
		}
		if peer != nil {
			request.Header.Set("X-Real-Ip", peer.String())
		}
		return
	}
	// 信頼する範囲のとき: Cf-Connecting-Ip、無ければ X-Forwarded-For の先頭を、クライアント IP にする。
	client := net.ParseIP(strings.TrimSpace(request.Header.Get("Cf-Connecting-Ip")))
	if client == nil {
		client = net.ParseIP(firstForwardedFor(request.Header.Get("X-Forwarded-For")))
	}
	if client == nil {
		client = peer
	}
	request.Header.Set("X-Real-Ip", client.String())
	// X-Forwarded-For は、「クライアント, (元の続き)」の形にする(元の続きが、クライアント自身なら、足さない)。
	existing := strings.TrimSpace(request.Header.Get("X-Forwarded-For"))
	chain := client.String()
	if existing != "" && firstForwardedFor(existing) != client.String() {
		chain = chain + ", " + existing
	} else if existing != "" {
		chain = existing
	}
	request.Header.Set("X-Forwarded-For", chain)
	// 手前で、HTTPS を終端しているとき(Cf-Visitor のスキーム)は、正しい Proto と Port にする。
	switch visitorScheme(request.Header.Get("Cf-Visitor")) {
	case "https":
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Header.Set("X-Forwarded-Port", "443")
	case "http":
		request.Header.Set("X-Forwarded-Proto", "http")
		request.Header.Set("X-Forwarded-Port", "80")
	}
}

// spoofableHeaders は、信頼する範囲の外から来たら捨てる、Cloudflare が付けるヘッダー(なりすませるため)。
var spoofableHeaders = []string{"Cf-Connecting-Ip", "Cf-Visitor", "True-Client-Ip", "Cf-Ipcountry", "Cf-Ray"}

// isTrusted は、IP が、信頼する範囲に入っているかを返す。
//
// 引数:
//   - ip: 接続元の IP。
//
// 戻り値:
//   - 入っていれば true。
func (edge *Edge) isTrusted(ip net.IP) bool {
	// 範囲を、順に確かめる。
	for _, network := range edge.trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// peerIP は、RemoteAddr(「IP:ポート」)から、IP を取り出す。
//
// 引数:
//   - remoteAddr: 接続元のアドレス。
//
// 戻り値:
//   - IP。読めなければ nil。
func peerIP(remoteAddr string) net.IP {
	// ポートを除く。ポートが無い形にも、対応する。
	host, _, splitError := net.SplitHostPort(remoteAddr)
	if splitError != nil {
		host = remoteAddr
	}
	return net.ParseIP(strings.Trim(host, "[]"))
}

// firstForwardedFor は、X-Forwarded-For の、先頭の要素を返す。
//
// 引数:
//   - value: X-Forwarded-For の値。
//
// 戻り値:
//   - 先頭の要素(前後の空白は除く)。空なら空文字。
func firstForwardedFor(value string) string {
	// カンマで区切った、先頭。
	first, _, _ := strings.Cut(value, ",")
	return strings.TrimSpace(first)
}

// visitorScheme は、Cf-Visitor({"scheme":"https"})から、スキームを取り出す。
//
// 引数:
//   - value: Cf-Visitor の値。
//
// 戻り値:
//   - "https" か "http"。読めない、または、他の値なら空文字。
func visitorScheme(value string) string {
	// JSON として読む(読めなければ、空)。
	var visitor struct {
		Scheme string `json:"scheme"`
	}
	if json.Unmarshal([]byte(value), &visitor) != nil {
		return ""
	}
	scheme := strings.ToLower(visitor.Scheme)
	if scheme == "https" || scheme == "http" {
		return scheme
	}
	return ""
}
