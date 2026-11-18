package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"gorm.io/gorm"

	"launchs/shared/database/testdatabase"
	"launchs/shared/repository"
	"launchs/shared/template"
)

// applyTestTemplate は、テンプレートの実行のテストに使う、volume、configmap、deploy(+service)、ingress を持つテンプレート。
const applyTestTemplate = `
apiVersion: launchs/v1
kind: Template
metadata:
  id: sample
  name: サンプル
variables:
  - name: password
    label: パスワード
    type: password
    generate: true
  - name: storage
    label: 大きさ
    type: integer
    default: "512"
    min: 256
    max: 1024
resources:
  - kind: volume
    name: data
    sizeMB: ${storage}
  - kind: configmap
    name: conf
    files:
      - key: a.conf
        content: "pw=${password}\n"
  - kind: deploy
    name: web
    image: nginx:alpine
    spec: small
    env:
      PASSWORD: ${password}
    mounts: [{ volume: data, path: /data }]
    configmaps: [{ configmap: conf, key: a.conf, path: /etc/a.conf }]
    service:
      ports: [{ name: http, port: 80, protocol: tcp }]
  - kind: ingress
    rules: [{ path: /, deploy: web, port: 80 }]
`

// fakeTemplateApplyStarter は、テスト用の TemplateApplyStarter。開始を頼まれた ID を覚え、決めたエラーを返す。
type fakeTemplateApplyStarter struct {
	startedApplyIDs []string
	startError      error
}

// StartTemplateApply は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeTemplateApplyStarter) StartTemplateApply(_ context.Context, applyID string) error {
	fakeStarter.startedApplyIDs = append(fakeStarter.startedApplyIDs, applyID)
	return fakeStarter.startError
}

// templateApplyEnvironment は、テンプレートの実行の API のテストに必要なものの一式。
type templateApplyEnvironment struct {
	database    *gorm.DB
	starter     *fakeTemplateApplyStarter
	ownerClient *projectAPIClient
	otherClient *projectAPIClient
	projectID   string
	applyPath   string
}

// newTemplateApplyEnvironment は、テスト用のテンプレートと、持ち主のプロジェクトを用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - 用意した環境。
func newTemplateApplyEnvironment(testingT *testing.T) *templateApplyEnvironment {
	// テスト用のテンプレートを、1 つだけ持つ一覧を作る。
	registry, loadError := template.Load(fstest.MapFS{"sample/template.yaml": {Data: []byte(applyTestTemplate)}})
	if loadError != nil {
		testingT.Fatal(loadError)
	}
	database := testdatabase.New(testingT)
	starter := &fakeTemplateApplyStarter{}
	dependencies := Dependencies{Database: database, TemplateRegistry: registry, TemplateApplyStarter: starter, IngressHostGenerator: &fakeHostGenerator{hosts: []string{"a-b-c.example.test"}}}
	ownerClient := newProjectAPIClient(testingT, "user-a", dependencies)
	otherClient := newProjectAPIClient(testingT, "user-b", dependencies)
	projectID := createProjectFor(ownerClient, "my-app")
	return &templateApplyEnvironment{database: database, starter: starter, ownerClient: ownerClient, otherClient: otherClient, projectID: projectID, applyPath: "/projects/" + projectID + "/templates/sample/apply"}
}

// TestTemplateApplyAPI は、受け付け(計画の保存、値の補い、Workflow の開始)、進み具合の取得、検査、持ち主、クォータ、失敗時の扱いを確認する。
func TestTemplateApplyAPI(testingT *testing.T) {
	testingT.Run("受け付けると、実行の ID と、自動生成を含む全ての値を返し、Workflow を開始する。進み具合は、全て待機中", func(subTest *testing.T) {
		environment := newTemplateApplyEnvironment(subTest)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.applyPath, `{"values":{"storage":"768"}}`)
		values, _ := body["values"].(map[string]any)
		applyID, _ := body["applyId"].(string)
		password, _ := values["password"].(string)
		if statusCode != 200 || !strings.HasPrefix(applyID, "apply-") || values["storage"] != "768" || len(password) != 24 {
			subTest.Fatalf("実行の ID と、値(password は 24 文字)のはず: %d %v", statusCode, body)
		}
		if len(environment.starter.startedApplyIDs) != 1 || environment.starter.startedApplyIDs[0] != applyID {
			subTest.Errorf("Workflow が、1 回開始されるはず: %v", environment.starter.startedApplyIDs)
		}
		_, progress := environment.ownerClient.call(http.MethodGet, "/projects/"+environment.projectID+"/template-applies/"+applyID, "")
		steps, _ := progress["steps"].([]any)
		if progress["status"] != "running" || progress["templateId"] != "sample" || progress["error"] != nil || len(steps) != 5 {
			subTest.Fatalf("running で、5 ステップのはず: %v", progress)
		}
		var kinds []string
		for _, raw := range steps {
			step := raw.(map[string]any)
			kinds = append(kinds, step["kind"].(string)+":"+step["name"].(string))
			if step["status"] != "waiting" || step["resourceId"] != "" || step["error"] != nil {
				subTest.Errorf("待機中のはず: %v", step)
			}
		}
		if got := strings.Join(kinds, " "); got != "volume:data configmap:conf deploy:web service:web ingress:ingress" {
			subTest.Errorf("作る順のはず: %s", got)
		}
		if progressValues, _ := progress["values"].(map[string]any); progressValues["password"] != password {
			subTest.Errorf("進み具合にも、同じ値が入るはず: %v", progress["values"])
		}
	})
	testingT.Run("計画が保存され、パスワードは env と ConfigMap に展開され、ID が決まっている", func(subTest *testing.T) {
		environment := newTemplateApplyEnvironment(subTest)
		_, body := environment.ownerClient.call(http.MethodPost, environment.applyPath, `{}`)
		password := body["values"].(map[string]any)["password"].(string)
		var plan string
		environment.database.Raw("SELECT plan::text FROM template_applies WHERE id = ?", body["applyId"]).Scan(&plan)
		// jsonb の文字列表現は、「": "」と空白が入る。
		for _, want := range []string{`"PASSWORD": "` + password + `"`, `pw=` + password, `"id": "deploy-`, `"id": "volume-`, `"id": "configmap-`, `"id": "service-`, `"id": "ingress-`, `"dnsToken": "`} {
			if !strings.Contains(plan, want) {
				subTest.Errorf("計画に %q が入るはず: %s", want, plan)
			}
		}
	})
	testingT.Run("本文が無くても、全て既定で受け付ける", func(subTest *testing.T) {
		environment := newTemplateApplyEnvironment(subTest)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.applyPath, "")
		if statusCode != 200 || body["values"].(map[string]any)["storage"] != "512" {
			subTest.Errorf("既定で受け付けるはず: %d %v", statusCode, body)
		}
	})
	for _, testCase := range []struct {
		name string
		body string
		want int
	}{
		{"変数が、範囲外なら 400", `{"values":{"storage":"2000"}}`, 400},
		{"変数が、小さすぎても 400", `{"values":{"storage":"10"}}`, 400},
		{"整数でないと 400", `{"values":{"storage":"abc"}}`, 400},
		{"テンプレートに無い変数は 400", `{"values":{"nothing":"x"}}`, 400},
		{"JSON でないと 400", `not json`, 400},
		{"変数が、境界(最大)なら成功", `{"values":{"storage":"1024"}}`, 200},
	} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			environment := newTemplateApplyEnvironment(subTest)
			if statusCode, _ := environment.ownerClient.call(http.MethodPost, environment.applyPath, testCase.body); statusCode != testCase.want {
				subTest.Errorf("期待 %d、実際 %d", testCase.want, statusCode)
			}
		})
	}
	testingT.Run("無いテンプレート、無い・他人のプロジェクトは 404。削除中のプロジェクトは 409", func(subTest *testing.T) {
		environment := newTemplateApplyEnvironment(subTest)
		missingTemplate, _ := environment.ownerClient.call(http.MethodPost, "/projects/"+environment.projectID+"/templates/nothing/apply", `{}`)
		missingProject, _ := environment.ownerClient.call(http.MethodPost, "/projects/project-nothing/templates/sample/apply", `{}`)
		otherUser, _ := environment.otherClient.call(http.MethodPost, environment.applyPath, `{}`)
		environment.database.Exec("UPDATE projects SET status = 'deleting' WHERE id = ?", environment.projectID)
		deleting, _ := environment.ownerClient.call(http.MethodPost, environment.applyPath, `{}`)
		if missingTemplate != 404 || missingProject != 404 || otherUser != 404 || deleting != 409 {
			subTest.Errorf("404、404、404、409 のはず: %d %d %d %d", missingTemplate, missingProject, otherUser, deleting)
		}
		if len(environment.starter.startedApplyIDs) != 0 {
			subTest.Errorf("失敗した受け付けでは、Workflow を開始しないはず: %v", environment.starter.startedApplyIDs)
		}
	})
	testingT.Run("クォータを超えるときは、409 で、何も保存せず、Workflow も開始しない", func(subTest *testing.T) {
		for name, statement := range map[string]string{
			"deploy の数":      "UPDATE quota_defaults SET max_deploys = 0",
			"volume の数":      "UPDATE quota_defaults SET max_volumes = 0",
			"volume の大きさの合計": "UPDATE quota_defaults SET max_volume_total_mb = 100",
		} {
			environment := newTemplateApplyEnvironment(subTest)
			environment.database.Exec(statement)
			statusCode, body := environment.ownerClient.call(http.MethodPost, environment.applyPath, `{}`)
			var count int64
			environment.database.Table("template_applies").Count(&count)
			if statusCode != 409 || errorCodeOf(body) != "conflict" || count != 0 || len(environment.starter.startedApplyIDs) != 0 {
				subTest.Errorf("%s: 409 で、何も作らないはず: %d %v 保存=%d", name, statusCode, body, count)
			}
		}
	})
	testingT.Run("ConfigMap が、プロジェクトの上限(50)を超えるときは、409", func(subTest *testing.T) {
		environment := newTemplateApplyEnvironment(subTest)
		for index := 0; index < 50; index++ {
			environment.database.Exec("INSERT INTO configmaps (id, project_id, user_id, name) VALUES (?, ?, 'user-a', ?)", fmt.Sprintf("configmap-%d", index), environment.projectID, fmt.Sprintf("c%d", index))
		}
		if statusCode, _ := environment.ownerClient.call(http.MethodPost, environment.applyPath, `{}`); statusCode != 409 {
			subTest.Errorf("409 のはず: %d", statusCode)
		}
	})
	testingT.Run("Workflow を開始できなかったら、500 で、実行は failed になる(running のまま残さない)", func(subTest *testing.T) {
		environment := newTemplateApplyEnvironment(subTest)
		environment.starter.startError = fmt.Errorf("temporal down")
		statusCode, _ := environment.ownerClient.call(http.MethodPost, environment.applyPath, `{}`)
		var status string
		environment.database.Raw("SELECT status FROM template_applies").Scan(&status)
		if statusCode != 500 || status != "failed" {
			subTest.Errorf("500 で、failed のはず: %d %s", statusCode, status)
		}
	})
	testingT.Run("進み具合は、他人と、無い ID は 404", func(subTest *testing.T) {
		environment := newTemplateApplyEnvironment(subTest)
		_, body := environment.ownerClient.call(http.MethodPost, environment.applyPath, `{}`)
		path := "/projects/" + environment.projectID + "/template-applies/" + body["applyId"].(string)
		otherUser, _ := environment.otherClient.call(http.MethodGet, path, "")
		missing, _ := environment.ownerClient.call(http.MethodGet, "/projects/"+environment.projectID+"/template-applies/apply-nothing", "")
		otherProject := createProjectFor(environment.ownerClient, "other-app")
		crossProject, _ := environment.ownerClient.call(http.MethodGet, "/projects/"+otherProject+"/template-applies/"+body["applyId"].(string), "")
		if otherUser != 404 || missing != 404 || crossProject != 404 {
			subTest.Errorf("404 のはず: %d %d %d", otherUser, missing, crossProject)
		}
	})
	testingT.Run("プロジェクトを削除すると、実行の記録も消える", func(subTest *testing.T) {
		environment := newTemplateApplyEnvironment(subTest)
		environment.ownerClient.call(http.MethodPost, environment.applyPath, `{}`)
		environment.database.Exec("UPDATE projects SET status = 'deleting' WHERE id = ?", environment.projectID)
		if deleteError := repository.NewProjectRepository(environment.database).DeleteRecord(context.Background(), environment.projectID); deleteError != nil {
			subTest.Fatal(deleteError)
		}
		var count int64
		environment.database.Table("template_applies").Count(&count)
		if count != 0 {
			subTest.Errorf("消えるはず: %d", count)
		}
	})
}
