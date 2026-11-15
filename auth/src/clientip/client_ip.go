// Package clientip は、リクエストの、元のクライアントの IP を決める。
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Resolver は、リクエストから、クライアントの IP を決める。
//
// 信頼できるプロキシ(Traefik など)から来たリクエストだけ、X-Forwarded-For を使う。
// そうでないリクエストの X-Forwarded-For は、偽れるので、無視して、接続元の IP を使う。
type Resolver struct {
	// trustedNetworks は、信頼できるプロキシのネットワーク。
	trustedNetworks []*net.IPNet
}

// NewResolver は、Resolver を作る。
//
// 引数:
//   - trustedCIDRs: 信頼できるプロキシの CIDR(例: 10.0.0.0/8)。空なら、X-Forwarded-For は使わない。
//
// 戻り値:
//   - 作成した Resolver。CIDR の形が不正ならエラー。
func NewResolver(trustedCIDRs []string) (*Resolver, error) {
	// CIDR を、ネットワークにする。
	networks := make([]*net.IPNet, 0, len(trustedCIDRs))
	for _, cidr := range trustedCIDRs {
		_, network, parseError := net.ParseCIDR(strings.TrimSpace(cidr))
		if parseError != nil {
			return nil, fmt.Errorf("信頼するプロキシの CIDR %q が正しくありません: %w", cidr, parseError)
		}
		networks = append(networks, network)
	}
	return &Resolver{trustedNetworks: networks}, nil
}

// isTrusted は、IP が、信頼できるプロキシのものかを返す。
//
// 引数:
//   - ip: 調べる IP。
//
// 戻り値:
//   - 信頼できるプロキシなら true。
func (resolver *Resolver) isTrusted(ip net.IP) bool {
	// どれかのネットワークに含まれるか。
	for _, network := range resolver.trustedNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// Resolve は、クライアントの IP を返す。
//
// 接続元が信頼できるプロキシなら、X-Forwarded-For を、右(プロキシに近い側)から見て、最初の、信頼できない IP を使う
// (左側は、クライアントが自由に書けるので、信じない)。全て信頼できる IP なら、一番左を使う。
// 読めないときは、接続元の IP を使う。
//
// 引数:
//   - request: HTTP のリクエスト。
//
// 戻り値:
//   - クライアントの IP の文字列。
func (resolver *Resolver) Resolve(request *http.Request) string {
	// 接続元の IP を取る。
	host, _, splitError := net.SplitHostPort(request.RemoteAddr)
	if splitError != nil {
		host = request.RemoteAddr
	}
	remoteIP := net.ParseIP(host)
	if remoteIP == nil || !resolver.isTrusted(remoteIP) {
		return host
	}
	// 信頼できるプロキシ経由なので、X-Forwarded-For を、右から見ていく。
	forwarded := strings.Split(request.Header.Get("X-Forwarded-For"), ",")
	for index := len(forwarded) - 1; index >= 0; index-- {
		candidate := net.ParseIP(strings.TrimSpace(forwarded[index]))
		if candidate == nil {
			return host
		}
		if !resolver.isTrusted(candidate) || index == 0 {
			return candidate.String()
		}
	}
	return host
}
