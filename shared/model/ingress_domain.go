package model

import (
	"crypto/rand"
	"encoding/hex"
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

	// MinSubdomainLength と MaxSubdomainLength は、利用者が選ぶサブドメイン(1 つ目のラベル)の長さの範囲。
	// 上限は、DNS のラベルの上限(63 文字)。
	MinSubdomainLength = 3
	MaxSubdomainLength = 63
)

// subdomainPattern は、サブドメインの形(小文字の英数字とハイフン。先頭と末尾はハイフンでない)。
// Traefik のルールの式(Host(`...`))に埋め込むので、これ以外の文字は、受け付けない。
var subdomainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// IngressDomain は、ingress_domains テーブルの 1 行。ingress に付けた、追加のホスト名 1 つ(<利用者が選んだ名前>.<ベースドメイン>)。
type IngressDomain struct {
	// ID は、ドメインの ID("domain-" + UUID v4)。
	ID string `gorm:"column:id;primaryKey"`

	// IngressID は、付け先の ingress の ID。
	IngressID string `gorm:"column:ingress_id"`

	// Hostname は、ドメイン名(小文字、末尾のドットなし)。
	Hostname string `gorm:"column:hostname"`

	// Token は、所有の確認のための値。ベースドメインの下の名前は、確認が要らない(すぐに確認済みにする)ので、今は使わない。
	// 将来、外部のドメインを受け付けるときの、DNS での確認のために残してある。
	Token string `gorm:"column:token"`

	// VerifiedAt は、有効にした日時。nil は、確認待ち(今は、作るときに、すぐ入れる)。
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

// ValidateSubdomain は、利用者が選ぶサブドメイン(1 つ目のラベル)の形を確かめる。整えたあと(NormalizeHostname)の値を渡す。
//
// 引数:
//   - subdomain: 確かめるサブドメイン(例: myapp)。ドット(.)は含められない。
//
// 戻り値:
//   - 不正なら invalid_request(理由つき)。
func ValidateSubdomain(subdomain string) error {
	// 長さを確かめる。
	if len(subdomain) < MinSubdomainLength || len(subdomain) > MaxSubdomainLength {
		return apperror.New(apperror.KindInvalidRequest, "名前は、3 文字以上 63 文字以内で入力してください")
	}
	// 小文字の英数字とハイフンだけで、先頭と末尾がハイフンでないことを確かめる(ドットや記号は、ここで弾く)。
	if !subdomainPattern.MatchString(subdomain) {
		return apperror.New(apperror.KindInvalidRequest, "名前は、小文字の英数字とハイフンだけで、先頭と末尾はハイフン以外にしてください")
	}
	// xn-- で始まるものは、国際化ドメイン(punycode)の形なので、受け付けない。
	if strings.HasPrefix(subdomain, "xn--") {
		return apperror.New(apperror.KindInvalidRequest, "xn-- で始まる名前は、使えません")
	}
	return nil
}
