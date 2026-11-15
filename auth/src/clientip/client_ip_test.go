package clientip

import (
	"net/http"
	"testing"
)

// TestResolve は、信頼できるプロキシ経由のときだけ X-Forwarded-For を使い(右から見て、最初の信頼できない IP)、
// そうでなければ、偽れるので、接続元の IP を使うことを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestResolve(testing *testing.T) {
	// 10.0.0.0/8 を、信頼するプロキシにする。
	resolver, createError := NewResolver([]string{"10.0.0.0/8"})
	if createError != nil {
		testing.Fatal(createError)
	}
	for name, testCase := range map[string]struct{ remoteAddress, forwarded, expected string }{
		"信頼するプロキシ経由: クライアントの IP":        {"10.1.2.3:5000", "203.0.113.5", "203.0.113.5"},
		"プロキシが複数: 右から見て、最初の信頼できない IP":   {"10.1.2.3:5000", "198.51.100.9, 203.0.113.5, 10.9.9.9", "203.0.113.5"},
		"左側に偽の IP を書いても、信じない":           {"10.1.2.3:5000", "1.1.1.1, 203.0.113.5", "203.0.113.5"},
		"全て信頼できる IP なら、一番左":             {"10.1.2.3:5000", "10.5.5.5, 10.6.6.6", "10.5.5.5"},
		"信頼しない接続元の X-Forwarded-For は無視": {"203.0.113.77:5000", "1.2.3.4", "203.0.113.77"},
		"X-Forwarded-For が無ければ、接続元":     {"10.1.2.3:5000", "", "10.1.2.3"},
		"X-Forwarded-For が壊れていれば、接続元":   {"10.1.2.3:5000", "not-an-ip", "10.1.2.3"},
		"ポートの無い接続元":                     {"203.0.113.8", "", "203.0.113.8"},
	} {
		request, _ := http.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = testCase.remoteAddress
		if testCase.forwarded != "" {
			request.Header.Set("X-Forwarded-For", testCase.forwarded)
		}
		if actual := resolver.Resolve(request); actual != testCase.expected {
			testing.Errorf("%s: 期待 %s、実際 %s", name, testCase.expected, actual)
		}
	}
	// プロキシを信頼しない設定では、いつも接続元。不正な CIDR は、エラー。
	untrusting, _ := NewResolver(nil)
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr, request.Header = "10.1.2.3:1", http.Header{"X-Forwarded-For": {"9.9.9.9"}}
	if untrusting.Resolve(request) != "10.1.2.3" {
		testing.Error("信頼しない設定では、接続元のはず")
	}
	if _, invalidError := NewResolver([]string{"not-a-cidr"}); invalidError == nil {
		testing.Error("不正な CIDR はエラーのはず")
	}
}
