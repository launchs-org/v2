package ingresshost

import (
	"regexp"
	"strings"
	"testing"
)

// TestGenerate は、ホスト名が、3つの単語とベースドメインの形で、払い出さない名前を含まないことを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestGenerate(testing *testing.T) {
	// 動物の1つ("fox")と、全体の名前を、払い出さないものにする。
	generator, createError := NewGenerator(" Launchs.ORG. ", []string{"fox", "WWW", "brave-blue-cat"})
	if createError != nil {
		testing.Fatalf("作れるはず: %v", createError)
	}
	// ホスト名の形。
	pattern := regexp.MustCompile(`^[a-z]+-[a-z]+-[a-z]+\.launchs\.org$`)
	// 多めに払い出して、形と、払い出さない名前を確かめる。
	seen := map[string]bool{}
	for count := 0; count < 3000; count++ {
		host, generateError := generator.Generate()
		if generateError != nil {
			testing.Fatalf("払い出せるはず: %v", generateError)
		}
		if !pattern.MatchString(host) {
			testing.Fatalf("形が違う: %s", host)
		}
		if strings.Contains(host, "-fox.") || strings.HasPrefix(host, "brave-blue-cat.") {
			testing.Fatalf("払い出さない名前: %s", host)
		}
		seen[host] = true
	}
	// 3000回で、ほとんどが重ならないこと(十分に散らばる)。
	if len(seen) < 2500 {
		testing.Errorf("重なりが多すぎる: %d", len(seen))
	}
	// 払い出さないかの判定は、大文字小文字を区別しない。
	if !generator.IsReserved("Www") || generator.IsReserved("api") {
		testing.Error("判定が違う")
	}
	// ベースドメインが空なら、作れない。
	if _, emptyError := NewGenerator(" . ", nil); emptyError == nil {
		testing.Error("空のドメインは拒否するはず")
	}
}
