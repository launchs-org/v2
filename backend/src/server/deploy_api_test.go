package server

import (
	"context"
	"errors"
	"fmt"
	"launchs/shared/database/testdatabase"
	"net/http"
	"strings"
	"testing"
)

// fakeDeployWorkflowStarter は、テスト用の DeployWorkflowStarter。開始を頼まれた ID を覚え、決めたエラーを返す。
type fakeDeployWorkflowStarter struct {
	startedDeployIDs []string
	appliedDeployIDs []string
	mountedDeployIDs []string
	filesDeployIDs   []string
	scaledDeployIDs  []string
	deletedDeployIDs []string
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

// StartMountsApply は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeDeployWorkflowStarter) StartMountsApply(_ context.Context, deployID string) error {
	fakeStarter.mountedDeployIDs = append(fakeStarter.mountedDeployIDs, deployID)
	return fakeStarter.startError
}

// StartFilesApply は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeDeployWorkflowStarter) StartFilesApply(_ context.Context, deployID string) error {
	fakeStarter.filesDeployIDs = append(fakeStarter.filesDeployIDs, deployID)
	return fakeStarter.startError
}

// StartScale は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeDeployWorkflowStarter) StartScale(_ context.Context, deployID string) error {
	fakeStarter.scaledDeployIDs = append(fakeStarter.scaledDeployIDs, deployID)
	return fakeStarter.startError
}

// StartDeletion は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeDeployWorkflowStarter) StartDeletion(_ context.Context, deployID string) error {
	fakeStarter.deletedDeployIDs = append(fakeStarter.deletedDeployIDs, deployID)
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
		{http.MethodPost, "/deploy/deploy-x/config/apply", ""},
		{http.MethodPost, "/deploy/deploy-x/scale", `{"replicas":1}`},
		{http.MethodDelete, "/deploy/deploy-x", ""},
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
