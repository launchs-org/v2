package model

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"regexp"
	"strings"
	"time"

	"launchs/shared/apperror"
)

const (
	// IngressDomainIDPrefix は、カスタムドメインの ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。
	IngressDomainIDPrefix = "domain-"

	// MaxIngressDomains は、1 つの ingress に付けられるカスタムドメインの数の上限。
	MaxIngressDomains = 5

	// IngressDomainChallengePrefix は、所有の確認に使う DNS の TXT のレコード名の、先頭の部分(ドメインの前に付ける)。
	IngressDomainChallengePrefix = "_launchs-challenge."

	// maxHostnameLength は、ドメイン名全体の長さの上限(DNS の規則)。
	maxHostnameLength = 253
)

// hostnameLabelPattern は、ドメイン名のラベル 1 つの形(小文字の英数字とハイフン。先頭と末尾はハイフンでない。63 文字まで)。
// Traefik のルールの式(Host(`...`))に埋め込むので、これ以外の文字は、受け付けない。
var hostnameLabelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// IngressDomain は、ingress_domains テーブルの 1 行。ingress に付けた、利用者のドメイン 1 つ。
type IngressDomain struct {
	// ID は、ドメインの ID("domain-" + UUID v4)。
	ID string `gorm:"column:id;primaryKey"`

	// IngressID は、付け先の ingress の ID。
	IngressID string `gorm:"column:ingress_id"`

	// Hostname は、ドメイン名(小文字、末尾のドットなし)。
	Hostname string `gorm:"column:hostname"`

	// Token は、所有の確認のための値(DNS の TXT に置いてもらう)。
	Token string `gorm:"column:token"`

	// VerifiedAt は、所有を確かめた日時。nil は、確認待ち。
	VerifiedAt *time.Time `gorm:"column:verified_at"`

	// CreatedAt は、申請した日時。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("ingress_domains")。
func (IngressDomain) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "ingress_domains"
}

// IsVerified は、所有の確認が済んでいるかを返す。
//
// 戻り値:
//   - 確認済みなら true。
func (domain IngressDomain) IsVerified() bool {
	// 確認した日時があれば、確認済み。
	return domain.VerifiedAt != nil
}

// ChallengeName は、所有の確認のために、TXT のレコードを置いてもらう名前を返す。
//
// 戻り値:
//   - 例: _launchs-challenge.app.example.com
func (domain IngressDomain) ChallengeName() string {
	// 接頭辞とドメイン名をつなぐ。
	return IngressDomainChallengePrefix + domain.Hostname
}

// NewIngressDomainToken は、所有の確認のための、推測できない値を作る。
//
// 戻り値:
//   - "launchs-verify=" と、乱数の 16 バイト(16 進数)をつないだ文字列。乱数を得られなければエラー。
func NewIngressDomainToken() (string, error) {
	// 暗号論的な乱数で、16 バイトを作る。
	randomBytes := make([]byte, 16)
	if _, readError := rand.Read(randomBytes); readError != nil {
		return "", apperror.Wrap(apperror.KindInternal, "確認用の値を作れませんでした", readError)
	}
	// 16 進数にして、他の TXT と区別できる接頭辞を付ける。
	return "launchs-verify=" + hex.EncodeToString(randomBytes), nil
}

// NormalizeHostname は、利用者が入力したドメイン名を、前後の空白と末尾のドットを除いて、小文字にする。
//
// 引数:
//   - raw: 入力された文字列。
//
// 戻り値:
//   - 整えたドメイン名。
func NormalizeHostname(raw string) string {
	// 空白を除き、小文字にし、末尾のドット(FQDN の書き方)を除く。
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
}

// ValidateCustomHostname は、カスタムドメインとして付けられるドメイン名かを確かめる。
// 整えたあと(NormalizeHostname)の値を渡す。
//
// 引数:
//   - hostname: 確かめるドメイン名。
//   - baseDomain: 払い出すホスト名のベースドメイン。これ自身と、その下は、付けられない(払い出しと重なるため)。
//
// 戻り値:
//   - 不正なら invalid_request(理由つき)。
func ValidateCustomHostname(hostname string, baseDomain string) error {
	// 空と、長すぎるものを拒否する。
	if hostname == "" || len(hostname) > maxHostnameLength {
		return apperror.New(apperror.KindInvalidRequest, "ドメイン名は、253 文字以内で入力してください")
	}
	// IP アドレスは、ドメイン名ではない。
	if net.ParseIP(hostname) != nil {
		return apperror.New(apperror.KindInvalidRequest, "IP アドレスは、ドメインとして使えません")
	}
	// ワイルドカードは、まだ使えない。
	if strings.Contains(hostname, "*") {
		return apperror.New(apperror.KindInvalidRequest, "ワイルドカード(*)は、まだ使えません")
	}
	// ラベルごとに形を確かめる。ドット 1 つ以上(例: example.com)が要る。
	labels := strings.Split(hostname, ".")
	if len(labels) < 2 {
		return apperror.New(apperror.KindInvalidRequest, "example.com のように、ドットを含むドメイン名を入力してください")
	}
	for _, label := range labels {
		if !hostnameLabelPattern.MatchString(label) {
			return apperror.New(apperror.KindInvalidRequest, "ドメイン名は、小文字の英数字とハイフンを、ドットで区切った形で入力してください(日本語ドメインは、punycode の形で)")
		}
	}
	// 最後のラベル(TLD)が数字だけのものは、IP アドレスと紛らわしいので拒否する。
	if regexp.MustCompile(`^[0-9]+$`).MatchString(labels[len(labels)-1]) {
		return apperror.New(apperror.KindInvalidRequest, "ドメイン名の最後が数字だけのものは、使えません")
	}
	// 払い出しのドメイン(ベースドメイン自身と、その下)は、付けられない。
	if baseDomain != "" && (hostname == baseDomain || strings.HasSuffix(hostname, "."+baseDomain)) {
		return apperror.New(apperror.KindInvalidRequest, baseDomain+" のドメインは、自動で払い出されるので、カスタムドメインには使えません")
	}
	return nil
}
