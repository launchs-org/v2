package model

import (
	"strings"
	"testing"
)

// TestValidateCustomHostname は、カスタムドメインとして、受け付ける形と、受け付けない形を確かめる。
func TestValidateCustomHostname(testingT *testing.T) {
	// 受け付ける形。
	for _, valid := range []string{"example.com", "app.example.co.jp", "a-b.example.com", "xn--eckwd4c7c.jp"} {
		if validateError := ValidateCustomHostname(valid, "shconn.org"); validateError != nil {
			testingT.Errorf("%q は受け付けるはず: %v", valid, validateError)
		}
	}
	// 受け付けない形。
	tooLong := strings.Repeat("a", 64) + ".example.com"
	for name, invalid := range map[string]string{
		"空": "", "ドット無し": "localhost", "IP": "192.0.2.1", "ワイルドカード": "*.example.com", "大文字": "Example.com",
		"先頭のハイフン": "-a.example.com", "アンダースコア": "a_b.example.com", "空のラベル": "a..example.com",
		"数字だけの TLD": "example.123", "ラベルが長すぎる": tooLong, "ベースドメイン": "shconn.org", "ベースドメインの下": "app.shconn.org",
		"引用符": "a`b.example.com", "空白": "a b.example.com",
	} {
		if validateError := ValidateCustomHostname(invalid, "shconn.org"); validateError == nil {
			testingT.Errorf("%s(%q)は拒否するはず", name, invalid)
		}
	}
	// ベースドメインに似ているだけのドメインは、受け付ける。
	if validateError := ValidateCustomHostname("notshconn.org", "shconn.org"); validateError != nil {
		testingT.Errorf("ベースドメインの末尾が同じだけのドメインは、受け付けるはず: %v", validateError)
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
