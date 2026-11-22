package apitoken

import (
	"strings"
	"testing"
	"time"
)

// testSecret は、テスト用の署名の秘密(32 バイト以上)。
const testSecret = "0123456789abcdef0123456789abcdef"

// newTestSigner は、テスト用の Signer を作る。
//
// 引数:
//   - testingT: テストの状態。
//   - now: 現在時刻を返す関数。
//
// 戻り値:
//   - 作成した Signer。
func newTestSigner(testingT *testing.T, now func() time.Time) *Signer {
	// 秘密と時計を渡して作り、失敗したらテストを止める。
	signer, signerError := NewSigner([]byte(testSecret), now)
	if signerError != nil {
		testingT.Fatalf("Signer を作れません: %v", signerError)
	}
	return signer
}

// TestNewSignerRejectsShortSecret は、短い秘密を拒否することを確認する。
func TestNewSignerRejectsShortSecret(testingT *testing.T) {
	// 短い秘密で作ると、エラーになるはず。
	if _, signerError := NewSigner([]byte("short"), nil); signerError == nil {
		testingT.Fatal("短い秘密を受け付けた")
	}
}

// TestIssueAndParse は、発行したトークンを、検証して読み戻せることを確認する。
func TestIssueAndParse(testingT *testing.T) {
	// 発行する。
	signer := newTestSigner(testingT, nil)
	tokenText, issueError := signer.Issue("apitoken-1", "user-1", []string{ScopeDeployImage}, []string{"deploy-1"}, nil, nil)
	if issueError != nil {
		testingT.Fatalf("発行できません: %v", issueError)
	}
	// 接頭辞が付くこと。
	if !strings.HasPrefix(tokenText, Prefix) {
		testingT.Fatalf("接頭辞が無い: %s", tokenText)
	}
	// 検証して、claims が戻ること。
	claims, parseError := signer.Parse(tokenText)
	if parseError != nil || claims.Subject != "user-1" || claims.ID != "apitoken-1" || len(claims.Deploys) != 1 {
		testingT.Fatalf("読み戻せない: %+v, %v", claims, parseError)
	}
}

// TestParseRejects は、期限切れ・改ざん・別の秘密・接頭辞なしを拒否することを確認する。
func TestParseRejects(testingT *testing.T) {
	// 過去の期限のトークンを、いまの時計で検証する。
	signer := newTestSigner(testingT, nil)
	past := time.Now().Add(-time.Hour)
	expired, _ := signer.Issue("apitoken-1", "user-1", []string{ScopeRead}, nil, nil, &past)
	valid, _ := signer.Issue("apitoken-2", "user-1", []string{ScopeRead}, nil, nil, nil)
	otherSigner, _ := NewSigner([]byte("ffffffffffffffffffffffffffffffff"), nil)
	otherToken, _ := otherSigner.Issue("apitoken-3", "user-1", []string{ScopeRead}, nil, nil, nil)
	cases := map[string]string{"期限切れ": expired, "改ざん": valid[:len(valid)-2] + "xx", "別の秘密": otherToken, "接頭辞なし": strings.TrimPrefix(valid, Prefix)}
	// どれも ErrInvalidToken になるはず。
	for name, tokenText := range cases {
		if _, parseError := signer.Parse(tokenText); parseError != ErrInvalidToken {
			testingT.Errorf("%s を受け付けた: %v", name, parseError)
		}
	}
	// 期限なしは通る。
	if _, parseError := signer.Parse(valid); parseError != nil {
		testingT.Errorf("期限なしを拒否した: %v", parseError)
	}
}

// TestAuthorize は、scope と対象による、許可の判定を確認する。
func TestAuthorize(testingT *testing.T) {
	// 判定したい組み合わせを表にする。
	cases := []struct {
		name      string
		claims    Claims
		method    string
		route     string
		deployID  string
		projectID string
		allowed   bool
	}{
		{"read は GET を許す", Claims{Scope: []string{ScopeRead}}, "GET", "/api/v1/deploy/:deployId", "d1", "", true},
		{"read は PUT を許さない", Claims{Scope: []string{ScopeRead}}, "PUT", "/api/v1/deploy/:deployId/config", "d1", "", false},
		{"write は何でも許す", Claims{Scope: []string{ScopeWrite}}, "DELETE", "/api/v1/deploy/:deployId", "d1", "", true},
		{"deploy:image はイメージの更新を許す", Claims{Scope: []string{ScopeDeployImage}, Deploys: []string{"d1"}}, "PUT", "/api/v1/deploy/:deployId/image", "d1", "", true},
		{"deploy:image は他の操作を許さない", Claims{Scope: []string{ScopeDeployImage}, Deploys: []string{"d1"}}, "GET", "/api/v1/deploy/:deployId", "d1", "", false},
		{"範囲外の deploy は許さない", Claims{Scope: []string{ScopeWrite}, Deploys: []string{"d1"}}, "PUT", "/api/v1/deploy/:deployId/image", "d2", "", false},
		{"絞ったトークンは対象の無い操作を許さない", Claims{Scope: []string{ScopeWrite}, Deploys: []string{"d1"}}, "POST", "/api/v1/deploy", "", "", false},
		{"プロジェクトで絞る", Claims{Scope: []string{ScopeRead}, Projects: []string{"p1"}}, "GET", "/api/v1/projects/:projectId/deploys", "", "p1", true},
	}
	// 表の全てを確かめる。
	for _, testCase := range cases {
		authorizeError := testCase.claims.Authorize(testCase.method, testCase.route, testCase.deployID, testCase.projectID)
		if (authorizeError == nil) != testCase.allowed {
			testingT.Errorf("%s: 結果が違う(err=%v)", testCase.name, authorizeError)
		}
	}
}
