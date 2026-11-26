package server

import (
	"net/http"
	"strings"
	"testing"
)

// TestTemplateAPI は、テンプレートの API(GET /templates、GET /templates/:templateId)の、
// 正常(一覧、1件、変数の項目、要約)・異常(無い id は 404、未認証は 401)・秘密の扱い(自動生成の password の既定値を返さない)を確認する。
func TestTemplateAPI(testingT *testing.T) {
	// 同梱のテンプレートで動くクライアント(データベースは使わないが、他の API と同じ作りのサーバーを使う)。
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{})

	testingT.Run("一覧は、同梱の 17 個を、id の順に返し、作る部品の中身(resources)は返さない", func(subTest *testing.T) {
		statusCode, responseBody := apiClient.call(http.MethodGet, "/templates", "")
		templates, _ := responseBody["templates"].([]any)
		if statusCode != http.StatusOK || len(templates) != 17 {
			subTest.Fatalf("200 で、17 個のはず: %d %v", statusCode, responseBody)
		}
		// id で引けるようにし、並びは、id の順(cloudbeaver、code-server、…、wordpress)になっていることを確かめる。
		byID := map[string]map[string]any{}
		ids := make([]string, 0, len(templates))
		for _, raw := range templates {
			item, _ := raw.(map[string]any)
			byID[item["id"].(string)] = item
			ids = append(ids, item["id"].(string))
			// 中身(resources)は、返さず、要約(summary)だけを返す。
			if _, hasResources := item["resources"]; hasResources {
				subTest.Errorf("%s: resources は返さないはず", item["id"])
			}
		}
		if strings.Join(ids, ",") != "cloudbeaver,code-server,excalidraw,gitea,grafana,hello-world,mailpit,mariadb,memos,n8n,nginx,postgres,redash,redis,uptime-kuma,vaultwarden,wordpress" {
			subTest.Errorf("id の順のはず: %v", ids)
		}
		// 要約: postgres は deploy 1・volume 1・ingress なし・ファイル 1、nginx は ingress あり、code-server は volume と ingress あり。
		postgres, _ := byID["postgres"]["summary"].(map[string]any)
		if postgres["deploys"] != float64(1) || postgres["volumes"] != float64(1) || postgres["ingress"] != false || postgres["files"] != float64(1) {
			subTest.Errorf("postgres の要約が違う: %v", postgres)
		}
		nginx, _ := byID["nginx"]["summary"].(map[string]any)
		codeServer, _ := byID["code-server"]["summary"].(map[string]any)
		if nginx["ingress"] != true || codeServer["ingress"] != true || codeServer["volumes"] != float64(1) {
			subTest.Errorf("nginx と code-server は ingress あり、code-server は volume 1: %v %v", nginx, codeServer)
		}
	})
	testingT.Run("一覧と詳細に、版(version と versions)が入る。詳細は、?version= で版を指定でき、README を返す。無い版は 404", func(subTest *testing.T) {
		_, listBody := apiClient.call(http.MethodGet, "/templates", "")
		first, _ := listBody["templates"].([]any)[0].(map[string]any)
		versions, _ := first["versions"].([]any)
		statusCode, detail := apiClient.call(http.MethodGet, "/templates/nginx?version=1.0.0", "")
		missingStatus, _ := apiClient.call(http.MethodGet, "/templates/nginx?version=9.9.9", "")
		if first["version"] != "1.0.0" || len(versions) < 1 || statusCode != http.StatusOK || detail["version"] != "1.0.0" || detail["readme"] == "" || missingStatus != http.StatusNotFound {
			subTest.Errorf("版が入り、README があり、無い版は 404 のはず: %v %v %d %v %d", first["version"], versions, statusCode, detail["readme"], missingStatus)
		}
		if _, hasReadme := first["readme"]; hasReadme {
			subTest.Errorf("一覧には、README を入れないはず: %v", first["readme"])
		}
	})
	testingT.Run("1 件: 変数の項目が入り、自動生成の password の既定値は、返さない", func(subTest *testing.T) {
		statusCode, responseBody := apiClient.call(http.MethodGet, "/templates/postgres", "")
		if statusCode != http.StatusOK || responseBody["id"] != "postgres" || responseBody["category"] != "database" {
			subTest.Fatalf("200 で postgres のはず: %d %v", statusCode, responseBody)
		}
		variables, _ := responseBody["variables"].([]any)
		byName := map[string]map[string]any{}
		for _, raw := range variables {
			variable, _ := raw.(map[string]any)
			byName[variable["name"].(string)] = variable
		}
		password := byName["password"]
		if password["type"] != "password" || password["generate"] != true || password["default"] != nil || password["required"] != false {
			subTest.Errorf("password は、generate で、default は null、必須でない: %v", password)
		}
		storage := byName["storage"]
		if storage["type"] != "integer" || storage["default"] != "1024" || storage["min"] != float64(256) || storage["max"] != float64(5120) || storage["required"] != false {
			subTest.Errorf("storage の項目が違う: %v", storage)
		}
		database := byName["database"]
		if database["pattern"] != "^[a-z][a-z0-9_]{0,30}$" || database["default"] != "app" {
			subTest.Errorf("database の項目が違う: %v", database)
		}
		// choice の options は、無くても、null ではなく、空の配列。
		if options, isArray := database["options"].([]any); !isArray || len(options) != 0 {
			subTest.Errorf("options は空の配列のはず: %v", database["options"])
		}
	})
	testingT.Run("無い id は 404", func(subTest *testing.T) {
		statusCode, responseBody := apiClient.call(http.MethodGet, "/templates/nope", "")
		if statusCode != http.StatusNotFound || errorCodeOf(responseBody) != "not_found" {
			subTest.Errorf("404 not_found のはず: %d %v", statusCode, responseBody)
		}
	})
	testingT.Run("認証できないときは、一覧も 1 件も 401", func(subTest *testing.T) {
		anonymous := newProjectAPIClient(subTest, "", Dependencies{})
		for _, path := range []string{"/templates", "/templates/nginx"} {
			statusCode, responseBody := anonymous.call(http.MethodGet, path, "")
			if statusCode != http.StatusUnauthorized || errorCodeOf(responseBody) != "unauthorized" {
				subTest.Errorf("401 unauthorized のはず(%s): %d %v", path, statusCode, responseBody)
			}
		}
	})
}
