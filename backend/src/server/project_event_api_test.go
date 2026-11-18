package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"launchs/shared/database/testdatabase"
	"launchs/shared/model"
)

// TestProjectEventAPIList は、プロジェクトの k8s の Event の一覧(GET /projects/:projectId/events)の、
// 正常(新しい順、ページング、絞り込み、各項目、JST)・異常(不正な limit・cursor・type は 400、他人と存在しないプロジェクトは 404、未認証は 401)・
// 境界値(空、deployId が空なら null、既定の件数、最後のページ)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestProjectEventAPIList(testingT *testing.T) {
	// 持ち主と他のユーザーが、同じデータベースを使う。プロジェクトを 2 つ(Event ありと、なし)作る。
	database := testdatabase.New(testingT)
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database})
	projectID := createProjectFor(ownerClient, "my-app")
	emptyProjectID := createProjectFor(ownerClient, "empty-app")
	listPath := func(project string, query string) string { return "/projects/" + project + "/events" + query }

	// Event が無いプロジェクトは、空の配列で、次のページも無い。
	status, body := ownerClient.call(http.MethodGet, listPath(emptyProjectID, ""), "")
	events, _ := body["events"].([]any)
	if status != 200 || events == nil || len(events) != 0 || body["nextCursor"] != nil {
		testingT.Fatalf("空の配列で、次のページは無いはず: %d %v", status, body)
	}

	// Event を 5 件入れる(1〜5 の順に新しくなる)。1 件目だけ deploy に属し、3 件目は Warning。
	base := time.Date(2026, 11, 17, 3, 0, 0, 0, time.UTC)
	for index := 1; index <= 5; index++ {
		event := model.ProjectEvent{ProjectID: projectID, EventUID: fmt.Sprintf("uid-%d", index), Type: model.ProjectEventTypeNormal, Reason: "Pulling", Message: fmt.Sprintf("message %d", index), ObjectKind: "Pod", ObjectName: "pod-" + fmt.Sprint(index), Count: index, FirstSeenAt: base.Add(time.Duration(index-1) * time.Minute), LastSeenAt: base.Add(time.Duration(index) * time.Minute)}
		if index == 1 {
			event.DeployID = "deploy-a"
		}
		if index == 3 {
			event.Type = model.ProjectEventTypeWarning
			event.Reason = "BackOff"
		}
		if createError := database.Create(&event).Error; createError != nil {
			testingT.Fatal(createError)
		}
	}
	// 他のプロジェクトの Event は、混ざらない。
	database.Create(&model.ProjectEvent{ProjectID: emptyProjectID, EventUID: "uid-other", Type: model.ProjectEventTypeNormal, Count: 1, FirstSeenAt: base, LastSeenAt: base})

	// 全件: 新しい順で、各項目が入る。日時は JST、deployId が空なら null、eventId は "event-" + 通し番号。
	status, body = ownerClient.call(http.MethodGet, listPath(projectID, ""), "")
	events, _ = body["events"].([]any)
	if status != 200 || len(events) != 5 || body["nextCursor"] != nil {
		testingT.Fatalf("5件で、次のページは無いはず(既定の件数は 50): %d %v", status, body)
	}
	first := events[0].(map[string]any)
	if first["message"] != "message 5" || first["type"] != "Normal" || first["reason"] != "Pulling" || first["objectKind"] != "Pod" || first["objectName"] != "pod-5" || first["count"] != float64(5) || first["deployId"] != nil {
		testingT.Fatalf("先頭は最も新しい Event で、各項目が入り、deployId は null のはず: %v", first)
	}
	if id, _ := first["eventId"].(string); !strings.HasPrefix(id, "event-") {
		testingT.Errorf("eventId は event- で始まるはず: %v", first["eventId"])
	}
	if lastSeen, _ := first["lastSeenAt"].(string); lastSeen != "2026-11-17T12:05:00+09:00" {
		testingT.Errorf("日時は JST のはず: %v", first["lastSeenAt"])
	}
	if firstSeen, _ := first["firstSeenAt"].(string); firstSeen != "2026-11-17T12:04:00+09:00" {
		testingT.Errorf("最初の時刻も JST のはず: %v", first["firstSeenAt"])
	}
	if last := events[4].(map[string]any); last["message"] != "message 1" || last["deployId"] != "deploy-a" {
		testingT.Fatalf("末尾は最も古い Event で、deployId が入るはず: %v", last)
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
		for _, entry := range pageBody["events"].([]any) {
			collected = append(collected, entry.(map[string]any)["message"].(string))
		}
		next, hasNext := pageBody["nextCursor"].(string)
		if !hasNext {
			break
		}
		cursor = next
	}
	if fmt.Sprint(collected) != "[message 5 message 4 message 3 message 2 message 1]" {
		testingT.Fatalf("新しい順に、重複も欠落も無く、集まるはず: %v", collected)
	}

	// 絞り込み: 種類、deploy、理由。
	for name, expectation := range map[string]struct {
		query string
		count int
	}{"Warning だけ": {"?type=Warning", 1}, "Normal だけ": {"?type=Normal", 4}, "deploy で絞る": {"?deployId=deploy-a", 1}, "理由で絞る": {"?reason=BackOff", 1}, "合う deploy が無い": {"?deployId=deploy-none", 0}} {
		_, filtered := ownerClient.call(http.MethodGet, listPath(projectID, expectation.query), "")
		if got := len(filtered["events"].([]any)); got != expectation.count {
			testingT.Errorf("%s は %d 件のはず: %d", name, expectation.count, got)
		}
	}

	// 入力の検証(400): limit が範囲外・整数でない、cursor が不正、type が不正。
	for name, query := range map[string]string{"limit が 0": "?limit=0", "limit が 101": "?limit=101", "limit が整数でない": "?limit=abc", "cursor が不正": "?cursor=not-a-cursor", "type が小文字": "?type=warning", "type が未知": "?type=Error"} {
		if invalidStatus, _ := ownerClient.call(http.MethodGet, listPath(projectID, query), ""); invalidStatus != 400 {
			testingT.Errorf("%s は 400 のはず: %d", name, invalidStatus)
		}
	}
	// limit の境界(1 と 100)は通る。
	for _, query := range []string{"?limit=1", "?limit=100"} {
		if okStatus, _ := ownerClient.call(http.MethodGet, listPath(projectID, query), ""); okStatus != 200 {
			testingT.Errorf("%s は 200 のはず: %d", query, okStatus)
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
}
