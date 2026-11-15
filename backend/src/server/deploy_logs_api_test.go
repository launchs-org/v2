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

// TestDeployAPIGetLogs は、ログの取得(GET /deploy/:deployId/logs)の、正常(時刻つきの行、続きの取得)・異常(podId、since、他人、
// 別の deploy の Pod、存在しない Pod)・境界値(1000行ごと、末尾を超える since、行が全て無い Pod)を確認する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployAPIGetLogs(testingT *testing.T) {
	// deploy を2つ作る。1つ目の Pod(uid-1)に 2500 行、2つ目の deploy の Pod(uid-other)に 1 行を、直接保存する。
	database := testdatabase.New(testingT)
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database})
	projectID := createProjectFor(ownerClient, "my-app")
	deployID := createDeployFor(ownerClient, projectID)
	otherDeployID := createDeployFor(ownerClient, projectID)
	startedAt := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	for _, stream := range []model.DeployLogStream{
		{PodUID: "uid-1", DeployID: deployID, ProjectID: projectID, PodName: "p1", StartedAt: startedAt},
		{PodUID: "uid-empty", DeployID: deployID, ProjectID: projectID, PodName: "p2", StartedAt: startedAt},
		{PodUID: "uid-other", DeployID: otherDeployID, ProjectID: projectID, PodName: "p3", StartedAt: startedAt},
	} {
		database.Create(&stream)
	}
	lines := make([]model.DeployLog, 0, 2500)
	for index := 0; index < 2500; index++ {
		lines = append(lines, model.DeployLog{PodUID: "uid-1", LoggedNS: startedAt.UnixNano() + int64(index)*int64(time.Millisecond), Line: fmt.Sprintf("line-%d", index)})
	}
	database.CreateInBatches(&lines, 500)
	database.Create(&model.DeployLog{PodUID: "uid-other", LoggedNS: startedAt.UnixNano(), Line: "other deploy's line"})
	logsPath := func(id string, query string) string { return "/deploy/" + id + "/logs" + query }

	// 最初から: 1000 行。各行は、JST のミリ秒つきの時刻と本文と改行。nextSince は、最後の行の id。
	status, body := ownerClient.call(http.MethodGet, logsPath(deployID, "?podId=pod-uid-1"), "")
	firstLogs, _ := body["logs"].(string)
	firstLines := strings.Split(strings.TrimSuffix(firstLogs, "\n"), "\n")
	if status != 200 || len(firstLines) != 1000 || firstLines[0] != "2026-11-15T21:00:00.000+09:00 line-0" || firstLines[999] != "2026-11-15T21:00:00.999+09:00 line-999" || !strings.HasSuffix(firstLogs, "\n") {
		testingT.Fatalf("最初の1000行のはず: %d %d %q", status, len(firstLines), firstLines[0])
	}
	firstNext := int64(body["nextSince"].(float64))
	// 続き: nextSince を since に渡すと、次の 1000 行、さらに残りの 500 行。
	_, body = ownerClient.call(http.MethodGet, logsPath(deployID, fmt.Sprintf("?podId=pod-uid-1&since=%d", firstNext)), "")
	secondLines := strings.Split(strings.TrimSuffix(body["logs"].(string), "\n"), "\n")
	secondNext := int64(body["nextSince"].(float64))
	if len(secondLines) != 1000 || !strings.HasSuffix(secondLines[0], " line-1000") || secondNext <= firstNext {
		testingT.Fatalf("次の1000行のはず: %d %q", len(secondLines), secondLines[0])
	}
	_, body = ownerClient.call(http.MethodGet, logsPath(deployID, fmt.Sprintf("?podId=pod-uid-1&since=%d", secondNext)), "")
	thirdLines := strings.Split(strings.TrimSuffix(body["logs"].(string), "\n"), "\n")
	thirdNext := int64(body["nextSince"].(float64))
	if len(thirdLines) != 500 || !strings.HasSuffix(thirdLines[499], " line-2499") {
		testingT.Fatalf("残りの500行のはず: %d", len(thirdLines))
	}
	// 末尾まで読んだ後は、空の文字列で、nextSince は、そのまま。
	_, body = ownerClient.call(http.MethodGet, logsPath(deployID, fmt.Sprintf("?podId=pod-uid-1&since=%d", thirdNext)), "")
	if body["logs"] != "" || int64(body["nextSince"].(float64)) != thirdNext {
		testingT.Fatalf("空で、nextSince はそのままのはず: %v", body)
	}
	// since が末尾を超えるときは、エラーにせず、空で、nextSince は末尾。
	_, body = ownerClient.call(http.MethodGet, logsPath(deployID, fmt.Sprintf("?podId=pod-uid-1&since=%d", thirdNext+100000)), "")
	if body["logs"] != "" || int64(body["nextSince"].(float64)) != thirdNext {
		testingT.Fatalf("末尾を超えたら、空で、nextSince は末尾のはず: %v", body)
	}
	// 行が全て無い Pod は、200 で、空、nextSince は 0。
	status, body = ownerClient.call(http.MethodGet, logsPath(deployID, "?podId=pod-uid-empty"), "")
	if status != 200 || body["logs"] != "" || body["nextSince"] != float64(0) {
		testingT.Fatalf("空で 200 のはず: %d %v", status, body)
	}
	// 入力の検証(400): podId が無い、接頭辞が無い、UID が空、since が整数でない、負。
	for name, query := range map[string]string{
		"podId が無い": "", "podId に接頭辞が無い": "?podId=uid-1", "UID が空": "?podId=pod-", "since が整数でない": "?podId=pod-uid-1&since=abc", "since が負": "?podId=pod-uid-1&since=-1",
	} {
		if invalidStatus, _ := ownerClient.call(http.MethodGet, logsPath(deployID, query), ""); invalidStatus != 400 {
			testingT.Errorf("%s は 400 のはず: %d", name, invalidStatus)
		}
	}
	// 404: 他のユーザー、存在しない deploy、別の deploy の Pod、存在しない Pod。
	for name, check := range map[string]struct {
		client *projectAPIClient
		id     string
		query  string
	}{
		"他のユーザー":          {otherClient, deployID, "?podId=pod-uid-1"},
		"存在しない deploy":    {ownerClient, "deploy-nothing", "?podId=pod-uid-1"},
		"別の deploy の Pod": {ownerClient, deployID, "?podId=pod-uid-other"},
		"存在しない Pod":       {ownerClient, deployID, "?podId=pod-uid-nothing"},
	} {
		if notFoundStatus, _ := check.client.call(http.MethodGet, logsPath(check.id, check.query), ""); notFoundStatus != 404 {
			testingT.Errorf("%s は 404 のはず: %d", name, notFoundStatus)
		}
	}
	// 認証が無ければ 401。
	unauthenticated := newProjectAPIClient(testingT, "", Dependencies{Database: database})
	if unauthenticatedStatus, _ := unauthenticated.call(http.MethodGet, logsPath(deployID, "?podId=pod-uid-1"), ""); unauthenticatedStatus != 401 {
		testingT.Errorf("認証が無ければ 401 のはず: %d", unauthenticatedStatus)
	}
	// deploy を消すと(ストリームは CASCADE で消える)、404。
	database.Exec("DELETE FROM deploys WHERE id = ?", deployID)
	if deletedStatus, _ := ownerClient.call(http.MethodGet, logsPath(deployID, "?podId=pod-uid-1"), ""); deletedStatus != 404 {
		testingT.Errorf("deploy を消したら 404 のはず: %d", deletedStatus)
	}
}

// TestDeployAPIGetLogPods は、deploy の詳細の logPods が、ログの行が残っている Pod だけを、新しい Pod の順に返すこと
// (終了した Pod は running=false と endedAt、稼働中は running=true と endedAt=null)、pods(稼働中の Pod)とは別であること、
// 他の deploy の Pod は載らず、最大 50 件であることを確認する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployAPIGetLogPods(testingT *testing.T) {
	// deploy を2つ作る。
	database := testdatabase.New(testingT)
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database})
	projectID := createProjectFor(ownerClient, "my-app")
	deployID := createDeployFor(ownerClient, projectID)
	otherDeployID := createDeployFor(ownerClient, projectID)
	logPodsOf := func() []any {
		status, body := ownerClient.call(http.MethodGet, "/deploy/"+deployID, "")
		logPods, _ := body["logPods"].([]any)
		if status != 200 || logPods == nil {
			testingT.Fatalf("logPods は、空でも配列のはず: %d %v", status, body)
		}
		return logPods
	}
	// ログが無いうちは、空の配列。
	if len(logPodsOf()) != 0 {
		testingT.Fatal("空のはず")
	}
	// Pod を3つ(古い順: ended、running、no-lines)と、他の deploy の Pod を用意する。
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	endedAt := base.Add(30 * time.Minute)
	for _, stream := range []model.DeployLogStream{
		{PodUID: "uid-ended", DeployID: deployID, ProjectID: projectID, PodName: "a", StartedAt: base, FinishedAt: &endedAt},
		{PodUID: "uid-running", DeployID: deployID, ProjectID: projectID, PodName: "b", StartedAt: base.Add(time.Hour)},
		{PodUID: "uid-no-lines", DeployID: deployID, ProjectID: projectID, PodName: "c", StartedAt: base.Add(2 * time.Hour)},
		{PodUID: "uid-other", DeployID: otherDeployID, ProjectID: projectID, PodName: "d", StartedAt: base.Add(3 * time.Hour)},
	} {
		database.Create(&stream)
	}
	for _, uid := range []string{"uid-ended", "uid-running", "uid-other"} {
		database.Create(&model.DeployLog{PodUID: uid, LoggedNS: base.UnixNano(), Line: "x"})
	}
	// 行のある2つが、新しい順(running、ended)。ログの無い Pod と、他の deploy の Pod は、載らない。
	logPods := logPodsOf()
	if len(logPods) != 2 {
		testingT.Fatalf("2つのはず: %v", logPods)
	}
	running, ended := logPods[0].(map[string]any), logPods[1].(map[string]any)
	if running["podId"] != "pod-uid-running" || running["running"] != true || running["endedAt"] != nil || running["startedAt"] != "2026-11-15T22:00:00+09:00" {
		testingT.Fatalf("稼働中の Pod が先のはず: %v", running)
	}
	if ended["podId"] != "pod-uid-ended" || ended["running"] != false || ended["endedAt"] != "2026-11-15T21:30:00+09:00" || ended["startedAt"] != "2026-11-15T21:00:00+09:00" {
		testingT.Fatalf("終了した Pod が後のはず: %v", ended)
	}
	// logPods の podId で、ログが取れる(終了した Pod も)。
	if status, body := ownerClient.call(http.MethodGet, "/deploy/"+deployID+"/logs?podId="+ended["podId"].(string), ""); status != 200 || !strings.HasSuffix(body["logs"].(string), " x\n") {
		testingT.Fatalf("logPods の podId で、ログが取れるはず: %d %v", status, body)
	}
	// pods(稼働中の Pod)は、別のもの: deploy_pods に無ければ、空のまま。
	_, detail := ownerClient.call(http.MethodGet, "/deploy/"+deployID, "")
	if pods, _ := detail["pods"].([]any); len(pods) != 0 {
		testingT.Fatalf("pods は、deploy_pods だけを返すはず: %v", pods)
	}
	// 60 個のストリーム(全て行あり)があっても、最大 50 件。
	for index := 0; index < 60; index++ {
		uid := fmt.Sprintf("uid-many-%02d", index)
		database.Create(&model.DeployLogStream{PodUID: uid, DeployID: deployID, ProjectID: projectID, PodName: uid, StartedAt: base.Add(24*time.Hour + time.Duration(index)*time.Minute)})
		database.Create(&model.DeployLog{PodUID: uid, LoggedNS: base.UnixNano(), Line: "y"})
	}
	if many := logPodsOf(); len(many) != 50 || many[0].(map[string]any)["podId"] != "pod-uid-many-59" {
		testingT.Fatalf("最大 50 件で、新しい Pod が先のはず: %d", len(many))
	}
}
