// Package ingresshost は、ingress に払い出すホスト名(ランダムな単語をつなげたサブドメイン)を作る。
package ingresshost

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// adjectives は、1つ目の単語(形容詞)の候補。
var adjectives = []string{
	"brave", "calm", "clever", "cosmic", "cozy", "crisp", "eager", "fancy", "fuzzy", "gentle",
	"giant", "glad", "golden", "happy", "humble", "jolly", "kind", "lively", "lucky", "mellow",
	"merry", "mighty", "noble", "polite", "proud", "quick", "quiet", "rapid", "shiny", "silent",
	"smart", "snowy", "solid", "speedy", "sunny", "swift", "tidy", "tiny", "vivid", "witty",
}

// colors は、2つ目の単語(色)の候補。
var colors = []string{
	"amber", "azure", "beige", "black", "blue", "bronze", "coral", "cream", "crimson", "cyan",
	"gold", "gray", "green", "indigo", "ivory", "jade", "lemon", "lilac", "lime", "maroon",
	"mint", "navy", "olive", "orange", "peach", "pink", "plum", "purple", "red", "rose",
	"ruby", "sand", "silver", "teal", "violet", "white", "yellow",
}

// animals は、3つ目の単語(動物)の候補。
var animals = []string{
	"badger", "bear", "bee", "bison", "cat", "cobra", "crane", "deer", "dolphin", "dove",
	"eagle", "falcon", "ferret", "fox", "frog", "gecko", "hawk", "heron", "horse", "koala",
	"lemur", "lion", "lynx", "mole", "moose", "otter", "owl", "panda", "parrot", "pony",
	"puma", "rabbit", "raven", "seal", "shark", "sheep", "sparrow", "swan", "tiger", "turtle",
	"whale", "wolf", "zebra",
}

// maxLabelLength は、DNS の1つのラベル(ドットの間)の長さの上限。
const maxLabelLength = 63

// Generator は、ホスト名を払い出す。ベースドメインと、払い出さないサブドメインを持つ。
type Generator struct {
	// baseDomain は、ホスト名の後ろに付けるドメイン(例: launchs.org)。
	baseDomain string

	// reserved は、払い出さないサブドメインの集合(小文字)。
	reserved map[string]bool
}

// NewGenerator は、Generator を作る。
//
// 引数:
//   - baseDomain: ベースドメイン(例: launchs.org)。空ではいけない。
//   - reservedSubdomains: 払い出さないサブドメイン(大文字小文字は区別しない)。
//
// 戻り値:
//   - 作成した Generator。ベースドメインが空ならエラー。
func NewGenerator(baseDomain string, reservedSubdomains []string) (*Generator, error) {
	// 空のベースドメインでは、ホスト名を作れない。
	trimmedDomain := strings.Trim(strings.ToLower(strings.TrimSpace(baseDomain)), ".")
	if trimmedDomain == "" {
		return nil, errors.New("ベースドメインが空です")
	}
	// 払い出さないサブドメインを、集合にする。
	reserved := map[string]bool{}
	for _, subdomain := range reservedSubdomains {
		reserved[strings.ToLower(strings.TrimSpace(subdomain))] = true
	}
	return &Generator{baseDomain: trimmedDomain, reserved: reserved}, nil
}

// IsReserved は、サブドメインが、払い出さないものかを返す。
//
// 引数:
//   - subdomain: 確かめるサブドメイン(ドメイン全体ではなく、1つ目のラベル)。
//
// 戻り値:
//   - 払い出さないものなら true。
func (generator *Generator) IsReserved(subdomain string) bool {
	// 小文字にして、集合に含まれるか調べる。
	return generator.reserved[strings.ToLower(subdomain)]
}

// Generate は、ホスト名を1つ作る(例: brave-blue-fox.launchs.org)。
//
// 3つの単語(形容詞・色・動物)をランダムに選んでつなげる。つなげた全体、または、どれか1つの単語が、払い出さないサブドメインに
// 含まれていたら、選び直す(単語を指定すると、その単語を含む名前が払い出されなくなる)。
// 全体で一意かどうかは、保存するときに確かめる(重なったら、呼び出し側が、もう一度作る)。
//
// 戻り値:
//   - ホスト名。乱数を得られなければエラー。
func (generator *Generator) Generate() (string, error) {
	// 払い出さないサブドメインに当たったときのために、何度か試す。
	for attempt := 0; attempt < 20; attempt++ {
		// 3つの単語を選ぶ。
		adjective, adjectiveError := pick(adjectives)
		color, colorError := pick(colors)
		animal, animalError := pick(animals)
		if err := errors.Join(adjectiveError, colorError, animalError); err != nil {
			return "", fmt.Errorf("乱数を得られませんでした: %w", err)
		}
		// 単語をハイフンでつなげて、ラベルにする。
		label := adjective + "-" + color + "-" + animal
		// 払い出さないものや、長すぎるものは、選び直す。
		if generator.IsReserved(label) || generator.IsReserved(adjective) || generator.IsReserved(color) || generator.IsReserved(animal) || len(label) > maxLabelLength {
			continue
		}
		return label + "." + generator.baseDomain, nil
	}
	return "", errors.New("使えるホスト名を作れませんでした")
}

// pick は、候補から、暗号論的な乱数で1つ選ぶ。
//
// 引数:
//   - words: 候補(空ではいけない)。
//
// 戻り値:
//   - 選んだ単語。乱数を得られなければエラー。
func pick(words []string) (string, error) {
	// 0 以上 len(words) 未満の乱数を得る。
	index, randomError := rand.Int(rand.Reader, big.NewInt(int64(len(words))))
	if randomError != nil {
		return "", randomError
	}
	return words[index.Int64()], nil
}
