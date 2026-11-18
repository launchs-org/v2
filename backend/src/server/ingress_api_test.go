package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"

	"launchs/shared/database/testdatabase"
	"launchs/shared/repository"
)

// fakeIngressWorkflowStarter は、テスト用の IngressWorkflowStarter。開始を頼まれた ID を覚え、決めたエラーを返す。
type fakeIngressWorkflowStarter struct {
	appliedIngressIDs   []string
	deletedIngressIDs   []string
	reappliedIngressIDs []string
	startError          error
}

// StartRulesApply は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeIngressWorkflowStarter) StartRulesApply(_ context.Context, ingressID string) error {
	fakeStarter.appliedIngressIDs = append(fakeStarter.appliedIngressIDs, ingressID)
	return fakeStarter.startError
}

// StartRouteReapply は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeIngressWorkflowStarter) StartRouteReapply(_ context.Context, ingressID string) error {
	fakeStarter.reappliedIngressIDs = append(fakeStarter.reappliedIngressIDs, ingressID)
	return fakeStarter.startError
}

// StartIngressDeletion は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeIngressWorkflowStarter) StartIngressDeletion(_ context.Context, ingressID string) error {
	fakeStarter.deletedIngressIDs = append(fakeStarter.deletedIngressIDs, ingressID)
	return fakeStarter.startError
}

// fakeHostGenerator は、テスト用のホスト名の払い出し。決めた順にホスト名を返し、尽きたら最後のものを返し続ける。
type fakeHostGenerator struct {
	hosts []string
	calls int
}

// Generate は、次のホスト名を返す。
func (generator *fakeHostGenerator) Generate() (string, error) {
	index := generator.calls
	if index >= len(generator.hosts) {
		index = len(generator.hosts) - 1
	}
	generator.calls++
	return generator.hosts[index], nil
}

// ingressTestEnvironment は、ingress の API のテストに必要なものの一式。
type ingressTestEnvironment struct {
	database    *gorm.DB
	starter     *fakeIngressWorkflowStarter
	generator   *fakeHostGenerator
	ownerClient *projectAPIClient
	otherClient *projectAPIClient
	projectID   string
	basePath    string
	serviceID   string
}

// newIngressTestEnvironment は、プロジェクトに、active の service(web の 8080 の tcp、game の 27015 の udp)がある状態を用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - 用意した環境。
func newIngressTestEnvironment(testingT *testing.T) *ingressTestEnvironment {
	// 依存するものを差し替えて、2人のユーザーのクライアントを作る。
	database := testdatabase.New(testingT)
	starter := &fakeIngressWorkflowStarter{}
	generator := &fakeHostGenerator{hosts: []string{"brave-blue-fox.launchs.org"}}
	dependencies := Dependencies{
		Database: database, DeployWorkflowStarter: &fakeDeployWorkflowStarter{}, ServiceWorkflowStarter: &fakeServiceWorkflowStarter{},
		IngressWorkflowStarter: starter, IngressHostGenerator: generator,
	}
	ownerClient := newProjectAPIClient(testingT, "user-a", dependencies)
	otherClient := newProjectAPIClient(testingT, "user-b", dependencies)
	// プロジェクトと deploy を作り、その deploy に service を作って、active にする。
	projectID := createProjectFor(ownerClient, "my-app")
	deployID := createDeployFor(ownerClient, projectID)
	_, serviceBody := ownerClient.call(http.MethodPost, "/deploy/"+deployID+"/service",
		`{"ports":[{"name":"web","port":8080,"protocol":"tcp"},{"name":"game","port":27015,"protocol":"udp"}]}`)
	serviceID := serviceBody["serviceId"].(string)
	database.Exec("UPDATE services SET status = 'active' WHERE id = ?", serviceID)
	return &ingressTestEnvironment{
		database: database, starter: starter, generator: generator, ownerClient: ownerClient, otherClient: otherClient,
		projectID: projectID, basePath: "/projects/" + projectID + "/ingresses", serviceID: serviceID,
	}
}

// createIngress は、ingress を1つ作って、ID を返す。
//
// 引数:
//   - environment: テストの環境。
//
// 戻り値:
//   - ingress の ID。
func createIngress(environment *ingressTestEnvironment) string {
	// 持ち主として作る。
	_, body := environment.ownerClient.call(http.MethodPost, environment.basePath, "")
	return body["ingressId"].(string)
}

// rulesBody は、ルールが1つのボディを作る。
//
// 引数:
//   - path: path。
//   - serviceID: 流す先の service の ID。
//   - port: ポート番号。
//
// 戻り値:
//   - リクエストのボディ。
func rulesBody(path string, serviceID string, port int) string {
	// 1つのルールを持つ JSON にする。
	return fmt.Sprintf(`{"rules":[{"path":"%s","serviceId":"%s","port":%d}]}`, path, serviceID, port)
}

// TestIngressAPICreateListGet は、作成(ホスト名の払い出し)・一覧・取得を確認する。
func TestIngressAPICreateListGet(testingT *testing.T) {
	testingT.Run("作成すると、host が払い出され、rules は空の配列、pending_rules は null、active になる", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.basePath, "")
		rules, rulesIsArray := body["rules"].([]any)
		if statusCode != 200 || !strings.HasPrefix(body["ingressId"].(string), "ingress-") || body["host"] != "brave-blue-fox.launchs.org" ||
			!rulesIsArray || len(rules) != 0 || body["pending_rules"] != nil || body["status"] != "active" {
			subTest.Fatalf("仕様の形のはず: %d %v", statusCode, body)
		}
		if len(environment.starter.appliedIngressIDs) != 0 {
			subTest.Errorf("作成では Workflow を開始しないはず: %v", environment.starter.appliedIngressIDs)
		}
	})
	testingT.Run("ホスト名が重なったときは、作り直して作成できる", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		environment.generator.hosts = []string{"brave-blue-fox.launchs.org", "brave-blue-fox.launchs.org", "calm-red-owl.launchs.org"}
		createIngress(environment)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.basePath, "")
		if statusCode != 200 || body["host"] != "calm-red-owl.launchs.org" {
			subTest.Errorf("重ならない名前で作成されるはず: %d %v", statusCode, body)
		}
	})
	testingT.Run("存在しない・他人のプロジェクトは 404、削除中のプロジェクトは 409", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		missingStatus, _ := environment.ownerClient.call(http.MethodPost, "/projects/project-nothing/ingresses", "")
		otherStatus, _ := environment.otherClient.call(http.MethodPost, environment.basePath, "")
		environment.database.Exec("UPDATE projects SET status = 'deleting' WHERE id = ?", environment.projectID)
		deletingStatus, deletingBody := environment.ownerClient.call(http.MethodPost, environment.basePath, "")
		if missingStatus != 404 || otherStatus != 404 || deletingStatus != 409 || errorCodeOf(deletingBody) != "conflict" {
			subTest.Errorf("404、404、409 のはず: %d %d %d", missingStatus, otherStatus, deletingStatus)
		}
	})
	testingT.Run("一覧は、新しい順で、ingressId と host だけを返し、ページングできる。他人は 404", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		environment.generator.hosts = []string{"a-a-a.launchs.org", "b-b-b.launchs.org", "c-c-c.launchs.org"}
		for count := 0; count < 3; count++ {
			createIngress(environment)
		}
		statusCode, body := environment.ownerClient.call(http.MethodGet, environment.basePath+"?limit=2", "")
		items, _ := body["ingresses"].([]any)
		if statusCode != 200 || len(items) != 2 || body["nextCursor"] == nil {
			subTest.Fatalf("2件と次のページがあるはず: %d %v", statusCode, body)
		}
		first := items[0].(map[string]any)
		if len(first) != 2 || first["host"] != "c-c-c.launchs.org" {
			subTest.Errorf("新しい順で、ingressId と host だけのはず: %v", first)
		}
		_, nextBody := environment.ownerClient.call(http.MethodGet, environment.basePath+"?limit=2&cursor="+body["nextCursor"].(string), "")
		nextItems, _ := nextBody["ingresses"].([]any)
		if len(nextItems) != 1 || nextBody["nextCursor"] != nil {
			subTest.Errorf("最後の1件で、nextCursor は null のはず: %v", nextBody)
		}
		if otherStatus, _ := environment.otherClient.call(http.MethodGet, environment.basePath, ""); otherStatus != 404 {
			subTest.Errorf("他人は 404 のはず: %d", otherStatus)
		}
		if badStatus, _ := environment.ownerClient.call(http.MethodGet, environment.basePath+"?limit=0", ""); badStatus != 400 {
			subTest.Errorf("limit が範囲外なら 400 のはず: %d", badStatus)
		}
	})
	testingT.Run("取得は詳細を返し、他人と存在しない ID は 404", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		ingressID := createIngress(environment)
		statusCode, body := environment.ownerClient.call(http.MethodGet, environment.basePath+"/"+ingressID, "")
		if statusCode != 200 || body["ingressId"] != ingressID || body["status"] != "active" {
			subTest.Errorf("詳細を返すはず: %d %v", statusCode, body)
		}
		otherStatus, _ := environment.otherClient.call(http.MethodGet, environment.basePath+"/"+ingressID, "")
		missingStatus, _ := environment.ownerClient.call(http.MethodGet, environment.basePath+"/ingress-nothing", "")
		if otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("404 のはず: %d %d", otherStatus, missingStatus)
		}
	})
}

// TestIngressAPIUpdateRules は、ルールの更新(PUT)の、保存・入力の検査・流す先の検査・拒否を確認する。
func TestIngressAPIUpdateRules(testingT *testing.T) {
	testingT.Run("pending_rules に保存され、rules は変わらず、Workflow は開始されない。空の配列も保存できる", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		ingressID := createIngress(environment)
		path := environment.basePath + "/" + ingressID
		statusCode, body := environment.ownerClient.call(http.MethodPut, path, rulesBody("/", environment.serviceID, 8080))
		if statusCode != 200 || len(body) != 0 {
			subTest.Fatalf("200 と空のオブジェクトのはず: %d %v", statusCode, body)
		}
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		pending, _ := detail["pending_rules"].([]any)
		rules, _ := detail["rules"].([]any)
		if len(pending) != 1 || len(rules) != 0 || len(environment.starter.appliedIngressIDs) != 0 {
			subTest.Fatalf("pending だけが入るはず: %v", detail)
		}
		// 現在(空)と同じ内容を送ると、pending は消える。
		environment.ownerClient.call(http.MethodPut, path, `{"rules":[]}`)
		_, again := environment.ownerClient.call(http.MethodGet, path, "")
		if again["pending_rules"] != nil {
			subTest.Errorf("現在と同じ内容なら pending は null のはず: %v", again)
		}
	})
	for _, testCase := range []struct{ name, body string }{
		{"rules が未指定なら 400", `{}`},
		{"JSON でないなら 400", `not json`},
		{"path が / で始まらないと 400", `{"rules":[{"path":"api","serviceId":"service-x","port":8080}]}`},
		{"path に空白や引用符があると 400", "{\"rules\":[{\"path\":\"/a b\",\"serviceId\":\"service-x\",\"port\":8080}]}"},
		{"path に ` があると 400", "{\"rules\":[{\"path\":\"/a`b\",\"serviceId\":\"service-x\",\"port\":8080}]}"},
		{"port が範囲外なら 400", `{"rules":[{"path":"/","serviceId":"service-x","port":70000}]}`},
		{"serviceId の形が違うと 400", `{"rules":[{"path":"/","serviceId":"nope","port":8080}]}`},
	} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			environment := newIngressTestEnvironment(subTest)
			ingressID := createIngress(environment)
			if statusCode, _ := environment.ownerClient.call(http.MethodPut, environment.basePath+"/"+ingressID, testCase.body); statusCode != 400 {
				subTest.Errorf("400 のはず: %d", statusCode)
			}
		})
	}
	testingT.Run("path の重複と、ルールが51件は 400", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		path := environment.basePath + "/" + createIngress(environment)
		duplicate := fmt.Sprintf(`{"rules":[{"path":"/","serviceId":"%[1]s","port":8080},{"path":"/","serviceId":"%[1]s","port":8080}]}`, environment.serviceID)
		items := []string{}
		for index := 0; index < 51; index++ {
			items = append(items, fmt.Sprintf(`{"path":"/p%d","serviceId":"%s","port":8080}`, index, environment.serviceID))
		}
		tooMany := `{"rules":[` + strings.Join(items, ",") + `]}`
		duplicateStatus, _ := environment.ownerClient.call(http.MethodPut, path, duplicate)
		tooManyStatus, _ := environment.ownerClient.call(http.MethodPut, path, tooMany)
		if duplicateStatus != 400 || tooManyStatus != 400 {
			subTest.Errorf("400 のはず: %d %d", duplicateStatus, tooManyStatus)
		}
	})
	testingT.Run("流す先が不正なら 400(保存されない): udp のポート、無いポート、無い service、別のプロジェクトの service", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		path := environment.basePath + "/" + createIngress(environment)
		// 別のプロジェクトに、service を作る。
		otherProjectID := createProjectFor(environment.ownerClient, "other-app")
		otherDeployID := createDeployFor(environment.ownerClient, otherProjectID)
		_, otherService := environment.ownerClient.call(http.MethodPost, "/deploy/"+otherDeployID+"/service", `{"ports":[{"name":"web","port":8080,"protocol":"tcp"}]}`)
		for name, body := range map[string]string{
			"udp":               rulesBody("/", environment.serviceID, 27015),
			"無いポート":             rulesBody("/", environment.serviceID, 9999),
			"無い service":        rulesBody("/", "service-00000000-0000-4000-8000-000000000000", 8080),
			"別のプロジェクトの service": rulesBody("/", otherService["serviceId"].(string), 8080),
		} {
			statusCode, responseBody := environment.ownerClient.call(http.MethodPut, path, body)
			if statusCode != 400 || errorCodeOf(responseBody) != "invalid_request" {
				subTest.Errorf("%s: 400 のはず: %d %v", name, statusCode, responseBody)
			}
		}
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		if detail["pending_rules"] != nil {
			subTest.Errorf("保存されないはず: %v", detail)
		}
	})
	for _, blocked := range []string{"applying", "deleting"} {
		testingT.Run("ingress が "+blocked+" の間は 409", func(subTest *testing.T) {
			environment := newIngressTestEnvironment(subTest)
			ingressID := createIngress(environment)
			environment.database.Exec("UPDATE ingresses SET status = ?", blocked)
			if statusCode, _ := environment.ownerClient.call(http.MethodPut, environment.basePath+"/"+ingressID, rulesBody("/", environment.serviceID, 8080)); statusCode != 409 {
				subTest.Errorf("409 のはず: %d", statusCode)
			}
		})
	}
	testingT.Run("failed の ingress は、ルールを直して保存できる。他人は 404", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		ingressID := createIngress(environment)
		environment.database.Exec("UPDATE ingresses SET status = 'failed'")
		statusCode, _ := environment.ownerClient.call(http.MethodPut, environment.basePath+"/"+ingressID, rulesBody("/", environment.serviceID, 8080))
		otherStatus, _ := environment.otherClient.call(http.MethodPut, environment.basePath+"/"+ingressID, rulesBody("/", environment.serviceID, 8080))
		if statusCode != 200 || otherStatus != 404 {
			subTest.Errorf("200 と 404 のはず: %d %d", statusCode, otherStatus)
		}
	})
}

// TestIngressAPIApplyDiscard は、適用(apply)と破棄(discard)の、状態の遷移・拒否・開始の失敗時の巻き戻しを確認する。
func TestIngressAPIApplyDiscard(testingT *testing.T) {
	testingT.Run("apply は applying にして Workflow を開始する。pending は、結果が確定するまで消費しない", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		ingressID := createIngress(environment)
		path := environment.basePath + "/" + ingressID
		environment.ownerClient.call(http.MethodPut, path, rulesBody("/", environment.serviceID, 8080))
		statusCode, body := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		pending, _ := detail["pending_rules"].([]any)
		if statusCode != 200 || len(body) != 0 || detail["status"] != "applying" || len(pending) != 1 {
			subTest.Fatalf("applying で pending が残るはず: %d %v", statusCode, detail)
		}
		if len(environment.starter.appliedIngressIDs) != 1 || environment.starter.appliedIngressIDs[0] != ingressID {
			subTest.Errorf("その ID で Workflow が開始されるはず: %v", environment.starter.appliedIngressIDs)
		}
		// applying の間は、多重 apply・更新・破棄・削除が 409。
		for name, status := range map[string]int{
			"apply":   statusOf(environment, http.MethodPost, path+"/apply", ""),
			"discard": statusOf(environment, http.MethodPost, path+"/discard", ""),
			"update":  statusOf(environment, http.MethodPut, path, rulesBody("/x", environment.serviceID, 8080)),
			"delete":  statusOf(environment, http.MethodDelete, path, ""),
		} {
			if status != 409 {
				subTest.Errorf("%s は 409 のはず: %d", name, status)
			}
		}
	})
	testingT.Run("pending が無いと apply と discard は 409", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		path := environment.basePath + "/" + createIngress(environment)
		applyStatus, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		discardStatus, _ := environment.ownerClient.call(http.MethodPost, path+"/discard", "")
		if applyStatus != 409 || discardStatus != 409 {
			subTest.Errorf("409 のはず: %d %d", applyStatus, discardStatus)
		}
	})
	testingT.Run("更新のあとに service のポートが無くなっていたら、apply は 409(applying にならない)", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		path := environment.basePath + "/" + createIngress(environment)
		environment.ownerClient.call(http.MethodPut, path, rulesBody("/", environment.serviceID, 8080))
		environment.database.Exec(`UPDATE services SET ports = '[{"name":"other","port":9090,"protocol":"tcp"}]'`)
		statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		if statusCode != 409 || detail["status"] != "active" || len(environment.starter.appliedIngressIDs) != 0 {
			subTest.Errorf("409 で、active のままのはず: %d %v", statusCode, detail)
		}
	})
	testingT.Run("Workflow を開始できなかったら、500 で、状態を元に戻す(pending は残る)", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		path := environment.basePath + "/" + createIngress(environment)
		environment.ownerClient.call(http.MethodPut, path, rulesBody("/", environment.serviceID, 8080))
		environment.starter.startError = errors.New("temporal が落ちている")
		statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		pending, _ := detail["pending_rules"].([]any)
		if statusCode != 500 || detail["status"] != "active" || len(pending) != 1 {
			subTest.Errorf("500 で、active に戻り、pending が残るはず: %d %v", statusCode, detail)
		}
	})
	testingT.Run("discard は pending を消す(rules は変わらない)。failed からも、apply し直せる", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		path := environment.basePath + "/" + createIngress(environment)
		environment.ownerClient.call(http.MethodPut, path, rulesBody("/", environment.serviceID, 8080))
		discardStatus, _ := environment.ownerClient.call(http.MethodPost, path+"/discard", "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		if discardStatus != 200 || detail["pending_rules"] != nil {
			subTest.Fatalf("pending が消えるはず: %d %v", discardStatus, detail)
		}
		environment.ownerClient.call(http.MethodPut, path, rulesBody("/", environment.serviceID, 8080))
		environment.database.Exec("UPDATE ingresses SET status = 'failed'")
		if applyStatus, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", ""); applyStatus != 200 {
			subTest.Errorf("failed からも apply できるはず: %d", applyStatus)
		}
	})
	testingT.Run("他人と存在しない ingress は 404", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		path := environment.basePath + "/" + createIngress(environment)
		for _, suffix := range []string{"/apply", "/discard"} {
			otherStatus, _ := environment.otherClient.call(http.MethodPost, path+suffix, "")
			missingStatus, _ := environment.ownerClient.call(http.MethodPost, environment.basePath+"/ingress-nothing"+suffix, "")
			if otherStatus != 404 || missingStatus != 404 {
				subTest.Errorf("%s は 404 のはず: %d %d", suffix, otherStatus, missingStatus)
			}
		}
	})
}

// statusOf は、持ち主として呼び出して、ステータスコードだけを返す。
//
// 引数:
//   - environment: テストの環境。
//   - method: HTTP のメソッド。
//   - path: パス。
//   - body: ボディ。
//
// 戻り値:
//   - ステータスコード。
func statusOf(environment *ingressTestEnvironment, method string, path string, body string) int {
	// 呼び出して、ステータスコードだけを取り出す。
	statusCode, _ := environment.ownerClient.call(method, path, body)
	return statusCode
}

// TestIngressAPIDelete は、削除の受け付け・拒否・開始の失敗時の巻き戻しを確認する。
func TestIngressAPIDelete(testingT *testing.T) {
	testingT.Run("削除すると deleting になり、Workflow が開始される。その間の操作は 409、取得は deleting", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		ingressID := createIngress(environment)
		path := environment.basePath + "/" + ingressID
		statusCode, body := environment.ownerClient.call(http.MethodDelete, path, "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		if statusCode != 200 || len(body) != 0 || detail["status"] != "deleting" {
			subTest.Fatalf("deleting になるはず: %d %v", statusCode, detail)
		}
		if len(environment.starter.deletedIngressIDs) != 1 || environment.starter.deletedIngressIDs[0] != ingressID {
			subTest.Errorf("削除の Workflow が開始されるはず: %v", environment.starter.deletedIngressIDs)
		}
		if again := statusOf(environment, http.MethodDelete, path, ""); again != 409 {
			subTest.Errorf("削除中の再削除は 409 のはず: %d", again)
		}
		if update := statusOf(environment, http.MethodPut, path, rulesBody("/", environment.serviceID, 8080)); update != 409 {
			subTest.Errorf("削除中の更新は 409 のはず: %d", update)
		}
	})
	testingT.Run("failed の ingress は、もう一度削除できる", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		ingressID := createIngress(environment)
		environment.database.Exec("UPDATE ingresses SET status = 'failed'")
		if statusCode, _ := environment.ownerClient.call(http.MethodDelete, environment.basePath+"/"+ingressID, ""); statusCode != 200 {
			subTest.Errorf("200 のはず: %d", statusCode)
		}
	})
	testingT.Run("Workflow を開始できなかったら、500 で、状態を元に戻す", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		ingressID := createIngress(environment)
		environment.starter.startError = errors.New("temporal が落ちている")
		statusCode, _ := environment.ownerClient.call(http.MethodDelete, environment.basePath+"/"+ingressID, "")
		_, detail := environment.ownerClient.call(http.MethodGet, environment.basePath+"/"+ingressID, "")
		if statusCode != 500 || detail["status"] != "active" {
			subTest.Errorf("500 で、active に戻るはず: %d %v", statusCode, detail)
		}
	})
	testingT.Run("他人と存在しない ingress は 404", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		ingressID := createIngress(environment)
		otherStatus, _ := environment.otherClient.call(http.MethodDelete, environment.basePath+"/"+ingressID, "")
		missingStatus, _ := environment.ownerClient.call(http.MethodDelete, environment.basePath+"/ingress-nothing", "")
		if otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("404 のはず: %d %d", otherStatus, missingStatus)
		}
	})
	testingT.Run("プロジェクトのレコードを消すと、配下の ingress も消える", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		createIngress(environment)
		environment.database.Exec("UPDATE projects SET status = 'deleting' WHERE id = ?", environment.projectID)
		if recordError := repository.NewProjectRepository(environment.database).DeleteRecord(context.Background(), environment.projectID); recordError != nil {
			subTest.Fatalf("削除できるはず: %v", recordError)
		}
		var count int64
		environment.database.Table("ingresses").Count(&count)
		if count != 0 {
			subTest.Errorf("ingress も消えるはず: %d", count)
		}
	})
}

// TestIngressAPIAccessStats は、アクセスの集計の切り替え(PATCH)を確認する。
func TestIngressAPIAccessStats(testingT *testing.T) {
	testingT.Run("作成直後は有効で、無効にできる(ルールが無ければ、IngressRoute の作り直しは開始しない)", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		ingressID := createIngress(environment)
		path := environment.basePath + "/" + ingressID
		_, created := environment.ownerClient.call(http.MethodGet, path, "")
		if created["accessStats"] != true {
			subTest.Fatalf("既定は有効のはず: %v", created)
		}
		statusCode, body := environment.ownerClient.call(http.MethodPatch, path, `{"accessStats":false}`)
		if statusCode != 200 || body["accessStats"] != false {
			subTest.Fatalf("無効になるはず: %d %v", statusCode, body)
		}
		_, fetched := environment.ownerClient.call(http.MethodGet, path, "")
		if fetched["accessStats"] != false || len(environment.starter.reappliedIngressIDs) != 0 {
			subTest.Errorf("保存され、作り直しは開始しないはず: %v %v", fetched, environment.starter.reappliedIngressIDs)
		}
	})
	testingT.Run("反映済みのルールがあれば、IngressRoute の作り直しを開始する(同じ値でも)", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		ingressID := createIngress(environment)
		path := environment.basePath + "/" + ingressID
		environment.database.Exec("UPDATE ingresses SET rules = ? WHERE id = ?", `[{"path":"/","serviceId":"`+environment.serviceID+`","port":8080}]`, ingressID)
		for range 2 {
			if statusCode, _ := environment.ownerClient.call(http.MethodPatch, path, `{"accessStats":true}`); statusCode != 200 {
				subTest.Fatalf("200 のはず: %d", statusCode)
			}
		}
		if len(environment.starter.reappliedIngressIDs) != 2 {
			subTest.Errorf("2回開始するはず: %v", environment.starter.reappliedIngressIDs)
		}
	})
	testingT.Run("accessStats が無いと 400、他人は 404、反映中は 409", func(subTest *testing.T) {
		environment := newIngressTestEnvironment(subTest)
		ingressID := createIngress(environment)
		path := environment.basePath + "/" + ingressID
		if statusCode, _ := environment.ownerClient.call(http.MethodPatch, path, `{}`); statusCode != 400 {
			subTest.Errorf("400 のはず: %d", statusCode)
		}
		if statusCode, _ := environment.otherClient.call(http.MethodPatch, path, `{"accessStats":false}`); statusCode != 404 {
			subTest.Errorf("404 のはず: %d", statusCode)
		}
		environment.database.Exec("UPDATE ingresses SET status = 'applying' WHERE id = ?", ingressID)
		if statusCode, _ := environment.ownerClient.call(http.MethodPatch, path, `{"accessStats":false}`); statusCode != 409 {
			subTest.Errorf("409 のはず: %d", statusCode)
		}
	})
}
