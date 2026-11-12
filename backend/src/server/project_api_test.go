package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"launchs/shared/database/testdatabase"
	"launchs/shared/logger"
)

// projectAPIClient は、テスト用に、あるユーザーとして /api/v1/projects を呼ぶ道具。
type projectAPIClient struct {
	testingT   *testing.T
	httpServer http.Handler
}

// newProjectAPIClient は、使い捨てのデータベースにつながるサーバーを作り、そのユーザーとして呼ぶ道具を返す。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - devUserID: リクエストのユーザーとして扱う ID。空なら未認証になる。
//   - dependencies: すでにあるデータベースを使い回したいときに渡す。Database が nil なら新しく作る。
//
// 戻り値:
//   - 呼び出す道具。
func newProjectAPIClient(testingT *testing.T, devUserID string, dependencies Dependencies) *projectAPIClient {
	testingT.Helper()
	if dependencies.Database == nil {
		dependencies.Database = testdatabase.New(testingT)
	}
	dependencies.DevUserID = devUserID
	var logOutput bytes.Buffer
	return &projectAPIClient{testingT: testingT, httpServer: New(logger.New(&logOutput, "backend", slog.LevelInfo), dependencies)}
}

// call は、API を呼び、ステータスコードと JSON のボディを返す。
//
// 引数:
//   - method: HTTP のメソッド。
//   - path: /api/v1 を除いたパス(例: /projects)。
//   - body: リクエストのボディ。空なら付けない。
//
// 戻り値:
//   - ステータスコード。
//   - JSON のボディ(map)。
func (client *projectAPIClient) call(method string, path string, body string) (int, map[string]any) {
	client.testingT.Helper()
	request := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	client.httpServer.ServeHTTP(recorder, request)

	decoded := map[string]any{}
	if recorder.Body.Len() > 0 {
		if unmarshalError := json.Unmarshal(recorder.Body.Bytes(), &decoded); unmarshalError != nil {
			client.testingT.Fatalf("ボディが JSON ではない: %v\nボディ: %s", unmarshalError, recorder.Body.String())
		}
	}
	return recorder.Code, decoded
}

// errorCodeOf は、エラーのボディから code を取り出す。
//
// 引数:
//   - responseBody: エラーのボディ。
//
// 戻り値:
//   - code の文字列。取り出せなければ空文字。
func errorCodeOf(responseBody map[string]any) string {
	errorObject, _ := responseBody["error"].(map[string]any)
	code, _ := errorObject["code"].(string)
	return code
}

// TestProjectAPICreate は、プロジェクト作成(POST /projects)の、正常・異常・境界値を確認する。
func TestProjectAPICreate(testingT *testing.T) {
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{})

	testCases := []struct {
		name               string
		body               string
		expectedStatusCode int
		expectedErrorCode  string
	}{
		{name: "有効な name で作成できる", body: `{"name":"my-app"}`, expectedStatusCode: 200},
		{name: "1文字の name は作成できる", body: `{"name":"a"}`, expectedStatusCode: 200},
		{name: "64文字の name は作成できる", body: fmt.Sprintf(`{"name":"%s"}`, strings.Repeat("b", 64)), expectedStatusCode: 200},
		{name: "name が未指定なら 400", body: `{}`, expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
		{name: "name が空文字なら 400", body: `{"name":""}`, expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
		{name: "英数字とハイフン以外を含むと 400", body: `{"name":"my_app"}`, expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
		{name: "65文字の name は 400", body: fmt.Sprintf(`{"name":"%s"}`, strings.Repeat("c", 65)), expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
		{name: "JSON でないボディは 400", body: `not json`, expectedStatusCode: 400, expectedErrorCode: "invalid_request"},
		{name: "同じユーザーで name が重複すると 409", body: `{"name":"my-app"}`, expectedStatusCode: 409, expectedErrorCode: "conflict"},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			statusCode, responseBody := apiClient.call(http.MethodPost, "/projects", testCase.body)

			if statusCode != testCase.expectedStatusCode {
				subTest.Fatalf("ステータスコードが違う: 期待 %d、実際 %d、ボディ %v", testCase.expectedStatusCode, statusCode, responseBody)
			}
			if testCase.expectedErrorCode != "" && errorCodeOf(responseBody) != testCase.expectedErrorCode {
				subTest.Errorf("code が違う: 期待 %q、実際 %v", testCase.expectedErrorCode, responseBody)
			}
			if statusCode == 200 {
				projectID, _ := responseBody["projectId"].(string)
				if !strings.HasPrefix(projectID, "project-") {
					subTest.Errorf("projectId の形が違う: %v", responseBody)
				}
			}
		})
	}
}

// TestProjectAPIRequiresAuthentication は、認証できないとき(DEV_USER_ID が空)、全ての
// プロジェクトの API が 401 を返すことを確認する。
func TestProjectAPIRequiresAuthentication(testingT *testing.T) {
	apiClient := newProjectAPIClient(testingT, "", Dependencies{})

	requests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/projects", `{"name":"my-app"}`},
		{http.MethodGet, "/projects", ""},
		{http.MethodGet, "/projects/project-x", ""},
		{http.MethodPatch, "/projects/project-x", `{"name":"renamed"}`},
	}
	for _, request := range requests {
		testingT.Run(request.method+" "+request.path, func(subTest *testing.T) {
			statusCode, responseBody := apiClient.call(request.method, request.path, request.body)
			if statusCode != http.StatusUnauthorized || errorCodeOf(responseBody) != "unauthorized" {
				subTest.Errorf("401 unauthorized になるはずが、違う: %d %v", statusCode, responseBody)
			}
		})
	}
}

// TestProjectAPIGetListAndRename は、作成したプロジェクトが、取得・一覧に出て、名前を変えられ、
// 他のユーザーからは見えず(404)、変更もできない(404)ことを確認する。
func TestProjectAPIGetListAndRename(testingT *testing.T) {
	database := testdatabase.New(testingT)
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database})

	_, createdBody := ownerClient.call(http.MethodPost, "/projects", `{"name":"first"}`)
	projectID := createdBody["projectId"].(string)

	testingT.Run("作成した name と active で取得できる", func(subTest *testing.T) {
		statusCode, responseBody := ownerClient.call(http.MethodGet, "/projects/"+projectID, "")
		if statusCode != 200 || responseBody["name"] != "first" || responseBody["status"] != "active" || responseBody["projectId"] != projectID {
			subTest.Errorf("取得の結果が違う: %d %v", statusCode, responseBody)
		}
	})
	testingT.Run("存在しない projectId は 404", func(subTest *testing.T) {
		statusCode, responseBody := ownerClient.call(http.MethodGet, "/projects/project-nothing", "")
		if statusCode != 404 || errorCodeOf(responseBody) != "not_found" {
			subTest.Errorf("404 のはずが、違う: %d %v", statusCode, responseBody)
		}
	})
	testingT.Run("他のユーザーのプロジェクトは、取得も変更も 404", func(subTest *testing.T) {
		getStatus, _ := otherClient.call(http.MethodGet, "/projects/"+projectID, "")
		patchStatus, _ := otherClient.call(http.MethodPatch, "/projects/"+projectID, `{"name":"stolen"}`)
		if getStatus != 404 || patchStatus != 404 {
			subTest.Errorf("どちらも 404 のはず: GET %d、PATCH %d", getStatus, patchStatus)
		}
	})
	testingT.Run("名前を変えると、取得に反映される", func(subTest *testing.T) {
		patchStatus, patchBody := ownerClient.call(http.MethodPatch, "/projects/"+projectID, `{"name":"renamed"}`)
		_, getBody := ownerClient.call(http.MethodGet, "/projects/"+projectID, "")
		if patchStatus != 200 || len(patchBody) != 0 || getBody["name"] != "renamed" {
			subTest.Errorf("名前の変更が反映されていない: PATCH %d %v、GET %v", patchStatus, patchBody, getBody)
		}
	})
	testingT.Run("変更後の name が不正なら 400", func(subTest *testing.T) {
		patchStatus, patchBody := ownerClient.call(http.MethodPatch, "/projects/"+projectID, `{"name":"bad name"}`)
		if patchStatus != 400 || errorCodeOf(patchBody) != "invalid_request" {
			subTest.Errorf("400 のはずが、違う: %d %v", patchStatus, patchBody)
		}
	})
	testingT.Run("他のプロジェクトと重複する名前に変えると 409", func(subTest *testing.T) {
		ownerClient.call(http.MethodPost, "/projects", `{"name":"second"}`)
		patchStatus, patchBody := ownerClient.call(http.MethodPatch, "/projects/"+projectID, `{"name":"second"}`)
		if patchStatus != 409 || errorCodeOf(patchBody) != "conflict" {
			subTest.Errorf("409 のはずが、違う: %d %v", patchStatus, patchBody)
		}
	})
	testingT.Run("一覧には自分のものだけが、新しい順に出る", func(subTest *testing.T) {
		statusCode, responseBody := ownerClient.call(http.MethodGet, "/projects", "")
		projects, _ := responseBody["projects"].([]any)
		if statusCode != 200 || len(projects) != 2 {
			subTest.Fatalf("2件のはず: %d %v", statusCode, responseBody)
		}
		firstProject := projects[0].(map[string]any)
		if firstProject["name"] != "second" || responseBody["nextCursor"] != nil {
			subTest.Errorf("新しい順で、nextCursor は null のはず: %v", responseBody)
		}
		_, otherBody := otherClient.call(http.MethodGet, "/projects", "")
		if otherProjects, _ := otherBody["projects"].([]any); len(otherProjects) != 0 {
			subTest.Errorf("他のユーザーの一覧は空のはず: %v", otherBody)
		}
	})
}

// TestProjectAPIListPaging は、limit とカーソルで全件を重複なくたどれることと、
// 不正な limit・cursor が 400 になることを確認する。
func TestProjectAPIListPaging(testingT *testing.T) {
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{})
	for index := 1; index <= 5; index++ {
		apiClient.call(http.MethodPost, "/projects", fmt.Sprintf(`{"name":"app-%d"}`, index))
	}

	testingT.Run("limit=2 で、3ページに分かれ、全件を重複なくたどれる", func(subTest *testing.T) {
		var collectedNames []string
		path := "/projects?limit=2"
		pageCount := 0
		for path != "" {
			statusCode, responseBody := apiClient.call(http.MethodGet, path, "")
			if statusCode != 200 {
				subTest.Fatalf("200 のはず: %d %v", statusCode, responseBody)
			}
			pageCount++
			for _, rawProject := range responseBody["projects"].([]any) {
				collectedNames = append(collectedNames, rawProject.(map[string]any)["name"].(string))
			}
			path = ""
			if nextCursor, hasNext := responseBody["nextCursor"].(string); hasNext {
				path = "/projects?limit=2&cursor=" + nextCursor
			}
		}
		if pageCount != 3 || fmt.Sprint(collectedNames) != "[app-5 app-4 app-3 app-2 app-1]" {
			subTest.Errorf("ページ数 %d、名前 %v", pageCount, collectedNames)
		}
	})
	for _, invalidPath := range []string{"/projects?limit=0", "/projects?limit=101", "/projects?limit=abc", "/projects?cursor=!!!"} {
		testingT.Run("不正な指定は 400: "+invalidPath, func(subTest *testing.T) {
			statusCode, responseBody := apiClient.call(http.MethodGet, invalidPath, "")
			if statusCode != 400 || errorCodeOf(responseBody) != "invalid_request" {
				subTest.Errorf("400 のはずが、違う: %d %v", statusCode, responseBody)
			}
		})
	}
}
