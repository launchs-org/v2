package server

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// newActiveServiceTestEnvironment は、deploy(active)に、active の service(ポートは web の 8080 の tcp)がある状態を用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - 用意した環境。
func newActiveServiceTestEnvironment(testingT *testing.T) *serviceTestEnvironment {
	// deploy を active にして、service を作り、Workflow が作れたあとの状態(active)にする。
	environment := newServiceTestEnvironment(testingT)
	environment.database.Exec("UPDATE deploys SET status = 'active' WHERE id = ?", environment.deployID)
	environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
	environment.database.Exec("UPDATE services SET status = 'active', dns_name = 'dns', cluster_ip = '10.0.0.1'")
	return environment
}

// newPorts は、web(8080、tcp)と game(27015、udp)の、新しいポートのボディ。
const newPorts = `{"ports":[{"name":"web","port":8080,"protocol":"tcp"},{"name":"game","port":27015,"protocol":"udp"}]}`

// serviceBody は、service を取得して、ボディを返す。
//
// 引数:
//   - environment: テストの環境。
//
// 戻り値:
//   - service のレスポンスのボディ。
func serviceBody(environment *serviceTestEnvironment) map[string]any {
	// 持ち主として取得する。
	_, body := environment.ownerClient.call(http.MethodGet, environment.servicePath, "")
	return body
}

// deployStatus は、deploy の status を返す。
//
// 引数:
//   - environment: テストの環境。
//
// 戻り値:
//   - deploy の status。
func deployStatus(environment *serviceTestEnvironment) string {
	// 持ち主として deploy の詳細を取得する。
	_, body := environment.ownerClient.call(http.MethodGet, "/deploy/"+environment.deployID, "")
	return body["status"].(string)
}

// TestServiceAPIUpdatePorts は、ポートの更新(PATCH /deploy/:deployId/service)の、保存・同じ内容・入力の検査・拒否を確認する。
func TestServiceAPIUpdatePorts(testingT *testing.T) {
	testingT.Run("pending_ports に保存され、現在の ports は変わらず、Workflow は開始されない。もう一度送ると上書きされる", func(subTest *testing.T) {
		environment := newActiveServiceTestEnvironment(subTest)
		statusCode, body := environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
		if statusCode != 200 || len(body) != 0 {
			subTest.Fatalf("200 と空のオブジェクトのはず: %d %v", statusCode, body)
		}
		current := serviceBody(environment)
		ports, _ := current["ports"].([]any)
		pending, _ := current["pending_ports"].([]any)
		if len(ports) != 1 || len(pending) != 2 || current["status"] != "active" {
			subTest.Fatalf("ports は1つのまま、pending_ports が2つのはず: %v", current)
		}
		if len(environment.starter.appliedServiceIDs) != 0 {
			subTest.Errorf("反映は、apply を呼ぶまで開始されないはず: %v", environment.starter.appliedServiceIDs)
		}
		environment.ownerClient.call(http.MethodPatch, environment.servicePath, `{"ports":[{"name":"metrics","port":9090,"protocol":"tcp"}]}`)
		overwritten, _ := serviceBody(environment)["pending_ports"].([]any)
		if len(overwritten) != 1 || overwritten[0].(map[string]any)["name"] != "metrics" {
			subTest.Errorf("上書きされるはず: %v", overwritten)
		}
	})
	testingT.Run("現在と同じ内容なら、pending_ports は null のまま(以前の pending も消える)で、apply も 409", func(subTest *testing.T) {
		environment := newActiveServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
		statusCode, _ := environment.ownerClient.call(http.MethodPatch, environment.servicePath, webPorts)
		if statusCode != 200 || serviceBody(environment)["pending_ports"] != nil {
			subTest.Fatalf("200 で、pending_ports は null のはず: %d", statusCode)
		}
		if applyStatus, _ := environment.ownerClient.call(http.MethodPost, environment.servicePath+"/apply", ""); applyStatus != 409 {
			subTest.Errorf("同じ内容は apply できない(409)のはず: %d", applyStatus)
		}
	})
	for _, testCase := range []struct{ name, body string }{
		{"ports が未指定なら 400", `{}`},
		{"ports が空の配列なら 400", `{"ports":[]}`},
		{"JSON でないボディは 400", `not json`},
		{"protocol が不正なら 400", `{"ports":[{"name":"web","port":8080,"protocol":"http"}]}`},
		{"port が範囲外なら 400", `{"ports":[{"name":"web","port":65536,"protocol":"tcp"}]}`},
		{"name が不正なら 400", `{"ports":[{"name":"Web_1","port":8080,"protocol":"tcp"}]}`},
		{"name が重複すると 400", `{"ports":[{"name":"a","port":1,"protocol":"tcp"},{"name":"a","port":2,"protocol":"tcp"}]}`},
		{"同じ port と protocol の組が重複すると 400", `{"ports":[{"name":"a","port":1,"protocol":"tcp"},{"name":"b","port":1,"protocol":"tcp"}]}`},
	} {
		testingT.Run(testCase.name+"(保存されない)", func(subTest *testing.T) {
			environment := newActiveServiceTestEnvironment(subTest)
			statusCode, _ := environment.ownerClient.call(http.MethodPatch, environment.servicePath, testCase.body)
			if statusCode != 400 || serviceBody(environment)["pending_ports"] != nil {
				subTest.Errorf("400 で、保存されないはず: %d", statusCode)
			}
		})
	}
	testingT.Run("同じ port で protocol が違うものは、保存できる(DNS の 53 番など)", func(subTest *testing.T) {
		environment := newActiveServiceTestEnvironment(subTest)
		statusCode, _ := environment.ownerClient.call(http.MethodPatch, environment.servicePath,
			`{"ports":[{"name":"dns-tcp","port":53,"protocol":"tcp"},{"name":"dns-udp","port":53,"protocol":"udp"}]}`)
		if statusCode != 200 {
			subTest.Errorf("200 のはず: %d", statusCode)
		}
	})
	for _, blocked := range []string{"applying", "deleting", "delete_failed"} {
		testingT.Run("deploy が "+blocked+" の間は 409", func(subTest *testing.T) {
			environment := newActiveServiceTestEnvironment(subTest)
			environment.database.Exec("UPDATE deploys SET status = ? WHERE id = ?", blocked, environment.deployID)
			statusCode, body := environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
			if statusCode != 409 || errorCodeOf(body) != "conflict" {
				subTest.Errorf("409 のはず: %d %v", statusCode, body)
			}
		})
	}
	for _, blocked := range []string{"pending", "failed", "deleting"} {
		testingT.Run("service が "+blocked+" なら 409", func(subTest *testing.T) {
			environment := newActiveServiceTestEnvironment(subTest)
			environment.database.Exec("UPDATE services SET status = ?", blocked)
			if statusCode, _ := environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts); statusCode != 409 {
				subTest.Errorf("409 のはず: %d", statusCode)
			}
		})
	}
	testingT.Run("service が無い deploy、他人の deploy、存在しない deploy は 404", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		noService, _ := environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
		environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		otherStatus, _ := environment.otherClient.call(http.MethodPatch, environment.servicePath, newPorts)
		missingStatus, _ := environment.ownerClient.call(http.MethodPatch, "/deploy/deploy-nothing/service", newPorts)
		if noService != 404 || otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("全て 404 のはず: %d %d %d", noService, otherStatus, missingStatus)
		}
	})
}

// TestServiceAPIApplyPorts は、ポートの適用(POST .../service/apply)の、受け付け・拒否・開始の失敗を確認する。
func TestServiceAPIApplyPorts(testingT *testing.T) {
	testingT.Run("適用できると、deploy が applying になり、pending_ports は保持され、service の status は変わらず、Workflow が開始される", func(subTest *testing.T) {
		environment := newActiveServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
		service := serviceBody(environment)

		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.servicePath+"/apply", "")

		if statusCode != 200 || len(body) != 0 || deployStatus(environment) != "applying" {
			subTest.Fatalf("200 で、deploy は applying のはず: %d %v %s", statusCode, body, deployStatus(environment))
		}
		after := serviceBody(environment)
		if pending, _ := after["pending_ports"].([]any); len(pending) != 2 || after["status"] != "active" {
			subTest.Errorf("pending_ports は保持され、service の status は active のままのはず: %v", after)
		}
		if len(environment.starter.appliedServiceIDs) != 1 || environment.starter.appliedServiceIDs[0] != service["serviceId"] {
			subTest.Errorf("その service の ID で、Workflow が1回開始されるはず: %v", environment.starter.appliedServiceIDs)
		}
	})
	testingT.Run("applying 中の再度の apply は 409(多重 apply 不可)で、Workflow は増えない", func(subTest *testing.T) {
		environment := newActiveServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
		environment.ownerClient.call(http.MethodPost, environment.servicePath+"/apply", "")
		statusCode, _ := environment.ownerClient.call(http.MethodPost, environment.servicePath+"/apply", "")
		if statusCode != 409 || len(environment.starter.appliedServiceIDs) != 1 {
			subTest.Errorf("409 で、増えないはず: %d %v", statusCode, environment.starter.appliedServiceIDs)
		}
	})
	testingT.Run("pending_ports が null なら 409 で、deploy の状態は変わらない", func(subTest *testing.T) {
		environment := newActiveServiceTestEnvironment(subTest)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.servicePath+"/apply", "")
		if statusCode != 409 || errorCodeOf(body) != "conflict" || deployStatus(environment) != "active" || len(environment.starter.appliedServiceIDs) != 0 {
			subTest.Errorf("409 で、何も変わらないはず: %d %v %s", statusCode, body, deployStatus(environment))
		}
	})
	for _, allowed := range []string{"failed", "scaled_zero"} {
		testingT.Run("deploy が "+allowed+" でも適用できる", func(subTest *testing.T) {
			environment := newActiveServiceTestEnvironment(subTest)
			environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
			environment.database.Exec("UPDATE deploys SET status = ? WHERE id = ?", allowed, environment.deployID)
			if statusCode, _ := environment.ownerClient.call(http.MethodPost, environment.servicePath+"/apply", ""); statusCode != 200 {
				subTest.Errorf("200 のはず: %d", statusCode)
			}
		})
	}
	for _, blocked := range []string{"pending", "deleting", "delete_failed"} {
		testingT.Run("deploy が "+blocked+" なら 409", func(subTest *testing.T) {
			environment := newActiveServiceTestEnvironment(subTest)
			environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
			environment.database.Exec("UPDATE deploys SET status = ? WHERE id = ?", blocked, environment.deployID)
			if statusCode, _ := environment.ownerClient.call(http.MethodPost, environment.servicePath+"/apply", ""); statusCode != 409 || len(environment.starter.appliedServiceIDs) != 0 {
				subTest.Errorf("409 のはず: %d", statusCode)
			}
		})
	}
	testingT.Run("service が active でなければ 409", func(subTest *testing.T) {
		environment := newActiveServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
		environment.database.Exec("UPDATE services SET status = 'failed'")
		if statusCode, _ := environment.ownerClient.call(http.MethodPost, environment.servicePath+"/apply", ""); statusCode != 409 {
			subTest.Errorf("409 のはず: %d", statusCode)
		}
	})
	testingT.Run("service が無い deploy、他人の deploy、存在しない deploy は 404", func(subTest *testing.T) {
		environment := newActiveServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
		otherStatus, _ := environment.otherClient.call(http.MethodPost, environment.servicePath+"/apply", "")
		missingStatus, _ := environment.ownerClient.call(http.MethodPost, "/deploy/deploy-nothing/service/apply", "")
		if otherStatus != 404 || missingStatus != 404 || len(environment.starter.appliedServiceIDs) != 0 {
			subTest.Errorf("どちらも 404 のはず: %d %d", otherStatus, missingStatus)
		}
		noService := newServiceTestEnvironment(subTest)
		if statusCode, _ := noService.ownerClient.call(http.MethodPost, noService.servicePath+"/apply", ""); statusCode != 404 {
			subTest.Errorf("service が無ければ 404 のはず: %d", statusCode)
		}
	})
	testingT.Run("Workflow を開始できなかったら、500 で、deploy の状態は元に戻り、pending_ports は残る", func(subTest *testing.T) {
		environment := newActiveServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
		environment.database.Exec("UPDATE deploys SET status = 'scaled_zero' WHERE id = ?", environment.deployID)
		environment.starter.startError = errors.New("temporal down")
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.servicePath+"/apply", "")
		if statusCode != 500 || errorCodeOf(body) != "internal_error" {
			subTest.Fatalf("500 のはず: %d %v", statusCode, body)
		}
		if deployStatus(environment) != "scaled_zero" || serviceBody(environment)["pending_ports"] == nil {
			subTest.Errorf("scaled_zero に戻り、pending_ports は残るはず: %s %v", deployStatus(environment), serviceBody(environment))
		}
	})
}

// TestServiceAPIDiscardPorts は、ポートの破棄(POST .../service/discard)の、成功と拒否を確認する。
func TestServiceAPIDiscardPorts(testingT *testing.T) {
	testingT.Run("pending_ports が null に戻り、ports は変わらない。もう一度は 409", func(subTest *testing.T) {
		environment := newActiveServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.servicePath+"/discard", "")
		after := serviceBody(environment)
		ports, _ := after["ports"].([]any)
		if statusCode != 200 || len(body) != 0 || after["pending_ports"] != nil || len(ports) != 1 {
			subTest.Fatalf("200 で、pending_ports は null、ports は変わらないはず: %d %v", statusCode, after)
		}
		if secondStatus, _ := environment.ownerClient.call(http.MethodPost, environment.servicePath+"/discard", ""); secondStatus != 409 {
			subTest.Errorf("2回目は 409 のはず: %d", secondStatus)
		}
	})
	for _, blocked := range []string{"applying", "deleting", "delete_failed"} {
		testingT.Run("deploy が "+blocked+" の間は 409 で、pending_ports は残る", func(subTest *testing.T) {
			environment := newActiveServiceTestEnvironment(subTest)
			environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
			environment.database.Exec("UPDATE deploys SET status = ? WHERE id = ?", blocked, environment.deployID)
			statusCode, _ := environment.ownerClient.call(http.MethodPost, environment.servicePath+"/discard", "")
			if statusCode != 409 || serviceBody(environment)["pending_ports"] == nil {
				subTest.Errorf("409 で、pending_ports は残るはず: %d", statusCode)
			}
		})
	}
	testingT.Run("service が無い deploy、他人の deploy、存在しない deploy は 404", func(subTest *testing.T) {
		environment := newActiveServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPatch, environment.servicePath, newPorts)
		otherStatus, _ := environment.otherClient.call(http.MethodPost, environment.servicePath+"/discard", "")
		missingStatus, _ := environment.ownerClient.call(http.MethodPost, "/deploy/deploy-nothing/service/discard", "")
		noService := newServiceTestEnvironment(subTest)
		noServiceStatus, _ := noService.ownerClient.call(http.MethodPost, noService.servicePath+"/discard", "")
		if otherStatus != 404 || missingStatus != 404 || noServiceStatus != 404 {
			subTest.Errorf("全て 404 のはず: %d %d %d", otherStatus, missingStatus, noServiceStatus)
		}
	})
}

// TestServicePortsAPIRequiresAuthentication は、認証できないとき、ポートの更新・適用・破棄が 401 を返すことを確認する。
func TestServicePortsAPIRequiresAuthentication(testingT *testing.T) {
	// DEV_USER_ID が空のサーバーを使う。
	apiClient := newProjectAPIClient(testingT, "", Dependencies{})
	for _, request := range []struct{ method, path, body string }{
		{http.MethodPatch, "/deploy/deploy-x/service", newPorts},
		{http.MethodPost, "/deploy/deploy-x/service/apply", ""},
		{http.MethodPost, "/deploy/deploy-x/service/discard", ""},
	} {
		testingT.Run(strings.TrimPrefix(request.path, "/deploy/deploy-x/"), func(subTest *testing.T) {
			statusCode, responseBody := apiClient.call(request.method, request.path, request.body)
			if statusCode != http.StatusUnauthorized || errorCodeOf(responseBody) != "unauthorized" {
				subTest.Errorf("401 unauthorized のはず: %d %v", statusCode, responseBody)
			}
		})
	}
}
