package model

import (
	"strings"
	"testing"
)

// TestValidateSubdomain は、サブドメインとして、受け付ける形と、受け付けない形を確かめる。
func TestValidateSubdomain(testingT *testing.T) {
	// 受け付ける形。
	for _, valid := range []string{"myapp", "my-app", "app2", "abc", "a-b-c", strings.Repeat("a", 59)} {
		if validateError := ValidateSubdomain(valid); validateError != nil {
			testingT.Errorf("%q は受け付けるはず: %v", valid, validateError)
		}
	}
	// 受け付けない形。
	for name, invalid := range map[string]string{
		"空": "", "短い": "ab", "長い": strings.Repeat("a", 60), "ドット": "a.b.c", "大文字": "MyApp", "先頭のハイフン": "-app",
		"末尾のハイフン": "app-", "アンダースコア": "my_app", "ワイルドカード": "*app", "引用符": "a`b", "空白": "a b",
		"日本語": "あいう", "punycode": "xn--eckwd4c7c",
	} {
		if validateError := ValidateSubdomain(invalid); validateError == nil {
			testingT.Errorf("%s(%q)は拒否するはず", name, invalid)
		}
	}
}

// TestNormalizeHostname は、前後の空白、大文字、末尾のドットが整うことを確かめる。
func TestNormalizeHostname(testingT *testing.T) {
	if got := NormalizeHostname("  App.Example.COM. "); got != "app.example.com" {
		testingT.Errorf("整った形にならない: %q", got)
	}
}

// TestNewIngressDomainToken は、確認用の値が、毎回違い、決まった形であることを確かめる。
func TestNewIngressDomainToken(testingT *testing.T) {
	first, firstError := NewIngressDomainToken()
	second, secondError := NewIngressDomainToken()
	if firstError != nil || secondError != nil {
		testingT.Fatalf("作れるはず: %v %v", firstError, secondError)
	}
	if first == second || !strings.HasPrefix(first, "launchs-verify=") || len(first) != len("launchs-verify=")+32 {
		testingT.Errorf("形が正しくない、または重なった: %q %q", first, second)
	}
}
