package repository

import (
	"context"
	"testing"

	"launchs/shared/apperror"
	"launchs/shared/database/testdatabase"
)

// TestAdminQueries は、全体の deploy とアクセスログの一覧(絞り込み、ページング)と、既定のクォータの更新を確かめる。
func TestAdminQueries(testingT *testing.T) {
	// 使い捨てのデータベースに、2 人分のデータを入れる。
	database := testdatabase.New(testingT)
	requestContext := context.Background()
	statements := []string{
		"INSERT INTO projects (id, user_id, name) VALUES ('project-1', 'user-a', 'alpha'), ('project-2', 'user-b', 'beta')",
		"INSERT INTO deploys (id, project_id, user_id, image_ref, spec, status, created_at) VALUES ('deploy-1', 'project-1', 'user-a', 'nginx:1', 'small', 'active', now() - interval '2 minutes'), ('deploy-2', 'project-2', 'user-b', 'redis:7', 'large', 'failed', now() - interval '1 minute')",
		"INSERT INTO ingresses (id, project_id, user_id, host) VALUES ('ingress-1', 'project-1', 'user-a', 'brave-blue-fox.example.org')",
	}
	for _, statement := range statements {
		if execError := database.Exec(statement).Error; execError != nil {
			testingT.Fatal(execError)
		}
	}
	for index, status := range []int{200, 404, 500} {
		insertError := database.Exec("INSERT INTO ingress_access_logs (ingress_id, replica, batch_start, seq, requested_at, method, url, status, duration_ms, client_ip, bytes) VALUES ('ingress-1', 'r', now(), ?, now() - make_interval(secs => ?), 'GET', '/a%b', ?, 1, '203.0.113.5', 10)", index, 10-index, status).Error
		if insertError != nil {
			testingT.Fatal(insertError)
		}
	}
	queryRepository := NewAdminQueryRepository(database)

	// deploy は、新しい順に、全ユーザー分が出る。絞り込みと、ページングもできる。
	deploys, next, listError := queryRepository.ListDeploys(requestContext, 10, nil, AdminDeployFilter{})
	if listError != nil || len(deploys) != 2 || next != nil || deploys[0].ID != "deploy-2" || deploys[1].ProjectName != "alpha" {
		testingT.Fatalf("deploy が 2 件、新しい順のはず: %v %v", deploys, listError)
	}
	if failed, _, _ := queryRepository.ListDeploys(requestContext, 10, nil, AdminDeployFilter{Status: "failed"}); len(failed) != 1 || failed[0].UserID != "user-b" {
		testingT.Errorf("状態で絞り込めるはず: %v", failed)
	}
	if byKeyword, _, _ := queryRepository.ListDeploys(requestContext, 10, nil, AdminDeployFilter{Keyword: "ALPHA"}); len(byKeyword) != 1 {
		testingT.Errorf("プロジェクト名の一部で絞り込めるはず: %v", byKeyword)
	}
	firstPage, position, _ := queryRepository.ListDeploys(requestContext, 1, nil, AdminDeployFilter{})
	secondPage, _, _ := queryRepository.ListDeploys(requestContext, 1, position, AdminDeployFilter{})
	if len(firstPage) != 1 || position == nil || len(secondPage) != 1 || secondPage[0].ID != "deploy-1" {
		testingT.Errorf("2 ページに分けられるはず: %v %v", firstPage, secondPage)
	}

	// アクセスログは、ホスト名つきで、新しい順。ステータスの分類、数字、キーワードで絞り込める。
	logs, _, logError := queryRepository.ListAccessLogs(requestContext, 10, nil, AdminAccessLogFilter{})
	if logError != nil || len(logs) != 3 || logs[0].Status != 500 || logs[0].Host != "brave-blue-fox.example.org" {
		testingT.Fatalf("3 件、新しい順のはず: %v %v", logs, logError)
	}
	if clientErrors, _, _ := queryRepository.ListAccessLogs(requestContext, 10, nil, AdminAccessLogFilter{Status: "4xx"}); len(clientErrors) != 1 {
		testingT.Errorf("4xx は 1 件のはず: %v", clientErrors)
	}
	if exact, _, _ := queryRepository.ListAccessLogs(requestContext, 10, nil, AdminAccessLogFilter{Status: "200", ClientIP: "203.0.113.5"}); len(exact) != 1 {
		testingT.Errorf("200 は 1 件のはず: %v", exact)
	}
	if percent, _, _ := queryRepository.ListAccessLogs(requestContext, 10, nil, AdminAccessLogFilter{Keyword: "a%b"}); len(percent) != 3 {
		testingT.Errorf("パーセント記号は、そのままの文字として探すはず: %v", percent)
	}
	if _, _, badError := queryRepository.ListAccessLogs(requestContext, 10, nil, AdminAccessLogFilter{Status: "abc"}); apperror.KindOf(badError) != apperror.KindInvalidRequest {
		testingT.Errorf("不正な status は invalid_request のはず: %v", badError)
	}
	pageOne, logPosition, _ := queryRepository.ListAccessLogs(requestContext, 2, nil, AdminAccessLogFilter{})
	pageTwo, _, _ := queryRepository.ListAccessLogs(requestContext, 2, logPosition, AdminAccessLogFilter{})
	if len(pageOne) != 2 || logPosition == nil || len(pageTwo) != 1 || pageTwo[0].Status != 200 {
		testingT.Errorf("ページングできるはず: %v %v", pageOne, pageTwo)
	}

	// 全体のクォータの既定を、読んで、更新して、反映を確かめる。存在しないスペックは拒否する。
	quotaRepository := NewQuotaDefaultRepository(database)
	defaults, getError := quotaRepository.Get(requestContext)
	if getError != nil || defaults.Projects != 10 || defaults.DeploysBySpec["large"] != 5 {
		testingT.Fatalf("初期値のはず: %v %v", defaults, getError)
	}
	updated, setError := quotaRepository.Set(requestContext, QuotaDefaults{Projects: 3, Deploys: 4, Volumes: 5, VolumeTotalMB: 600, DeploysBySpec: map[string]int{"large": 1}})
	if setError != nil || updated.Projects != 3 || updated.VolumeTotalMB != 600 || updated.DeploysBySpec["large"] != 1 || updated.DeploysBySpec["small"] != 20 {
		testingT.Fatalf("更新できるはず(含めないスペックは変わらない): %v %v", updated, setError)
	}
	if _, badSpecError := quotaRepository.Set(requestContext, QuotaDefaults{DeploysBySpec: map[string]int{"huge": 1}}); apperror.KindOf(badSpecError) != apperror.KindInvalidRequest {
		testingT.Errorf("存在しないスペックは invalid_request のはず: %v", badSpecError)
	}
	// 失敗した更新は、全体の既定も変えない(1 つのトランザクション)。
	if unchanged, _ := quotaRepository.Get(requestContext); unchanged.Projects != 3 {
		testingT.Errorf("失敗した更新で値が変わってはいけない: %v", unchanged)
	}
}
