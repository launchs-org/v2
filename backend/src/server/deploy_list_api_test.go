package server

import (
	"fmt"
	"net/http"
	"testing"

	"launchs/shared/database/testdatabase"
)

// TestDeployAPIListByProject は、プロジェクトの deploy の履歴(GET /projects/:projectId/deploys)の、正常(新しい順、ページング、各項目)・
// 異常(他人・存在しないプロジェクトは 404、limit と cursor の不正は 400、未認証は 401)・境界値(空、削除中を含む、最後のページ)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployAPIListByProject(testingT *testing.T) {
	// 持ち主と他のユーザーが、同じデータベースを使う。プロジェクトを2つ(deploy ありと、なし)作る。
	database := testdatabase.New(testingT)
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database})
	projectID := createProjectFor(ownerClient, "my-app")
	emptyProjectID := createProjectFor(ownerClient, "empty-app")
	listPath := func(project string, query string) string { return "/projects/" + project + "/deploys" + query }

	// deploy が無いプロジェクトは、空の配列で、次のページも無い。
	status, body := ownerClient.call(http.MethodGet, listPath(emptyProjectID, ""), "")
	deploys, _ := body["deploys"].([]any)
	if status != 200 || deploys == nil || len(deploys) != 0 || body["nextCursor"] != nil {
		testingT.Fatalf("空の配列で、次のページは無いはず: %d %v", status, body)
	}

	// deploy を 5 つ作る(作った順に、古い → 新しい)。1つ目を、削除中にする。
	var deployIDs []string
	for range 5 {
		deployIDs = append(deployIDs, createDeployFor(ownerClient, projectID))
	}
	database.Exec("UPDATE deploys SET status = 'deleting' WHERE id = ?", deployIDs[0])

	// 全件: 新しい順で、各項目が入る。削除中のものも含まれる。
	status, body = ownerClient.call(http.MethodGet, listPath(projectID, ""), "")
	deploys, _ = body["deploys"].([]any)
	if status != 200 || len(deploys) != 5 || body["nextCursor"] != nil {
		testingT.Fatalf("5件で、次のページは無いはず: %d %v", status, body)
	}
	first := deploys[0].(map[string]any)
	if first["deployId"] != deployIDs[4] || first["status"] != "pending" || first["spec"] != "small" || first["replicas"] != float64(1) || first["imageRef"] != "registry/app:1" || first["createdAt"] == nil {
		testingT.Fatalf("先頭は最も新しい deploy で、各項目が入るはず: %v", first)
	}
	if last := deploys[4].(map[string]any); last["deployId"] != deployIDs[0] || last["status"] != "deleting" {
		testingT.Fatalf("末尾は最も古い deploy で、削除中のはず: %v", last)
	}

	// ページング: 2 件ずつ辿ると、5 件を、重複も欠落も無く、新しい順に集められる。最後のページで、nextCursor が null になる。
	var collected []string
	cursor := ""
	for page := 0; page < 5; page++ {
		query := "?limit=2"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		_, pageBody := ownerClient.call(http.MethodGet, listPath(projectID, query), "")
		for _, entry := range pageBody["deploys"].([]any) {
			collected = append(collected, entry.(map[string]any)["deployId"].(string))
		}
		next, hasNext := pageBody["nextCursor"].(string)
		if !hasNext {
			break
		}
		cursor = next
	}
	expected := []string{deployIDs[4], deployIDs[3], deployIDs[2], deployIDs[1], deployIDs[0]}
	if fmt.Sprint(collected) != fmt.Sprint(expected) {
		testingT.Fatalf("新しい順に、重複も欠落も無く、集まるはず: %v(期待 %v)", collected, expected)
	}

	// 入力の検証(400): limit が範囲外・整数でない、cursor が不正。
	for name, query := range map[string]string{"limit が 0": "?limit=0", "limit が 101": "?limit=101", "limit が整数でない": "?limit=abc", "cursor が不正": "?cursor=not-a-cursor"} {
		if invalidStatus, _ := ownerClient.call(http.MethodGet, listPath(projectID, query), ""); invalidStatus != 400 {
			testingT.Errorf("%s は 400 のはず: %d", name, invalidStatus)
		}
	}
	// 他人のプロジェクトと、存在しないプロジェクトは、同じ 404。
	if otherStatus, _ := otherClient.call(http.MethodGet, listPath(projectID, ""), ""); otherStatus != 404 {
		testingT.Errorf("他人のプロジェクトは 404 のはず: %d", otherStatus)
	}
	if missingStatus, _ := ownerClient.call(http.MethodGet, listPath("project-nothing", ""), ""); missingStatus != 404 {
		testingT.Errorf("存在しないプロジェクトは 404 のはず: %d", missingStatus)
	}
	// 認証が無ければ 401。
	unauthenticated := newProjectAPIClient(testingT, "", Dependencies{Database: database})
	if unauthenticatedStatus, _ := unauthenticated.call(http.MethodGet, listPath(projectID, ""), ""); unauthenticatedStatus != 401 {
		testingT.Errorf("認証が無ければ 401 のはず: %d", unauthenticatedStatus)
	}
	// 他のユーザーの deploy は、混ざらない(他のユーザーが、自分のプロジェクトに作った deploy は、そのプロジェクトにだけ出る)。
	otherProjectID := createProjectFor(otherClient, "other-app")
	otherDeployID := createDeployFor(otherClient, otherProjectID)
	_, otherBody := otherClient.call(http.MethodGet, listPath(otherProjectID, ""), "")
	if otherDeploys := otherBody["deploys"].([]any); len(otherDeploys) != 1 || otherDeploys[0].(map[string]any)["deployId"] != otherDeployID {
		testingT.Fatalf("自分のプロジェクトの deploy だけのはず: %v", otherBody)
	}
}
