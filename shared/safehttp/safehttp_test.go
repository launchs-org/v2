package safehttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

// TestIsBlockedAddress は、内部や予約の IP を禁止し、公開の IP を許すことを確かめる。
func TestIsBlockedAddress(testingT *testing.T) {
	// 禁止するもの。
	for _, blocked := range []string{"127.0.0.1", "10.0.0.1", "172.16.5.5", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "::1", "fc00::1", "fe80::1", "::ffff:10.0.0.1", "224.0.0.1", "255.255.255.255"} {
		if !IsBlockedAddress(netip.MustParseAddr(blocked)) {
			testingT.Errorf("%s は禁止するはず", blocked)
		}
	}
	// 許すもの。
	for _, allowed := range []string{"8.8.8.8", "93.184.216.34", "2606:4700:4700::1111"} {
		if IsBlockedAddress(netip.MustParseAddr(allowed)) {
			testingT.Errorf("%s は許すはず", allowed)
		}
	}
}

// TestValidateURL は、受け付ける URL と、断る URL を確かめる。
func TestValidateURL(testingT *testing.T) {
	// 受け付ける。
	for _, valid := range []string{"https://example.com/hook", "https://hooks.example.com:8443/a?token=x"} {
		if _, validateError := ValidateURL(valid, Options{}); validateError != nil {
			testingT.Errorf("%q は受け付けるはず: %v", valid, validateError)
		}
	}
	// 断る。
	for name, invalid := range map[string]string{
		"http": "http://example.com/", "ユーザー情報": "https://user:pass@example.com/", "localhost": "https://localhost/x", "ループバック": "https://127.0.0.1/x",
		"メタデータ": "https://169.254.169.254/latest", "プライベート": "https://10.1.2.3/", "空": "", "ホストなし": "https:///path", "ポートが不正": "https://example.com:abc/",
		"内部の名前": "https://db.internal/", "スキームなし": "example.com/hook",
	} {
		if _, validateError := ValidateURL(invalid, Options{}); validateError == nil {
			testingT.Errorf("%s(%q)は断るはず", name, invalid)
		}
	}
	// 開発用に、http と、内部の宛先を許せる。
	if _, validateError := ValidateURL("http://localhost:8080/x", Options{AllowHTTP: true, AllowPrivateNetworks: true}); validateError != nil {
		testingT.Errorf("開発用の設定では許すはず: %v", validateError)
	}
}

// TestClientBlocksLoopback は、既定のクライアントが、ループバックの受け口に接続できず、ゆるめれば接続できることを確かめる。
func TestClientBlocksLoopback(testingT *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	// 既定では、接続の直前の検査で、拒否される。
	if _, getError := NewClient(2*time.Second, Options{AllowHTTP: true}).Get(server.URL); getError == nil || !errors.Is(getError, ErrBlockedAddress) {
		testingT.Errorf("ループバックには、接続できないはず: %v", getError)
	}
	// ゆるめると、接続できる。
	response, getError := NewClient(2*time.Second, Options{AllowHTTP: true, AllowPrivateNetworks: true}).Get(server.URL)
	if getError != nil {
		testingT.Fatalf("ゆるめれば接続できるはず: %v", getError)
	}
	_ = response.Body.Close()
}

// TestClientDoesNotFollowRedirects は、リダイレクトを辿らないことを確かめる。
func TestClientDoesNotFollowRedirects(testingT *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// 最初の呼び出しは、別の場所に、リダイレクトさせる。
		if request.URL.Path == "/start" {
			http.Redirect(writer, request, "/final", http.StatusFound)
			return
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	response, getError := NewClient(2*time.Second, Options{AllowHTTP: true, AllowPrivateNetworks: true}).Get(server.URL + "/start")
	if getError != nil {
		testingT.Fatalf("呼べるはず: %v", getError)
	}
	defer func() { _ = response.Body.Close() }()
	// 302 のまま返る(辿らない)。
	if response.StatusCode != http.StatusFound {
		testingT.Errorf("リダイレクトは辿らないはず: %d", response.StatusCode)
	}
}
