package service

import (
	"context"
	"net"
	"strings"
	"time"

	"launchs/shared/model"
)

// dnsLookupTimeout は、DNS の問い合わせ 1 回の待ち時間の上限。
const dnsLookupTimeout = 5 * time.Second

// DNSDomainVerifier は、実際の DNS を引いて、ドメインの所有を確かめる。
type DNSDomainVerifier struct {
	// resolver は、DNS を引くもの。
	resolver *net.Resolver
}

// NewDNSDomainVerifier は、システムの DNS を使う DNSDomainVerifier を作る。
//
// 戻り値:
//   - 作った verifier。
func NewDNSDomainVerifier() *DNSDomainVerifier {
	// 既定の resolver(システムの設定)を使う。
	return &DNSDomainVerifier{resolver: net.DefaultResolver}
}

// Verify は、次のどちらかが満たされていれば、確認できたとする。
//   - _launchs-challenge.<ドメイン> の TXT レコードに、token がある。
//   - <ドメイン> の CNAME が、cnameTarget(払い出したホスト名)を指す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - hostname: 確かめるドメイン名。
//   - token: TXT に置いてもらう値。
//   - cnameTarget: CNAME の向き先(払い出したホスト名)。
//
// 戻り値:
//   - 確認できたか。できなければ、利用者に見せる理由。
func (verifier *DNSDomainVerifier) Verify(requestContext context.Context, hostname string, token string, cnameTarget string) (bool, string) {
	// 問い合わせの待ち時間に、上限を付ける。
	lookupContext, cancel := context.WithTimeout(requestContext, dnsLookupTimeout)
	defer cancel()
	// TXT を調べる。
	records, txtError := verifier.resolver.LookupTXT(lookupContext, model.IngressDomainChallengePrefix+hostname)
	if txtError == nil {
		for _, record := range records {
			if strings.TrimSpace(record) == token {
				return true, ""
			}
		}
	}
	// CNAME を調べる(末尾のドットを除いて比べる)。
	cname, cnameError := verifier.resolver.LookupCNAME(lookupContext, hostname)
	if cnameError == nil && strings.EqualFold(strings.TrimSuffix(cname, "."), strings.TrimSuffix(cnameTarget, ".")) {
		return true, ""
	}
	// どちらも合わないときの、理由。DNS の反映には時間がかかることを添える。
	return false, "TXT レコード(" + model.IngressDomainChallengePrefix + hostname + ")にも、CNAME(" + cnameTarget + ")にも、確認できる設定が見つかりませんでした。DNS の反映には、数分から数時間かかることがあります。"
}
