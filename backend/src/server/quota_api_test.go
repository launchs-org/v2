package server

import (
	"fmt"
	"net/http"
	"testing"

	"launchs/shared/database/testdatabase"
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
	instances, _ := body["instances"].(map[string]any)
	large, _ := instances["large"].(map[string]any)
	if volumeTotal["limit"] != float64(20480) || large["limit"] != float64(5) || large["used"] != float64(0) || len(instances) != 3 {
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
