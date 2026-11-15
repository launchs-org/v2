package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/database/testdatabase"
	"launchs/shared/jwtverify"
	"launchs/shared/logger"
)

// TestQuotaAPI は、GET /quota が、既定の上限と使用量を返すこと、上限を超えるプロジェクトの作成が 409 になること、
// 他のユーザーの使用量は混ざらないことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaAPI(testingT *testing.T) {
	// 同じデータベースを、2人のユーザーで使う。
	database := testdatabase.New(testingT)
	alice := newProjectAPIClient(testingT, "user-alice", Dependencies{Database: database})
	bob := newProjectAPIClient(testingT, "user-bob", Dependencies{Database: database})
	// 最初は、既定の上限で、使用量は 0。
	status, body := alice.call(http.MethodGet, "/quota", "")
	projects, _ := body["projects"].(map[string]any)
	if status != http.StatusOK || projects["limit"] != float64(10) || projects["used"] != float64(0) || body["maxVolumeSizeMB"] != float64(5120) {
		testingT.Fatalf("既定の上限で、使用量 0 のはず: %d %v", status, body)
	}
	volumeTotal, _ := body["volumeTotalMB"].(map[string]any)
	deploysBySpec, _ := body["deploysBySpec"].(map[string]any)
	large, _ := deploysBySpec["large"].(map[string]any)
	if volumeTotal["limit"] != float64(20480) || large["limit"] != float64(5) || large["used"] != float64(0) || len(deploysBySpec) != 3 || body["maxReplicas"] != float64(5) {
		testingT.Fatalf("初期値のはず: %v", body)
	}
	// alice の上限を2にして、3つ目の作成が 409 になる。
	database.Exec("INSERT INTO account_quotas (user_id, max_projects) VALUES ('user-alice', 2)")
	for number := 1; number <= 2; number++ {
		if createStatus, createBody := alice.call(http.MethodPost, "/projects", fmt.Sprintf(`{"name":"app-%d"}`, number)); createStatus != http.StatusOK {
			testingT.Fatalf("作れるはず: %d %v", createStatus, createBody)
		}
	}
	status, body = alice.call(http.MethodPost, "/projects", `{"name":"app-3"}`)
	if status != http.StatusConflict || errorCodeOf(body) != "conflict" {
		testingT.Fatalf("上限を超えたら 409 のはず: %d %v", status, body)
	}
	// 使用量が増え、上限が上書きされている。他のユーザー(bob)の使用量は混ざらない。
	_, body = alice.call(http.MethodGet, "/quota", "")
	projects, _ = body["projects"].(map[string]any)
	if projects["limit"] != float64(2) || projects["used"] != float64(2) {
		testingT.Fatalf("上限 2、使用量 2 のはず: %v", body)
	}
	_, body = bob.call(http.MethodGet, "/quota", "")
	projects, _ = body["projects"].(map[string]any)
	if projects["limit"] != float64(10) || projects["used"] != float64(0) {
		testingT.Fatalf("bob は、既定で、使用量 0 のはず: %v", body)
	}
}

// tokenVerifier は、テスト用の検証。トークンの文字列を、ユーザーの情報に対応づける("admin-token" は管理者、"alice-token" は一般ユーザー)。
type tokenVerifier struct{}

// Verify は、決まったトークンだけを通す。
//
// 引数:
//   - requestContext: 使わない。
//   - tokenText: 検証するトークン。
//
// 戻り値:
//   - トークンの中身。知らないトークンはエラー。
func (tokenVerifier) Verify(requestContext context.Context, tokenText string) (*jwtverify.Claims, error) {
	// トークンごとに、持ち主と役割を返す。
	switch tokenText {
	case "admin-token":
		return &jwtverify.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "userid-admin"}, Name: "root", Role: "admin"}, nil
	case "alice-token":
		return &jwtverify.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "userid-alice"}, Name: "alice", Role: "user"}, nil
	}
	return nil, errors.New("無効")
}

// TestAdminQuotaAPI は、管理者向けのクォータ API が、トークン無しは 401、一般ユーザーは 403、管理者は 200 であること、
// 上書きの設定が、対象のユーザーの上限に反映され(PUT は一式の置き換え)、他のユーザーに影響しないこと、
// 不正な値・知らない項目名・存在しないスペック・不正な userId は 400 で、何も変えないことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestAdminQuotaAPI(testingT *testing.T) {
	// access token の検証(役割つき)を使うサーバーを組み立てる。
	database := testdatabase.New(testingT)
	httpServer := New(logger.New(&bytes.Buffer{}, "backend", slog.LevelInfo), Dependencies{
		Database: database, Authentication: middlewares.NewAccessTokenMiddleware(tokenVerifier{}), DeployWorkflowStarter: &fakeDeployWorkflowStarter{}, ProjectDeletionStarter: &fakeDeletionStarter{},
	})
	call := func(method string, path string, body string, token string) (int, map[string]any) {
		request := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		httpServer.ServeHTTP(recorder, request)
		decoded := map[string]any{}
		_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
		return recorder.Code, decoded
	}
	path := "/admin/users/userid-alice/quota"
	// 権限: トークン無し(401)、一般ユーザー(403)。GET と PUT の両方。
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if status, _ := call(method, path, `{}`, ""); status != http.StatusUnauthorized {
			testingT.Errorf("%s: トークン無しは 401 のはず: %d", method, status)
		}
		if status, body := call(method, path, `{}`, "alice-token"); status != http.StatusForbidden || errorCodeOf(body) != "forbidden" {
			testingT.Errorf("%s: 一般ユーザーは 403 のはず: %d %v", method, status, body)
		}
	}
	// 管理者は、対象の上限と使用量、上書き(最初は無い)を見られる。
	status, body := call(http.MethodGet, path, "", "admin-token")
	overrides, _ := body["overrides"].(map[string]any)
	projects, _ := body["projects"].(map[string]any)
	if status != 200 || projects["limit"] != float64(10) || overrides["projects"] != nil || len(overrides["deploysBySpec"].(map[string]any)) != 0 {
		testingT.Fatalf("既定で、上書きは無いはず: %d %v", status, body)
	}
	// 設定すると、レスポンスと、alice 自身の /quota に反映される。
	status, body = call(http.MethodPut, path, `{"projects": 2, "volumeTotalMB": 100, "deploysBySpec": {"large": 1}}`, "admin-token")
	projects, _ = body["projects"].(map[string]any)
	overrides, _ = body["overrides"].(map[string]any)
	large := body["deploysBySpec"].(map[string]any)["large"].(map[string]any)
	if status != 200 || projects["limit"] != float64(2) || large["limit"] != float64(1) || overrides["projects"] != float64(2) || overrides["deploys"] != nil {
		testingT.Fatalf("反映されるはず: %d %v", status, body)
	}
	_, own := call(http.MethodGet, "/quota", "", "alice-token")
	if own["projects"].(map[string]any)["limit"] != float64(2) || own["volumeTotalMB"].(map[string]any)["limit"] != float64(100) {
		testingT.Fatalf("alice 自身の上限に反映されるはず: %v", own)
	}
	// alice は、上限(2)までプロジェクトを作れ、3つ目は 409。管理者(別のユーザー)は、既定のまま。
	for number := 1; number <= 2; number++ {
		if createStatus, _ := call(http.MethodPost, "/projects", fmt.Sprintf(`{"name":"app-%d"}`, number), "alice-token"); createStatus != 200 {
			testingT.Fatalf("作れるはず: %d", createStatus)
		}
	}
	if createStatus, _ := call(http.MethodPost, "/projects", `{"name":"app-3"}`, "alice-token"); createStatus != http.StatusConflict {
		testingT.Fatalf("上限を超えたら 409 のはず: %d", createStatus)
	}
	if _, adminOwn := call(http.MethodGet, "/quota", "", "admin-token"); adminOwn["projects"].(map[string]any)["limit"] != float64(10) {
		testingT.Fatalf("他のユーザーは既定のはず: %v", adminOwn)
	}
	// 一式の置き換え: 空のボディで、全て既定に戻る(使用量は、そのまま)。
	status, body = call(http.MethodPut, path, `{}`, "admin-token")
	projects, _ = body["projects"].(map[string]any)
	if status != 200 || projects["limit"] != float64(10) || projects["used"] != float64(2) {
		testingT.Fatalf("既定に戻り、使用量はそのままのはず: %d %v", status, body)
	}
	// 不正な入力は 400 で、何も変えない(先に、2 に設定しておく)。
	call(http.MethodPut, path, `{"projects": 2}`, "admin-token")
	for name, request := range map[string]struct{ path, body string }{
		"負の値":         {path, `{"projects": -1}`},
		"大きすぎる値":      {path, `{"deploys": 1000001}`},
		"知らない項目名":     {path, `{"project": 5}`},
		"存在しないスペック":   {path, `{"deploysBySpec": {"huge": 1}}`},
		"スペックの値が負":    {path, `{"deploysBySpec": {"small": -1}}`},
		"整数でない値":      {path, `{"projects": "many"}`},
		"JSON でないボディ": {path, `not json`},
		"空のボディ":       {path, ``},
		"不正な userId":  {"/admin/users/bad_id!/quota", `{}`},
	} {
		if invalidStatus, _ := call(http.MethodPut, request.path, request.body, "admin-token"); invalidStatus != http.StatusBadRequest {
			testingT.Errorf("%s: 400 のはず: %d", name, invalidStatus)
		}
	}
	if invalidGet, _ := call(http.MethodGet, "/admin/users/bad_id!/quota", "", "admin-token"); invalidGet != http.StatusBadRequest {
		testingT.Errorf("不正な userId の GET は 400 のはず: %d", invalidGet)
	}
	_, after := call(http.MethodGet, path, "", "admin-token")
	if after["projects"].(map[string]any)["limit"] != float64(2) {
		testingT.Fatalf("拒否されたら、変わらないはず: %v", after)
	}
}
