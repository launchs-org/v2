package turnstile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"launchs/shared/apperror"
)

// fakeCloudflare は、テスト用の、偽の siteverify。受けたフォームを記録し、決めた応答を返す。
type fakeCloudflare struct {
	server      *httptest.Server
	form        url.Values
	calls       atomic.Int64
	status      int
	body        string
	contentType string
}

// newFakeCloudflare は、偽の siteverify を立てる(テストの終わりに止める)。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - status: 返すステータス。
//   - body: 返すボディ。
//
// 戻り値:
//   - 偽の siteverify。
func newFakeCloudflare(testingT *testing.T, status int, body string) *fakeCloudflare {
	// リクエストを記録して、決めた応答を返すサーバーを立てる。
	fake := &fakeCloudflare{status: status, body: body}
	fake.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fake.calls.Add(1)
		_ = request.ParseForm()
		fake.form = request.PostForm
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.WriteHeader(fake.status)
		_, _ = writer.Write([]byte(fake.body))
	}))
	testingT.Cleanup(fake.server.Close)
	return fake
}

// TestVerifySuccessSendsSecretTokenAndIP は、通ること、シークレット・トークン・IP が、フォームで送られることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestVerifySuccessSendsSecretTokenAndIP(testingT *testing.T) {
	// 成功を返す偽の Cloudflare。
	fake := newFakeCloudflare(testingT, 200, `{"success":true,"hostname":"app.example","challenge_ts":"2026-11-16T00:00:00Z"}`)
	verifier := NewVerifier(Options{SecretKey: "secret-1", VerifyURL: fake.server.URL})
	if verifyError := verifier.Verify(context.Background(), "token-1", "203.0.113.5"); verifyError != nil {
		testingT.Fatalf("通るはず: %v", verifyError)
	}
	// 送った内容。
	if fake.form.Get("secret") != "secret-1" || fake.form.Get("response") != "token-1" || fake.form.Get("remoteip") != "203.0.113.5" {
		testingT.Fatalf("シークレット、トークン、IP が送られるはず: %v", fake.form)
	}
	// IP が空なら、remoteip は送らない。
	_ = verifier.Verify(context.Background(), "token-2", "")
	if _, sent := fake.form["remoteip"]; sent {
		testingT.Fatal("IP が空なら、送らないはず")
	}
}

// TestVerifyRejectsBadTokensWithoutCallingCloudflare は、トークンが無い・空白だけ・長すぎるとき、Cloudflare に問い合わせず、invalid_request にすることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestVerifyRejectsBadTokensWithoutCallingCloudflare(testingT *testing.T) {
	fake := newFakeCloudflare(testingT, 200, `{"success":true}`)
	verifier := NewVerifier(Options{SecretKey: "s", VerifyURL: fake.server.URL})
	for name, token := range map[string]string{"空": "", "空白だけ": "   ", "長すぎる": strings.Repeat("a", MaxTokenLength+1)} {
		if verifyError := verifier.Verify(context.Background(), token, ""); apperror.KindOf(verifyError) != apperror.KindInvalidRequest {
			testingT.Errorf("%s は invalid_request のはず: %v", name, verifyError)
		}
	}
	if fake.calls.Load() != 0 {
		testingT.Fatalf("Cloudflare には、問い合わせないはず: %d", fake.calls.Load())
	}
	// 上限ちょうどの長さは、問い合わせる。
	if verifyError := verifier.Verify(context.Background(), strings.Repeat("a", MaxTokenLength), ""); verifyError != nil || fake.calls.Load() != 1 {
		testingT.Fatalf("上限ちょうどは、問い合わせて、通るはず: %v %d", verifyError, fake.calls.Load())
	}
}

// TestVerifyClassifiesFailures は、Cloudflare の失敗を、トークンの問題(400)、シークレットの誤り(500)、Cloudflare の障害(503)に分けることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestVerifyClassifiesFailures(testingT *testing.T) {
	testCases := []struct {
		name     string
		status   int
		body     string
		expected apperror.Kind
	}{
		{"無効なトークン", 200, `{"success":false,"error-codes":["invalid-input-response"]}`, apperror.KindInvalidRequest},
		{"使用済み・期限切れ", 200, `{"success":false,"error-codes":["timeout-or-duplicate"]}`, apperror.KindInvalidRequest},
		{"トークンが無い", 200, `{"success":false,"error-codes":["missing-input-response"]}`, apperror.KindInvalidRequest},
		{"シークレットが違う(設定の誤り)", 200, `{"success":false,"error-codes":["invalid-input-secret"]}`, apperror.KindInternal},
		{"シークレットが無い", 200, `{"success":false,"error-codes":["missing-input-secret"]}`, apperror.KindInternal},
		{"Cloudflare の内部エラー", 200, `{"success":false,"error-codes":["internal-error"]}`, apperror.KindUnavailable},
		{"Cloudflare が 500", 500, `oops`, apperror.KindUnavailable},
		{"Cloudflare が 503", 503, ``, apperror.KindUnavailable},
		{"JSON でない応答", 200, `<html>proxy error</html>`, apperror.KindUnavailable},
		{"空の応答", 200, ``, apperror.KindUnavailable},
		{"success=false で、コードが無い", 200, `{"success":false}`, apperror.KindInvalidRequest},
	}
	for _, testCase := range testCases {
		fake := newFakeCloudflare(testingT, testCase.status, testCase.body)
		verifyError := NewVerifier(Options{SecretKey: "s", VerifyURL: fake.server.URL}).Verify(context.Background(), "t", "")
		if apperror.KindOf(verifyError) != testCase.expected {
			testingT.Errorf("%s: 期待 %s、実際 %v", testCase.name, testCase.expected, verifyError)
		}
	}
}

// TestVerifyDoesNotLeakSecretToUsers は、設定の誤り(シークレットの誤り)のエラーが internal_error で、利用者向けのメッセージに、シークレットも、設定の詳細(エラーコード)も含めず、原因は、エラーの中身(ログ用)に残ることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestVerifyDoesNotLeakSecretToUsers(testingT *testing.T) {
	fake := newFakeCloudflare(testingT, 200, `{"success":false,"error-codes":["invalid-input-secret"]}`)
	verifyError := NewVerifier(Options{SecretKey: "super-secret-value", VerifyURL: fake.server.URL}).Verify(context.Background(), "t", "")
	var appError *apperror.Error
	if !errors.As(verifyError, &appError) || strings.Contains(appError.Message, "super-secret-value") || strings.Contains(appError.Message, "invalid-input-secret") || strings.Contains(appError.Message, "シークレット") {
		testingT.Fatalf("利用者向けのメッセージに、シークレットや設定の詳細を含めてはいけない: %+v", appError)
	}
	// 原因は、ログ用に、エラーの中身に残る(シークレットそのものは、含めない)。
	if !strings.Contains(verifyError.Error(), "invalid-input-secret") || strings.Contains(verifyError.Error(), "super-secret-value") {
		testingT.Fatalf("原因は残り、シークレットは含まないはず: %v", verifyError)
	}
}

// TestVerifyUnreachableAndTimeout は、Cloudflare に届かない(接続できない、遅すぎる)とき、service_unavailable になることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestVerifyUnreachableAndTimeout(testingT *testing.T) {
	// 閉じたサーバーの URL(接続できない)。
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	if verifyError := NewVerifier(Options{SecretKey: "s", VerifyURL: closedURL}).Verify(context.Background(), "t", ""); apperror.KindOf(verifyError) != apperror.KindUnavailable {
		testingT.Fatalf("接続できないときは service_unavailable のはず: %v", verifyError)
	}
	// 応答が遅すぎる(クライアントのタイムアウトが、短い)。
	slow := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-request.Context().Done():
		}
	}))
	defer slow.Close()
	verifier := NewVerifier(Options{SecretKey: "s", VerifyURL: slow.URL, HTTPClient: &http.Client{Timeout: 50 * time.Millisecond}})
	if verifyError := verifier.Verify(context.Background(), "t", ""); apperror.KindOf(verifyError) != apperror.KindUnavailable {
		testingT.Fatalf("遅すぎるときは service_unavailable のはず: %v", verifyError)
	}
	// 呼び出し側の context が、キャンセルされたときも、service_unavailable(待たない)。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if verifyError := NewVerifier(Options{SecretKey: "s", VerifyURL: slow.URL}).Verify(canceled, "t", ""); apperror.KindOf(verifyError) != apperror.KindUnavailable {
		testingT.Fatalf("キャンセルされたら、待たずに失敗するはず: %v", verifyError)
	}
}

// TestVerifyHostnames は、許すホスト名が決まっているとき、それ以外のホスト名で作られたトークンを、拒否することを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestVerifyHostnames(testingT *testing.T) {
	fake := newFakeCloudflare(testingT, 200, `{"success":true,"hostname":"evil.example"}`)
	restricted := NewVerifier(Options{SecretKey: "s", VerifyURL: fake.server.URL, Hostnames: []string{"app.example", "localhost"}})
	if verifyError := restricted.Verify(context.Background(), "t", ""); apperror.KindOf(verifyError) != apperror.KindInvalidRequest {
		testingT.Fatalf("別のホスト名は、拒否するはず: %v", verifyError)
	}
	fake.body = `{"success":true,"hostname":"app.example"}`
	if verifyError := restricted.Verify(context.Background(), "t", ""); verifyError != nil {
		testingT.Fatalf("許すホスト名は、通るはず: %v", verifyError)
	}
	// 許すホスト名の指定が無ければ、確かめない。
	fake.body = `{"success":true,"hostname":"anything.example"}`
	if verifyError := NewVerifier(Options{SecretKey: "s", VerifyURL: fake.server.URL}).Verify(context.Background(), "t", ""); verifyError != nil {
		testingT.Fatalf("指定が無ければ、確かめないはず: %v", verifyError)
	}
}
