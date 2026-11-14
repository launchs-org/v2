package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"launchs/shared/database/testdatabase"
)

// fakeDeployWorkflowStarter は、テスト用の DeployWorkflowStarter。開始を頼まれた ID を覚え、決めたエラーを返す。
type fakeDeployWorkflowStarter struct {
	startedDeployIDs []string
	appliedDeployIDs []string
	startError       error
}

// StartDeployCreation は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeDeployWorkflowStarter) StartDeployCreation(_ context.Context, deployID string) error {
	fakeStarter.startedDeployIDs = append(fakeStarter.startedDeployIDs, deployID)
	return fakeStarter.startError
}

// StartConfigApply は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeDeployWorkflowStarter) StartConfigApply(_ context.Context, deployID string) error {
	fakeStarter.appliedDeployIDs = append(fakeStarter.appliedDeployIDs, deployID)
	return fakeStarter.startError
}

// createProjectFor は、プロジェクトを1つ作って、その ID を返す。
//
// 引数:
//   - apiClient: プロジェクトの持ち主として呼ぶ道具。
//   - name: プロジェクトの名前。
//
// 戻り値:
//   - 作成したプロジェクトの ID。
func createProjectFor(apiClient *projectAPIClient, name string) string {
	// プロジェクトを作成する API を呼び、projectId を取り出す。
	_, createdBody := apiClient.call(http.MethodPost, "/projects", fmt.Sprintf(`{"name":"%s"}`, name))
	return createdBody["projectId"].(string)
}

// TestDeployAPICreate は、deploy 作成(POST /deploy)の、正常・異常・境界値を確認する。
func TestDeployAPICreate(testingT *testing.T) {
	// 持ち主と他のユーザーが、同じデータベースを使う。
	database := testdatabase.New(testingT)
	fakeStarter := &fakeDeployWorkflowStarter{}
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	projectID := createProjectFor(ownerClient, "my-app")
	imageRef := "registry.launchs-org.internal/" + projectID + ":build-1"

	// 作成の入力ごとの期待。
	testCases := []struct {
		name               string
		client             *projectAPIClient
		body               string
		expectedStatusCode int
		expectedErrorCode  string
	}{
		{name: "有効な内容で作成できる", client: ownerClient, body: fmt.Sprintf(`{"projectId":"%s","imageRef":"%s","spec":"small"}`, projectID, imageRef), expectedStatusCode: 200},
		{name: "medium も作成できる", client: ownerClient, body: fmt.Sprintf(`{"projectId":"%s","imageRef":"%s","spec":"medium"}`, projectID, imageRef), expectedStatusCode: 200},
		{name: "同じ imageRef から複数回作成できる(別の deployId になる)", client: ownerClient, body: fmt.Sprintf(`{"projectId":"%s","imageRef":"%s","spec":"large"}`, projectID, imageRef), expectedStatusCode: 200},
		{name: "imageRef が未指定なら 400", client: ownerClient, body: fmt.Sprintf(`{"projectId":"%s","spec":"small"}`, projectID), expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
		{name: "imageRef に空白が含まれると 400", client: ownerClient, body: fmt.Sprintf(`{"projectId":"%s","imageRef":"a b","spec":"small"}`, projectID), expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
		{name: "imageRef が513文字なら 400", client: ownerClient, body: fmt.Sprintf(`{"projectId":"%s","imageRef":"%s","spec":"small"}`, projectID, strings.Repeat("a", 513)), expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
		{name: "spec が未指定なら 400", client: ownerClient, body: fmt.Sprintf(`{"projectId":"%s","imageRef":"%s"}`, projectID, imageRef), expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
		{name: "spec が small・medium・large 以外なら 400", client: ownerClient, body: fmt.Sprintf(`{"projectId":"%s","imageRef":"%s","spec":"huge"}`, projectID, imageRef), expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
		{name: "projectId が未指定なら 400", client: ownerClient, body: fmt.Sprintf(`{"imageRef":"%s","spec":"small"}`, imageRef), expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
		{name: "存在しない projectId は 404", client: ownerClient, body: fmt.Sprintf(`{"projectId":"project-nothing","imageRef":"%s","spec":"small"}`, imageRef), expectedStatusCode: 404, expectedErrorCode: "not_found"},
		{name: "他のユーザーの projectId は 404", client: otherClient, body: fmt.Sprintf(`{"projectId":"%s","imageRef":"%s","spec":"small"}`, projectID, imageRef), expectedStatusCode: 404, expectedErrorCode: "not_found"},
		{name: "JSON でないボディは 400", client: ownerClient, body: `not json`, expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			statusCode, responseBody := testCase.client.call(http.MethodPost, "/deploy", testCase.body)

			// ステータスコードと、エラーの code を確認する。
			if statusCode != testCase.expectedStatusCode {
				subTest.Fatalf("ステータスコードが違う: 期待 %d、実際 %d、ボディ %v", testCase.expectedStatusCode, statusCode, responseBody)
			}
			if testCase.expectedErrorCode != "" && errorCodeOf(responseBody) != testCase.expectedErrorCode {
				subTest.Errorf("code が違う: 期待 %q、実際 %v", testCase.expectedErrorCode, responseBody)
			}
			// 成功したときは、deploy- で始まる ID が返る。
			if statusCode == 200 {
				if deployID, _ := responseBody["deployId"].(string); !strings.HasPrefix(deployID, "deploy-") {
					subTest.Errorf("deployId の形が違う: %v", responseBody)
				}
			}
		})
	}

	// 成功した3回の分だけ、Workflow が開始されている(失敗した分は開始されない)。
	if len(fakeStarter.startedDeployIDs) != 3 {
		testingT.Errorf("Workflow は3回開始されるはず: %v", fakeStarter.startedDeployIDs)
	}
}

// TestDeployAPICreateRejectsDeletingProject は、削除中のプロジェクトには、409 で deploy を作れないことを確認する。
func TestDeployAPICreateRejectsDeletingProject(testingT *testing.T) {
	// プロジェクトを作って、削除を受け付けて deleting にする。
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{})
	projectID := createProjectFor(apiClient, "going")
	apiClient.call(http.MethodDelete, "/projects/"+projectID, "")

	// deploy の作成は 409 になる。
	statusCode, responseBody := apiClient.call(http.MethodPost, "/deploy", fmt.Sprintf(`{"projectId":"%s","imageRef":"registry/app:1","spec":"small"}`, projectID))

	if statusCode != 409 || errorCodeOf(responseBody) != "conflict" {
		testingT.Errorf("409 conflict のはず: %d %v", statusCode, responseBody)
	}
}

// TestDeployAPICreateWhenWorkflowCannotStart は、Workflow を開始できなかったとき、500 を返し、
// 起動されない deploy が残らない(作成が取り消される)ことを確認する。
func TestDeployAPICreateWhenWorkflowCannotStart(testingT *testing.T) {
	// 開始に必ず失敗する fake を使う。
	database := testdatabase.New(testingT)
	fakeStarter := &fakeDeployWorkflowStarter{startError: errors.New("temporal down")}
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	projectID := createProjectFor(apiClient, "my-app")

	// 作成は 500 になる。
	statusCode, responseBody := apiClient.call(http.MethodPost, "/deploy", fmt.Sprintf(`{"projectId":"%s","imageRef":"registry/app:1","spec":"small"}`, projectID))
	if statusCode != 500 || errorCodeOf(responseBody) != "internal_error" {
		testingT.Fatalf("500 internal_error のはず: %d %v", statusCode, responseBody)
	}
	// deploy が1件も残っていない。
	var deployCount int64
	database.Table("deploys").Count(&deployCount)
	if deployCount != 0 {
		testingT.Errorf("deploy が残っていないはず: %d 件", deployCount)
	}
}

// TestDeployAPIGet は、作成した deploy の詳細が、仕様の形で取得でき、他のユーザーや存在しない ID は 404 になることを確認する。
func TestDeployAPIGet(testingT *testing.T) {
	// 持ち主が deploy を1件作る。
	database := testdatabase.New(testingT)
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database})
	projectID := createProjectFor(ownerClient, "my-app")
	_, createdBody := ownerClient.call(http.MethodPost, "/deploy", fmt.Sprintf(`{"projectId":"%s","imageRef":"registry/app:1","spec":"medium"}`, projectID))
	deployID := createdBody["deployId"].(string)

	// 作成直後の詳細が、仕様の形になっている。
	testingT.Run("作成直後の詳細が、仕様の形で取得できる", func(subTest *testing.T) {
		statusCode, responseBody := ownerClient.call(http.MethodGet, "/deploy/"+deployID, "")
		if statusCode != 200 {
			subTest.Fatalf("200 のはず: %d %v", statusCode, responseBody)
		}
		// 基本の項目。
		if responseBody["deployId"] != deployID || responseBody["status"] != "pending" || responseBody["spec"] != "medium" ||
			responseBody["replicas"] != float64(1) || responseBody["imageRef"] != "registry/app:1" {
			subTest.Errorf("基本の項目が違う: %v", responseBody)
		}
		// 作成日時が、JST(+09:00)の形式。
		if createdAt, _ := responseBody["createdAt"].(string); !strings.HasSuffix(createdAt, "+09:00") {
			subTest.Errorf("createdAt が JST ではない: %v", responseBody["createdAt"])
		}
		// Pod は空の配列、service は null、volumes は空のマウント。
		if pods, isArray := responseBody["pods"].([]any); !isArray || len(pods) != 0 {
			subTest.Errorf("pods は空の配列のはず: %v", responseBody["pods"])
		}
		if responseBody["service"] != nil {
			subTest.Errorf("service は null のはず: %v", responseBody["service"])
		}
		volumes, _ := responseBody["volumes"].(map[string]any)
		if mounts, isArray := volumes["mounts"].([]any); !isArray || len(mounts) != 0 || volumes["pending_mounts"] != nil {
			subTest.Errorf("volumes が違う: %v", responseBody["volumes"])
		}
		// 起動設定は、作成直後の初期状態(env は空、command と args は null、pending は全て null)。
		config, _ := responseBody["config"].(map[string]any)
		env, _ := config["env"].(map[string]any)
		if len(env) != 0 || config["command"] != nil || config["args"] != nil ||
			config["pending_env"] != nil || config["pending_command"] != nil || config["pending_args"] != nil {
			subTest.Errorf("起動設定が初期状態ではない: %v", config)
		}
	})
	// 他のユーザーは、存在しないものとして 404 になる。
	testingT.Run("他のユーザーの deploy は 404", func(subTest *testing.T) {
		statusCode, responseBody := otherClient.call(http.MethodGet, "/deploy/"+deployID, "")
		if statusCode != 404 || errorCodeOf(responseBody) != "not_found" {
			subTest.Errorf("404 のはず: %d %v", statusCode, responseBody)
		}
	})
	// 存在しない ID は 404 になる。
	testingT.Run("存在しない deployId は 404", func(subTest *testing.T) {
		statusCode, _ := ownerClient.call(http.MethodGet, "/deploy/deploy-nothing", "")
		if statusCode != 404 {
			subTest.Errorf("404 のはず: %d", statusCode)
		}
	})
}

// TestDeployAPIRequiresAuthentication は、認証できないとき、deploy の API が 401 を返すことを確認する。
func TestDeployAPIRequiresAuthentication(testingT *testing.T) {
	// DEV_USER_ID が空のサーバーを使う。
	apiClient := newProjectAPIClient(testingT, "", Dependencies{})

	// 作成と取得が、どちらも 401 になる。
	for _, request := range []struct{ method, path, body string }{
		{http.MethodPost, "/deploy", `{"projectId":"p","imageRef":"a","spec":"small"}`},
		{http.MethodGet, "/deploy/deploy-x", ""},
		{http.MethodGet, "/deploy/deploy-x/config", ""},
		{http.MethodPut, "/deploy/deploy-x/config", `{"env":{}}`},
		{http.MethodPost, "/deploy/deploy-x/config/discard", ""},
	} {
		testingT.Run(request.method+" "+request.path, func(subTest *testing.T) {
			statusCode, responseBody := apiClient.call(request.method, request.path, request.body)
			if statusCode != http.StatusUnauthorized || errorCodeOf(responseBody) != "unauthorized" {
				subTest.Errorf("401 unauthorized のはず: %d %v", statusCode, responseBody)
			}
		})
	}
}

// createDeployFor は、deploy を1つ作って、その ID を返す。
//
// 引数:
//   - apiClient: deploy の持ち主として呼ぶ道具。
//   - projectID: deploy を作るプロジェクトの ID。
//
// 戻り値:
//   - 作成した deploy の ID。
func createDeployFor(apiClient *projectAPIClient, projectID string) string {
	// deploy を作成する API を呼び、deployId を取り出す。
	_, createdBody := apiClient.call(http.MethodPost, "/deploy", fmt.Sprintf(`{"projectId":"%s","imageRef":"registry/app:1","spec":"small"}`, projectID))
	return createdBody["deployId"].(string)
}

// TestDeployAPIConfigPutGetDiscard は、起動設定の更新・取得・破棄の流れを確認する。
func TestDeployAPIConfigPutGetDiscard(testingT *testing.T) {
	// 持ち主が deploy を1件作る。
	database := testdatabase.New(testingT)
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database})
	deployID := createDeployFor(ownerClient, createProjectFor(ownerClient, "my-app"))
	configPath := "/deploy/" + deployID + "/config"

	// 作成直後の起動設定は、初期状態(env は空、他は null)。
	testingT.Run("作成直後は初期状態で取得できる", func(subTest *testing.T) {
		statusCode, body := ownerClient.call(http.MethodGet, configPath, "")
		env, _ := body["env"].(map[string]any)
		if statusCode != 200 || len(env) != 0 || body["command"] != nil || body["args"] != nil || body["pending_env"] != nil {
			subTest.Errorf("初期状態のはず: %d %v", statusCode, body)
		}
	})
	// pending が無いときの破棄は 409。
	testingT.Run("pending が無いときの discard は 409", func(subTest *testing.T) {
		statusCode, body := ownerClient.call(http.MethodPost, configPath+"/discard", "")
		if statusCode != 409 || errorCodeOf(body) != "conflict" {
			subTest.Errorf("409 のはず: %d %v", statusCode, body)
		}
	})
	// 更新すると、pending に入り、現在の値は変わらない。
	testingT.Run("PUT すると pending_* に入り、現在の値は変わらない", func(subTest *testing.T) {
		statusCode, body := ownerClient.call(http.MethodPut, configPath, `{"env":{"PORT":"8080"},"command":"/app/server","args":["--config","/app/config.yaml"]}`)
		if statusCode != 200 || len(body) != 0 {
			subTest.Fatalf("200 と空のオブジェクトのはず: %d %v", statusCode, body)
		}
		_, configBody := ownerClient.call(http.MethodGet, configPath, "")
		pendingEnv, _ := configBody["pending_env"].(map[string]any)
		pendingArgs, _ := configBody["pending_args"].([]any)
		if pendingEnv["PORT"] != "8080" || configBody["pending_command"] != "/app/server" || len(pendingArgs) != 2 {
			subTest.Errorf("pending に入っていない: %v", configBody)
		}
		currentEnv, _ := configBody["env"].(map[string]any)
		if len(currentEnv) != 0 || configBody["command"] != nil || configBody["args"] != nil {
			subTest.Errorf("現在の値が変わっている: %v", configBody)
		}
	})
	// 再度 PUT すると、前回の pending は、新しい内容で上書きされる(command と args は null に戻せる)。
	testingT.Run("再度 PUT すると pending が全て上書きされる", func(subTest *testing.T) {
		ownerClient.call(http.MethodPut, configPath, `{"env":{"B":"2"}}`)
		_, configBody := ownerClient.call(http.MethodGet, configPath, "")
		pendingEnv, isObject := configBody["pending_env"].(map[string]any)
		if !isObject || len(pendingEnv) != 1 || pendingEnv["B"] != "2" || configBody["pending_command"] != nil || configBody["pending_args"] != nil {
			subTest.Errorf("上書きされていない: %v", configBody)
		}
	})
	// 破棄すると、pending が null に戻り、もう一度の破棄は 409 になる。
	testingT.Run("discard すると pending が全て null に戻る", func(subTest *testing.T) {
		ownerClient.call(http.MethodPut, configPath, `{"env":{"A":"1"},"command":"/x","args":["y"]}`)
		statusCode, _ := ownerClient.call(http.MethodPost, configPath+"/discard", "")
		_, configBody := ownerClient.call(http.MethodGet, configPath, "")
		if statusCode != 200 || configBody["pending_env"] != nil || configBody["pending_command"] != nil || configBody["pending_args"] != nil {
			subTest.Errorf("破棄されていない: %d %v", statusCode, configBody)
		}
		if secondStatus, _ := ownerClient.call(http.MethodPost, configPath+"/discard", ""); secondStatus != 409 {
			subTest.Errorf("2回目は 409 のはず: %d", secondStatus)
		}
	})
}

// TestDeployAPIConfigValidation は、起動設定の更新の、入力の検証を確認する。
func TestDeployAPIConfigValidation(testingT *testing.T) {
	// deploy を1件用意する。
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{})
	deployID := createDeployFor(apiClient, createProjectFor(apiClient, "my-app"))

	// 入力ごとの期待。
	manyEnv := make([]string, 0, 101)
	for number := 0; number < 101; number++ {
		manyEnv = append(manyEnv, fmt.Sprintf(`"K%d":"v"`, number))
	}
	testCases := []struct {
		name               string
		body               string
		expectedStatusCode int
	}{
		{name: "env が空のオブジェクトなら成功する", body: `{"env":{}}`, expectedStatusCode: 200},
		{name: "command と args が null なら成功する", body: `{"env":{"A":"1"},"command":null,"args":null}`, expectedStatusCode: 200},
		{name: "args が空の配列でも成功する", body: `{"env":{},"args":[]}`, expectedStatusCode: 200},
		{name: "env が未指定なら 400", body: `{"command":"/app"}`, expectedStatusCode: 400},
		{name: "env が null なら 400", body: `{"env":null}`, expectedStatusCode: 400},
		{name: "env の値が文字列でないと 400", body: `{"env":{"PORT":8080}}`, expectedStatusCode: 400},
		{name: "env の名前が数字で始まると 400", body: `{"env":{"1A":"x"}}`, expectedStatusCode: 400},
		{name: "env の名前に = を含むと 400", body: `{"env":{"A=B":"x"}}`, expectedStatusCode: 400},
		{name: "env の名前が空だと 400", body: `{"env":{"":"x"}}`, expectedStatusCode: 400},
		{name: "env の値が4097文字だと 400", body: `{"env":{"A":"` + strings.Repeat("a", 4097) + `"}}`, expectedStatusCode: 400},
		{name: "env の値が4096文字なら成功する", body: `{"env":{"A":"` + strings.Repeat("a", 4096) + `"}}`, expectedStatusCode: 200},
		{name: "env が101個だと 400", body: `{"env":{` + strings.Join(manyEnv, ",") + `}}`, expectedStatusCode: 400},
		{name: "command が空文字だと 400", body: `{"env":{},"command":""}`, expectedStatusCode: 400},
		{name: "args が文字列の配列でないと 400", body: `{"env":{},"args":[1]}`, expectedStatusCode: 400},
		{name: "JSON でないボディは 400", body: `not json`, expectedStatusCode: 400},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			statusCode, responseBody := apiClient.call(http.MethodPut, "/deploy/"+deployID+"/config", testCase.body)
			if statusCode != testCase.expectedStatusCode {
				subTest.Errorf("ステータスコードが違う: 期待 %d、実際 %d、ボディ %v", testCase.expectedStatusCode, statusCode, responseBody)
			}
		})
	}
}

// TestDeployAPIConfigRejectsByStatusAndOwner は、起動設定の操作が、状態と持ち主によって拒否されることを確認する。
func TestDeployAPIConfigRejectsByStatusAndOwner(testingT *testing.T) {
	// 持ち主が deploy を作る。
	database := testdatabase.New(testingT)
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database})
	deployID := createDeployFor(ownerClient, createProjectFor(ownerClient, "my-app"))
	configPath := "/deploy/" + deployID + "/config"
	// discard が 409 になる理由が「pending が無い」ではなく状態になるよう、先に pending を作っておく。
	ownerClient.call(http.MethodPut, configPath, `{"env":{}}`)

	// 状態ごとに、PUT と discard が 409 になる。
	for _, blockedStatus := range []string{"applying", "deleting", "delete_failed"} {
		testingT.Run(blockedStatus+" の間は PUT も discard も 409", func(subTest *testing.T) {
			database.Exec("UPDATE deploys SET status = ? WHERE id = ?", blockedStatus, deployID)
			putStatus, _ := ownerClient.call(http.MethodPut, configPath, `{"env":{}}`)
			discardStatus, _ := ownerClient.call(http.MethodPost, configPath+"/discard", "")
			if putStatus != 409 || discardStatus != 409 {
				subTest.Errorf("どちらも 409 のはず: PUT %d、discard %d", putStatus, discardStatus)
			}
		})
	}
	// 取得は、状態に関係なく成功する。
	testingT.Run("状態に関係なく GET はできる", func(subTest *testing.T) {
		if statusCode, _ := ownerClient.call(http.MethodGet, configPath, ""); statusCode != 200 {
			subTest.Errorf("200 のはず: %d", statusCode)
		}
	})
	// 他のユーザーと存在しない ID は、どの操作も 404 になる。
	for _, target := range []string{"/deploy/" + deployID + "/config", "/deploy/deploy-nothing/config"} {
		testingT.Run("他人・存在しない deploy は 404: "+target, func(subTest *testing.T) {
			client := otherClient
			if strings.Contains(target, "nothing") {
				client = ownerClient
			}
			getStatus, _ := client.call(http.MethodGet, target, "")
			putStatus, _ := client.call(http.MethodPut, target, `{"env":{}}`)
			discardStatus, _ := client.call(http.MethodPost, target+"/discard", "")
			if getStatus != 404 || putStatus != 404 || discardStatus != 404 {
				subTest.Errorf("全て 404 のはず: GET %d、PUT %d、discard %d", getStatus, putStatus, discardStatus)
			}
		})
	}
}

// TestDeployAPIConfigApply は、起動設定の適用(POST .../config/apply)の、受け付けと拒否を確認する。
func TestDeployAPIConfigApply(testingT *testing.T) {
	// 持ち主が deploy を作る。作成直後(pending)は、起動中なので、active にしてから使う。
	database := testdatabase.New(testingT)
	fakeStarter := &fakeDeployWorkflowStarter{}
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	deployID := createDeployFor(ownerClient, createProjectFor(ownerClient, "my-app"))
	configPath := "/deploy/" + deployID + "/config"
	applyPath := configPath + "/apply"
	statusOf := func() string {
		_, body := ownerClient.call(http.MethodGet, "/deploy/"+deployID, "")
		return body["status"].(string)
	}

	// 起動中(pending)は、反映を始められない。
	testingT.Run("起動中(pending)の deploy は 409", func(subTest *testing.T) {
		ownerClient.call(http.MethodPut, configPath, `{"env":{"A":"1"}}`)
		statusCode, body := ownerClient.call(http.MethodPost, applyPath, "")
		if statusCode != 409 || errorCodeOf(body) != "conflict" {
			subTest.Errorf("409 のはず: %d %v", statusCode, body)
		}
	})
	database.Exec("UPDATE deploys SET status = 'active' WHERE id = ?", deployID)

	// 他人と存在しない ID は 404。
	testingT.Run("他人・存在しない deploy は 404", func(subTest *testing.T) {
		otherStatus, _ := otherClient.call(http.MethodPost, applyPath, "")
		missingStatus, _ := ownerClient.call(http.MethodPost, "/deploy/deploy-nothing/config/apply", "")
		if otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("どちらも 404 のはず: %d %d", otherStatus, missingStatus)
		}
	})
	// pending を破棄して、適用する値が無い状態では 409。
	testingT.Run("適用する値が無いと 409 で、Workflow は開始されない", func(subTest *testing.T) {
		ownerClient.call(http.MethodPost, configPath+"/discard", "")
		statusCode, _ := ownerClient.call(http.MethodPost, applyPath, "")
		if statusCode != 409 || len(fakeStarter.appliedDeployIDs) != 0 {
			subTest.Errorf("409 で、開始されないはず: %d %v", statusCode, fakeStarter.appliedDeployIDs)
		}
	})
	// 正常。applying になり、Workflow が開始され、pending は保持される。
	testingT.Run("適用できると、applying になり、Workflow が開始され、pending は保持される", func(subTest *testing.T) {
		ownerClient.call(http.MethodPut, configPath, `{"env":{"PORT":"8080"}}`)
		statusCode, body := ownerClient.call(http.MethodPost, applyPath, "")
		if statusCode != 200 || len(body) != 0 {
			subTest.Fatalf("200 と空のオブジェクトのはず: %d %v", statusCode, body)
		}
		if statusOf() != "applying" || len(fakeStarter.appliedDeployIDs) != 1 || fakeStarter.appliedDeployIDs[0] != deployID {
			subTest.Errorf("applying で、1回開始されるはず: %s %v", statusOf(), fakeStarter.appliedDeployIDs)
		}
		_, configBody := ownerClient.call(http.MethodGet, configPath, "")
		if pendingEnv, _ := configBody["pending_env"].(map[string]any); pendingEnv["PORT"] != "8080" {
			subTest.Errorf("pending が保持されていない: %v", configBody)
		}
	})
	// 反映中の再度の apply は 409(多重 apply 不可)で、Workflow は増えない。
	testingT.Run("applying 中の apply は 409", func(subTest *testing.T) {
		statusCode, _ := ownerClient.call(http.MethodPost, applyPath, "")
		if statusCode != 409 || len(fakeStarter.appliedDeployIDs) != 1 {
			subTest.Errorf("409 で、増えないはず: %d %v", statusCode, fakeStarter.appliedDeployIDs)
		}
	})
	// 削除の途中は 409。
	for _, blockedStatus := range []string{"deleting", "delete_failed"} {
		testingT.Run(blockedStatus+" の apply は 409", func(subTest *testing.T) {
			database.Exec("UPDATE deploys SET status = ? WHERE id = ?", blockedStatus, deployID)
			if statusCode, _ := ownerClient.call(http.MethodPost, applyPath, ""); statusCode != 409 {
				subTest.Errorf("409 のはず: %d", statusCode)
			}
		})
	}
	// failed と scaled_zero の deploy は、適用できる。
	for _, allowedStatus := range []string{"failed", "scaled_zero"} {
		testingT.Run(allowedStatus+" の deploy は適用できる", func(subTest *testing.T) {
			database.Exec("UPDATE deploys SET status = ? WHERE id = ?", allowedStatus, deployID)
			if statusCode, _ := ownerClient.call(http.MethodPost, applyPath, ""); statusCode != 200 {
				subTest.Errorf("200 のはず: %d", statusCode)
			}
		})
	}
}

// TestDeployAPIConfigApplyRevertsWhenWorkflowCannotStart は、Workflow を開始できなかったとき、
// 500 を返し、状態が元に戻る(applying のまま残らない)ことを確認する。
func TestDeployAPIConfigApplyRevertsWhenWorkflowCannotStart(testingT *testing.T) {
	// deploy の作成は成功させ、そのあとから、開始に失敗する状態にする。
	database := testdatabase.New(testingT)
	fakeStarter := &fakeDeployWorkflowStarter{}
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	deployID := createDeployFor(apiClient, createProjectFor(apiClient, "my-app"))
	// deploy を failed にして、pending を入れる。
	database.Exec("UPDATE deploys SET status = 'failed' WHERE id = ?", deployID)
	apiClient.call(http.MethodPut, "/deploy/"+deployID+"/config", `{"env":{"A":"1"}}`)
	fakeStarter.startError = errors.New("temporal down")

	// 適用は 500 になり、状態は failed に戻り、pending は残る。
	statusCode, body := apiClient.call(http.MethodPost, "/deploy/"+deployID+"/config/apply", "")
	if statusCode != 500 || errorCodeOf(body) != "internal_error" {
		testingT.Fatalf("500 internal_error のはず: %d %v", statusCode, body)
	}
	_, deployBody := apiClient.call(http.MethodGet, "/deploy/"+deployID, "")
	config, _ := deployBody["config"].(map[string]any)
	if deployBody["status"] != "failed" || config["pending_env"] == nil {
		testingT.Errorf("failed に戻り、pending が残るはず: %v", deployBody)
	}
}

// TestDeployAPIConfigSameAsCurrentIsNotPending は、現在の値と同じ設定を PUT しても、pending にならず、
// apply も 409 になる(同じ設定の再適用はできない)ことを確認する。
func TestDeployAPIConfigSameAsCurrentIsNotPending(testingT *testing.T) {
	// active の deploy を用意する(現在の値は、env が空、command と args が null)。
	database := testdatabase.New(testingT)
	fakeStarter := &fakeDeployWorkflowStarter{}
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	deployID := createDeployFor(apiClient, createProjectFor(apiClient, "my-app"))
	database.Exec("UPDATE deploys SET status = 'active' WHERE id = ?", deployID)
	configPath := "/deploy/" + deployID + "/config"

	// 現在と同じ内容を PUT すると、200 だが pending は null のまま。
	statusCode, _ := apiClient.call(http.MethodPut, configPath, `{"env":{}}`)
	_, configBody := apiClient.call(http.MethodGet, configPath, "")
	if statusCode != 200 || configBody["pending_env"] != nil {
		testingT.Fatalf("200 で、pending は null のはず: %d %v", statusCode, configBody)
	}
	// apply は 409 で、Workflow は開始されない。
	applyStatus, _ := apiClient.call(http.MethodPost, configPath+"/apply", "")
	if applyStatus != 409 || len(fakeStarter.appliedDeployIDs) != 0 {
		testingT.Errorf("409 で、開始されないはず: %d %v", applyStatus, fakeStarter.appliedDeployIDs)
	}
}
