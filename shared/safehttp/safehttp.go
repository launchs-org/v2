// Package safehttp は、利用者が指定した URL に、こちらから接続するときの、SSRF(サーバーを踏み台にした内部への接続)を防ぐ。
//
// 次を守る:
//   - URL の検査: https だけ(開発用に http を許せる)、ユーザー情報なし、ホストあり。
//   - 接続する IP の検査: 名前解決のあと、実際に接続する IP を、接続の直前(net.Dialer.Control)に検査する。
//     DNS の再解決で、すり替えられる(DNS rebinding)ことを防ぐ。
//   - ループバック、プライベート(RFC 1918、IPv6 ULA)、リンクローカル(クラウドのメタデータ 169.254.0.0/16 を含む)、
//     CGNAT、予約済みの範囲には、接続しない。
//   - リダイレクトは辿らない。環境変数のプロキシは使わない。
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Options は、接続の決まりの、ゆるめ方(試験と開発用)。
type Options struct {
	// AllowHTTP が true のとき、http:// も許す(開発用)。
	AllowHTTP bool

	// AllowPrivateNetworks が true のとき、プライベートやループバックの IP にも接続する(試験と開発用。本番では使わない)。
	AllowPrivateNetworks bool
}

// ErrBlockedAddress は、接続を禁止している IP に、つなごうとしたときのエラー。
var ErrBlockedAddress = errors.New("この宛先には、接続できません(内部のネットワークや、予約された範囲です)")

// blockedPrefixes は、IsPrivate などで拾えない、接続を禁止する範囲。
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),   // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF プロトコル用
	netip.MustParsePrefix("192.0.2.0/24"),    // 文書用
	netip.MustParsePrefix("198.18.0.0/15"),   // 試験用
	netip.MustParsePrefix("198.51.100.0/24"), // 文書用
	netip.MustParsePrefix("203.0.113.0/24"),  // 文書用
	netip.MustParsePrefix("240.0.0.0/4"),     // 予約済み(255.255.255.255 を含む)
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64
	netip.MustParsePrefix("2001:db8::/32"),   // 文書用
}

// IsBlockedAddress は、IP が、接続を禁止する範囲かを返す。
//
// 引数:
//   - address: 調べる IP。
//
// 戻り値:
//   - 禁止する範囲なら true。
func IsBlockedAddress(address netip.Addr) bool {
	// IPv4 にマップされた IPv6(::ffff:10.0.0.1 など)は、IPv4 として調べる。
	address = address.Unmap()
	// 未指定、ループバック、プライベート、リンクローカル、マルチキャストは、禁止する。
	if !address.IsValid() || address.IsUnspecified() || address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsInterfaceLocalMulticast() {
		return true
	}
	// そのほかの予約済みの範囲も、禁止する。
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// ValidateURL は、利用者が指定した URL を検査する(登録のときに使う。接続のときの IP の検査は、別に、接続の直前に行う)。
//
// 引数:
//   - rawURL: 検査する URL。
//   - options: 決まりのゆるめ方。
//
// 戻り値:
//   - 解析した URL。不正なときは、利用者に見せられる理由のエラー。
func ValidateURL(rawURL string, options Options) (*url.URL, error) {
	// 解析できて、長すぎないこと。
	if len(rawURL) > 2000 {
		return nil, errors.New("URL が長すぎます(2000 文字まで)")
	}
	parsed, parseError := url.Parse(strings.TrimSpace(rawURL))
	if parseError != nil || parsed.Host == "" {
		return nil, errors.New("URL の形が正しくありません(例: https://example.com/hook)")
	}
	// スキームは、https だけ(開発用に http を許せる)。
	if parsed.Scheme != "https" && !(options.AllowHTTP && parsed.Scheme == "http") {
		return nil, errors.New("URL は、https:// で始めてください")
	}
	// ユーザー情報(user:pass@)は、受け付けない(秘密が、ログや画面に出やすいため)。
	if parsed.User != nil {
		return nil, errors.New("URL に、ユーザー名やパスワードは含められません")
	}
	// localhost と、禁止する範囲の IP を、直接書いたものは、先に断る(名前解決後の検査は、接続のときに行う)。
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" || (!options.AllowPrivateNetworks && (hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".local") || strings.HasSuffix(hostname, ".internal"))) {
		return nil, errors.New("この宛先は、使えません(内部のネットワークです)")
	}
	if address, addressError := netip.ParseAddr(hostname); addressError == nil && !options.AllowPrivateNetworks && IsBlockedAddress(address) {
		return nil, errors.New("この宛先は、使えません(内部のネットワークや、予約された範囲の IP です)")
	}
	// ポートを書くなら、数字であること。
	if port := parsed.Port(); port != "" {
		for _, character := range port {
			if character < '0' || character > '9' {
				return nil, errors.New("ポート番号が正しくありません")
			}
		}
	}
	return parsed, nil
}

// NewClient は、禁止する範囲に接続せず、リダイレクトを辿らない HTTP クライアントを作る。
//
// 引数:
//   - timeout: 1 回のリクエスト全体の待ち時間の上限。
//   - options: 決まりのゆるめ方。
//
// 戻り値:
//   - 作ったクライアント。
func NewClient(timeout time.Duration, options Options) *http.Client {
	// 接続の直前に、実際の IP を検査する(名前解決のあとなので、すり替えられない)。
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_ string, address string, _ syscall.RawConn) error {
			// 試験と開発用に、検査を止められる。
			if options.AllowPrivateNetworks {
				return nil
			}
			host, _, splitError := net.SplitHostPort(address)
			if splitError != nil {
				return ErrBlockedAddress
			}
			parsed, parseError := netip.ParseAddr(host)
			if parseError != nil || IsBlockedAddress(parsed) {
				return fmt.Errorf("%w", ErrBlockedAddress)
			}
			return nil
		},
	}
	transport := &http.Transport{
		// 環境変数のプロキシは、使わない(検査をすり抜けるため)。
		Proxy: nil,
		DialContext: func(requestContext context.Context, network string, address string) (net.Conn, error) {
			return dialer.DialContext(requestContext, network, address)
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// リダイレクトは辿らない(辿った先が、内部の宛先かもしれないため)。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
