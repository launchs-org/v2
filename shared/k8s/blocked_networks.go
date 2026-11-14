package k8s

import (
	"fmt"
	"net"
	"strings"
)

// defaultBlockedNetworks は、プロジェクトの namespace から、インターネットへの出口として許可しない、内部のネットワークの既定。
var defaultBlockedNetworks = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10"}

// DefaultBlockedNetworks は、プロジェクトの namespace から、出ていく通信を許可しない、内部のネットワークの既定を返す。
//
// 内訳: プライベートアドレス(10.0.0.0/8、172.16.0.0/12、192.168.0.0/16。ノード、Pod、Service の CIDR を含む)、
// リンクローカル(169.254.0.0/16。クラウドのメタデータなど)、CGNAT(100.64.0.0/10。Tailscale など)。
//
// 戻り値:
//   - CIDR の一覧(呼び出し側が書き換えても、既定の値は変わらない)。
func DefaultBlockedNetworks() []string {
	// 書き換えられないよう、コピーを返す。
	return append([]string(nil), defaultBlockedNetworks...)
}

// ParseBlockedNetworks は、カンマ区切りの CIDR の一覧を、検査して、NetworkPolicy に使える形にする。
//
// 規則:
//   - 空文字列(または空白だけ)なら、既定(DefaultBlockedNetworks)を返す。何も除かない設定は、できない
//     (隔離を、誤って外さないため)。
//   - 要素の前後の空白は取り除く。空の要素(末尾のカンマなど)は無視する。重複は1つにする。
//   - IPv4 の CIDR だけ(インターネットへの出口は 0.0.0.0/0 なので、それに含まれるものだけを除ける)。
//   - 0.0.0.0/0(全て)は、インターネットへの出口が無くなる上、NetworkPolicy として不正なので、拒否する。
//   - ネットワークの形に直す(例 10.1.2.3/8 は 10.0.0.0/8)。
//
// 引数:
//   - text: カンマ区切りの CIDR(環境変数 EGRESS_BLOCKED_NETWORKS の値)。
//
// 戻り値:
//   - 検査して、ネットワークの形に直した CIDR の一覧(指定の順)。
//   - 読めない、IPv6、0.0.0.0/0 のときのエラー(どの要素が問題かを含む)。
func ParseBlockedNetworks(text string) ([]string, error) {
	// 空なら、既定を使う。
	if strings.TrimSpace(text) == "" {
		return DefaultBlockedNetworks(), nil
	}
	// 要素ごとに検査する。
	seen := map[string]bool{}
	var networks []string
	for _, item := range strings.Split(text, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		ip, network, parseError := net.ParseCIDR(item)
		if parseError != nil {
			return nil, fmt.Errorf("CIDR として読めません: %q", item)
		}
		// IPv4 だけを受け付ける。
		if ip.To4() == nil {
			return nil, fmt.Errorf("IPv4 の CIDR でなければなりません(IPv6 は扱いません): %q", item)
		}
		// 全てを除くと、インターネットへの出口が無くなる。
		if ones, _ := network.Mask.Size(); ones == 0 {
			return nil, fmt.Errorf("0.0.0.0/0(全て)は指定できません: %q", item)
		}
		// ネットワークの形に直し、重複を除く。
		normalized := network.String()
		if !seen[normalized] {
			seen[normalized] = true
			networks = append(networks, normalized)
		}
	}
	// 要素が1つも残らなかったとき(カンマだけなど)は、設定の誤りとする。
	if len(networks) == 0 {
		return nil, fmt.Errorf("CIDR が1つもありません: %q", text)
	}
	return networks, nil
}
