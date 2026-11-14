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
)

// fakeServiceWorkflowStarter は、テスト用の ServiceWorkflowStarter。開始を頼まれた ID を覚え、決めたエラーを返す。
type fakeServiceWorkflowStarter struct {
	createdServiceIDs []string
	deletedServiceIDs []string
	appliedServiceIDs []string
	startError        error
}

// StartServiceCreation は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeServiceWorkflowStarter) StartServiceCreation(_ context.Context, serviceID string) error {
	fakeStarter.createdServiceIDs = append(fakeStarter.createdServiceIDs, serviceID)
	return fakeStarter.startError
}

// StartPortsApply は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeServiceWorkflowStarter) StartPortsApply(_ context.Context, serviceID string) error {
	fakeStarter.appliedServiceIDs = append(fakeStarter.appliedServiceIDs, serviceID)
	return fakeStarter.startError
}

// StartServiceDeletion は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeServiceWorkflowStarter) StartServiceDeletion(_ context.Context, serviceID string) error {
	fakeStarter.deletedServiceIDs = append(fakeStarter.deletedServiceIDs, serviceID)
	return fakeStarter.startError
}

// serviceTestEnvironment は、service の API のテストに使う、持ち主と他のユーザーの道具と、deploy の ID をまとめたもの。
type serviceTestEnvironment struct {
	database    *gorm.DB
	starter     *fakeServiceWorkflowStarter
	ownerClient *projectAPIClient
	otherClient *projectAPIClient
	deployID    string
	servicePath string
}

// newServiceTestEnvironment は、使い捨てのデータベースに、持ち主 user-a の deploy を1つ作った状態を用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - 用意した環境。
func newServiceTestEnvironment(testingT *testing.T) *serviceTestEnvironment {
	// 持ち主と他のユーザーが、同じデータベースを使う。
	database := testdatabase.New(testingT)
	starter := &fakeServiceWorkflowStarter{}
	dependencies := Dependencies{Database: database, DeployWorkflowStarter: &fakeDeployWorkflowStarter{}, ServiceWorkflowStarter: starter}
	ownerClient := newProjectAPIClient(testingT, "user-a", dependencies)
	otherClient := newProjectAPIClient(testingT, "user-b", dependencies)
	deployID := createDeployFor(ownerClient, createProjectFor(ownerClient, "my-app"))
	return &serviceTestEnvironment{database: database, starter: starter, ownerClient: ownerClient, otherClient: otherClient, deployID: deployID, servicePath: "/deploy/" + deployID + "/service"}
}

// webPorts は、よく使うポートのボディ(web の 8080 の tcp 1つ)。
const webPorts = `{"ports":[{"name":"web","port":8080,"protocol":"tcp"}]}`

// TestServiceAPICreate は、service の作成(POST /deploy/:deployId/service)の、正常系と、入力の検査を確認する。
func TestServiceAPICreate(testingT *testing.T) {
	testingT.Run("複数のポート(tcp と udp)で作成でき、pending で、DNS 名と ClusterIP は null で、Workflow が開始される", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.servicePath,
			`{"ports":[{"name":"web","port":8080,"protocol":"tcp"},{"name":"game","port":27015,"protocol":"udp"}]}`)
		if statusCode != 200 {
			subTest.Fatalf("200 のはず: %d %v", statusCode, body)
		}
		serviceID, _ := body["serviceId"].(string)
		ports, _ := body["ports"].([]any)
		if !strings.HasPrefix(serviceID, "service-") || body["status"] != "pending" || body["pending_ports"] != nil || body["dnsName"] != nil || body["clusterIP"] != nil || len(ports) != 2 {
			subTest.Errorf("仕様の形のはず: %v", body)
		}
		udp, _ := ports[1].(map[string]any)
		if udp["name"] != "game" || udp["port"] != float64(27015) || udp["protocol"] != "udp" {
			subTest.Errorf("2つ目は game の 27015 の udp のはず: %v", udp)
		}
		if len(environment.starter.createdServiceIDs) != 1 || environment.starter.createdServiceIDs[0] != serviceID {
			subTest.Errorf("作成の Workflow が、その ID で1回開始されるはず: %v", environment.starter.createdServiceIDs)
		}
	})

	// 入力の検査。
	manyPorts := make([]string, 0, 21)
	for index := 1; index <= 21; index++ {
		manyPorts = append(manyPorts, fmt.Sprintf(`{"name":"p%d","port":%d,"protocol":"tcp"}`, index, 1000+index))
	}
	validationCases := []struct {
		name               string
		body               string
		expectedStatusCode int
	}{
		{"ports が未指定なら 400", `{}`, 400},
		{"ports が null なら 400", `{"ports":null}`, 400},
		{"ports が空の配列なら 400", `{"ports":[]}`, 400},
		{"JSON でないボディは 400", `not json`, 400},
		{"protocol が tcp と udp 以外なら 400", `{"ports":[{"name":"web","port":8080,"protocol":"http"}]}`, 400},
		{"protocol が大文字なら 400", `{"ports":[{"name":"web","port":8080,"protocol":"TCP"}]}`, 400},
		{"protocol が未指定なら 400", `{"ports":[{"name":"web","port":8080}]}`, 400},
		{"port が 0 なら 400", `{"ports":[{"name":"web","port":0,"protocol":"tcp"}]}`, 400},
		{"port が負なら 400", `{"ports":[{"name":"web","port":-1,"protocol":"tcp"}]}`, 400},
		{"port が 65536 なら 400", `{"ports":[{"name":"web","port":65536,"protocol":"tcp"}]}`, 400},
		{"port が 65535 なら成功", `{"ports":[{"name":"web","port":65535,"protocol":"tcp"}]}`, 200},
		{"port が 1 なら成功", `{"ports":[{"name":"web","port":1,"protocol":"tcp"}]}`, 200},
		{"port が文字列なら 400", `{"ports":[{"name":"web","port":"8080","protocol":"tcp"}]}`, 400},
		{"name が未指定なら 400", `{"ports":[{"port":8080,"protocol":"tcp"}]}`, 400},
		{"name が 16 文字なら 400", `{"ports":[{"name":"abcdefghijklmnop","port":8080,"protocol":"tcp"}]}`, 400},
		{"name が 15 文字なら成功", `{"ports":[{"name":"abcdefghijklmno","port":8080,"protocol":"tcp"}]}`, 200},
		{"name に大文字があると 400", `{"ports":[{"name":"Web","port":8080,"protocol":"tcp"}]}`, 400},
		{"name にアンダースコアがあると 400", `{"ports":[{"name":"my_web","port":8080,"protocol":"tcp"}]}`, 400},
		{"name がハイフンで始まると 400", `{"ports":[{"name":"-web","port":8080,"protocol":"tcp"}]}`, 400},
		{"name がハイフンで終わると 400", `{"ports":[{"name":"web-","port":8080,"protocol":"tcp"}]}`, 400},
		{"name にハイフンが連続すると 400", `{"ports":[{"name":"a--b","port":8080,"protocol":"tcp"}]}`, 400},
		{"name に英字が1つも無い(数字だけ)と 400", `{"ports":[{"name":"8080","port":8080,"protocol":"tcp"}]}`, 400},
		{"name にハイフンと数字が混ざっても、英字があれば成功", `{"ports":[{"name":"web-1","port":8080,"protocol":"tcp"}]}`, 200},
		{"同じ配列で name が重複すると 400", `{"ports":[{"name":"web","port":8080,"protocol":"tcp"},{"name":"web","port":9090,"protocol":"tcp"}]}`, 400},
		{"同じ port と protocol の組が重複すると 400", `{"ports":[{"name":"a","port":8080,"protocol":"tcp"},{"name":"b","port":8080,"protocol":"tcp"}]}`, 400},
		{"同じ port でも protocol が違えば成功(DNS の 53 番など)", `{"ports":[{"name":"dns-tcp","port":53,"protocol":"tcp"},{"name":"dns-udp","port":53,"protocol":"udp"}]}`, 200},
		{"ポートが 21 個なら 400", `{"ports":[` + strings.Join(manyPorts, ",") + `]}`, 400},
		{"ポートが 20 個なら成功", `{"ports":[` + strings.Join(manyPorts[:20], ",") + `]}`, 200},
	}
	for _, testCase := range validationCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			environment := newServiceTestEnvironment(subTest)
			statusCode, body := environment.ownerClient.call(http.MethodPost, environment.servicePath, testCase.body)
			if statusCode != testCase.expectedStatusCode {
				subTest.Errorf("期待 %d、実際 %d、ボディ %v", testCase.expectedStatusCode, statusCode, body)
			}
			// 失敗したときは、保存も、Workflow の開始も、されない。
			var count int64
			environment.database.Table("services").Count(&count)
			if testCase.expectedStatusCode != 200 && (count != 0 || len(environment.starter.createdServiceIDs) != 0) {
				subTest.Errorf("保存も開始もされないはず: %d %v", count, environment.starter.createdServiceIDs)
			}
		})
	}
}

// TestServiceAPICreateRejections は、service の作成が、状態と持ち主によって拒否されることを確認する。
func TestServiceAPICreateRejections(testingT *testing.T) {
	testingT.Run("すでに service がある deploy には、409(1 deploy = 1 service)で、Workflow は増えない", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		if statusCode != 409 || errorCodeOf(body) != "conflict" || len(environment.starter.createdServiceIDs) != 1 {
			subTest.Errorf("409 で、Workflow は1回のままのはず: %d %v %v", statusCode, body, environment.starter.createdServiceIDs)
		}
	})
	for _, blocked := range []string{"deleting", "delete_failed"} {
		testingT.Run("deploy が "+blocked+" の間は 409", func(subTest *testing.T) {
			environment := newServiceTestEnvironment(subTest)
			environment.database.Exec("UPDATE deploys SET status = ? WHERE id = ?", blocked, environment.deployID)
			statusCode, body := environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
			if statusCode != 409 || errorCodeOf(body) != "conflict" || len(environment.starter.createdServiceIDs) != 0 {
				subTest.Errorf("409 で、開始されないはず: %d %v", statusCode, body)
			}
		})
	}
	testingT.Run("他のユーザーの deploy と、存在しない deploy は 404", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		otherStatus, _ := environment.otherClient.call(http.MethodPost, environment.servicePath, webPorts)
		missingStatus, _ := environment.ownerClient.call(http.MethodPost, "/deploy/deploy-nothing/service", webPorts)
		if otherStatus != 404 || missingStatus != 404 || len(environment.starter.createdServiceIDs) != 0 {
			subTest.Errorf("どちらも 404 のはず: %d %d", otherStatus, missingStatus)
		}
	})
	testingT.Run("Workflow を開始できなかったら、500 で、保存は取り消され、もう一度作れる", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		environment.starter.startError = errors.New("temporal down")
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		var count int64
		environment.database.Table("services").Count(&count)
		if statusCode != 500 || errorCodeOf(body) != "internal_error" || count != 0 {
			subTest.Fatalf("500 で、service が残らないはず: %d %v %d", statusCode, body, count)
		}
		environment.starter.startError = nil
		if retryStatus, _ := environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts); retryStatus != 200 {
			subTest.Errorf("取り消されているので、もう一度作れるはず: %d", retryStatus)
		}
	})
}

// TestServiceAPIGet は、service の取得(GET /deploy/:deployId/service)の、正常系と、404 を確認する。
func TestServiceAPIGet(testingT *testing.T) {
	testingT.Run("作成直後は pending で、DNS 名と ClusterIP は null。作れたあとは、値が入る", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		_, created := environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		statusCode, body := environment.ownerClient.call(http.MethodGet, environment.servicePath, "")
		if statusCode != 200 || body["serviceId"] != created["serviceId"] || body["status"] != "pending" || body["dnsName"] != nil || body["clusterIP"] != nil {
			subTest.Fatalf("pending のはず: %d %v", statusCode, body)
		}
		// Workflow が作れたあとの状態にする。
		environment.database.Exec("UPDATE services SET status = 'active', dns_name = 'service-x.project-x.svc.test.local', cluster_ip = '10.112.0.5'")
		_, active := environment.ownerClient.call(http.MethodGet, environment.servicePath, "")
		if active["status"] != "active" || active["dnsName"] != "service-x.project-x.svc.test.local" || active["clusterIP"] != "10.112.0.5" || active["pending_ports"] != nil {
			subTest.Errorf("active で、値が入るはず: %v", active)
		}
	})
	testingT.Run("deploy が削除中なら、service の status は deleting として返る", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		environment.database.Exec("UPDATE services SET status = 'active'")
		environment.database.Exec("UPDATE deploys SET status = 'deleting' WHERE id = ?", environment.deployID)
		_, body := environment.ownerClient.call(http.MethodGet, environment.servicePath, "")
		if body["status"] != "deleting" {
			subTest.Errorf("deleting のはず: %v", body)
		}
	})
	testingT.Run("service が無い deploy、他人の deploy、存在しない deploy は 404", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		noService, _ := environment.ownerClient.call(http.MethodGet, environment.servicePath, "")
		environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		otherStatus, _ := environment.otherClient.call(http.MethodGet, environment.servicePath, "")
		missingStatus, _ := environment.ownerClient.call(http.MethodGet, "/deploy/deploy-nothing/service", "")
		if noService != 404 || otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("全て 404 のはず: %d %d %d", noService, otherStatus, missingStatus)
		}
	})
}

// TestServiceAPIDelete は、service の削除(DELETE /deploy/:deployId/service)の、受け付けと拒否を確認する。
func TestServiceAPIDelete(testingT *testing.T) {
	testingT.Run("active の service は、200 で deleting になり、削除の Workflow がその ID で開始される。再度の削除は 409", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		_, created := environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		environment.database.Exec("UPDATE services SET status = 'active'")
		statusCode, body := environment.ownerClient.call(http.MethodDelete, environment.servicePath, "")
		_, current := environment.ownerClient.call(http.MethodGet, environment.servicePath, "")
		if statusCode != 200 || len(body) != 0 || current["status"] != "deleting" ||
			len(environment.starter.deletedServiceIDs) != 1 || environment.starter.deletedServiceIDs[0] != created["serviceId"] {
			subTest.Fatalf("200 で deleting、開始1回のはず: %d %v %v %v", statusCode, body, current, environment.starter.deletedServiceIDs)
		}
		if secondStatus, _ := environment.ownerClient.call(http.MethodDelete, environment.servicePath, ""); secondStatus != 409 || len(environment.starter.deletedServiceIDs) != 1 {
			subTest.Errorf("2回目は 409 で、増えないはず: %d", secondStatus)
		}
	})
	testingT.Run("作成中(pending)の service は 409", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		statusCode, body := environment.ownerClient.call(http.MethodDelete, environment.servicePath, "")
		if statusCode != 409 || errorCodeOf(body) != "conflict" || len(environment.starter.deletedServiceIDs) != 0 {
			subTest.Errorf("409 のはず: %d %v", statusCode, body)
		}
	})
	testingT.Run("failed の service は、もう一度削除できる(やり直し)", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		environment.database.Exec("UPDATE services SET status = 'failed'")
		if statusCode, _ := environment.ownerClient.call(http.MethodDelete, environment.servicePath, ""); statusCode != 200 || len(environment.starter.deletedServiceIDs) != 1 {
			subTest.Errorf("200 で、開始されるはず: %d", statusCode)
		}
	})
	for _, blocked := range []string{"deleting", "delete_failed"} {
		testingT.Run("deploy が "+blocked+" の間は 409(deploy の削除が、service も片付ける)", func(subTest *testing.T) {
			environment := newServiceTestEnvironment(subTest)
			environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
			environment.database.Exec("UPDATE services SET status = 'active'")
			environment.database.Exec("UPDATE deploys SET status = ? WHERE id = ?", blocked, environment.deployID)
			if statusCode, _ := environment.ownerClient.call(http.MethodDelete, environment.servicePath, ""); statusCode != 409 || len(environment.starter.deletedServiceIDs) != 0 {
				subTest.Errorf("409 のはず: %d", statusCode)
			}
		})
	}
	testingT.Run("service が無い deploy、他人の deploy、存在しない deploy は 404", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		noService, _ := environment.ownerClient.call(http.MethodDelete, environment.servicePath, "")
		environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		environment.database.Exec("UPDATE services SET status = 'active'")
		otherStatus, _ := environment.otherClient.call(http.MethodDelete, environment.servicePath, "")
		missingStatus, _ := environment.ownerClient.call(http.MethodDelete, "/deploy/deploy-nothing/service", "")
		if noService != 404 || otherStatus != 404 || missingStatus != 404 || len(environment.starter.deletedServiceIDs) != 0 {
			subTest.Errorf("全て 404 で、削除されないはず: %d %d %d", noService, otherStatus, missingStatus)
		}
	})
	testingT.Run("Workflow を開始できなかったら、500 で、状態は元に戻る", func(subTest *testing.T) {
		environment := newServiceTestEnvironment(subTest)
		environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
		environment.database.Exec("UPDATE services SET status = 'failed'")
		environment.starter.startError = errors.New("temporal down")
		statusCode, body := environment.ownerClient.call(http.MethodDelete, environment.servicePath, "")
		_, current := environment.ownerClient.call(http.MethodGet, environment.servicePath, "")
		if statusCode != 500 || errorCodeOf(body) != "internal_error" || current["status"] != "failed" {
			subTest.Errorf("500 で、failed に戻るはず: %d %v %v", statusCode, body, current)
		}
	})
}

// TestServiceAPIRequiresAuthentication は、認証できないとき、service の API が 401 を返すことを確認する。
func TestServiceAPIRequiresAuthentication(testingT *testing.T) {
	// DEV_USER_ID が空のサーバーを使う。
	apiClient := newProjectAPIClient(testingT, "", Dependencies{})
	for _, request := range []struct{ method, body string }{{http.MethodPost, webPorts}, {http.MethodGet, ""}, {http.MethodDelete, ""}} {
		testingT.Run(request.method, func(subTest *testing.T) {
			statusCode, responseBody := apiClient.call(request.method, "/deploy/deploy-x/service", request.body)
			if statusCode != http.StatusUnauthorized || errorCodeOf(responseBody) != "unauthorized" {
				subTest.Errorf("401 unauthorized のはず: %d %v", statusCode, responseBody)
			}
		})
	}
}

// TestServiceAPIDeletesWithDeploy は、deploy の行を削除すると、service の行も一緒に消える(連鎖)ことを、API 越しに確認する。
func TestServiceAPIDeletesWithDeploy(testingT *testing.T) {
	environment := newServiceTestEnvironment(testingT)
	environment.ownerClient.call(http.MethodPost, environment.servicePath, webPorts)
	environment.database.Exec("DELETE FROM deploys WHERE id = ?", environment.deployID)
	var count int64
	environment.database.Table("services").Count(&count)
	if count != 0 {
		testingT.Errorf("service も消えるはず: %d", count)
	}
}
