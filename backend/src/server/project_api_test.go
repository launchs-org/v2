package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"launchs/shared/database/testdatabase"
	"launchs/shared/logger"
)

// fakeDeletionStarter は、テスト用の ProjectDeletionStarter。開始を頼まれた ID を覚え、決めたエラーを返す。
type fakeDeletionStarter struct {
	startedProjectIDs []string
	startError        error
}

// StartProjectDeletion は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeDeletionStarter) StartProjectDeletion(_ context.Context, projectID string) error {
	fakeStarter.startedProjectIDs = append(fakeStarter.startedProjectIDs, projectID)
	return fakeStarter.startError
}

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
	// 失敗の行を、このヘルパーではなく呼び出し元として表示させる。
	testingT.Helper()
	// データベースが渡されていなければ、使い捨てのものを新しく作る。
	if dependencies.Database == nil {
		// テスト終了時に片付く、使い捨てのデータベースを用意する。
		dependencies.Database = testdatabase.New(testingT)
	}
	// このクライアントのユーザーとして扱う ID を設定する。
	dependencies.DevUserID = devUserID
	// 削除の開始が指定されていなければ、何もしない fake を使う。
	if dependencies.ProjectDeletionStarter == nil {
		dependencies.ProjectDeletionStarter = &fakeDeletionStarter{}
	}
	// ログの出力先を、メモリ上に用意する。
	var logOutput bytes.Buffer
	// サーバーを組み立て、呼び出す道具として返す。
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
	// 失敗の行を、呼び出し元として表示させる。
	client.testingT.Helper()
	// /api/v1 を付けたパスで、リクエストを作る。
	request := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	// ボディがあるときだけ、JSON であることをヘッダで伝える。
	if body != "" {
		// Content-Type を JSON にする。
		request.Header.Set("Content-Type", "application/json")
	}
	// レスポンスの記録用の入れ物を作る。
	recorder := httptest.NewRecorder()
	// サーバーにリクエストを渡して処理させる。
	client.httpServer.ServeHTTP(recorder, request)

	// ボディが無いときは、空の map を返せるようにしておく。
	decoded := map[string]any{}
	// ボディがあるときだけ、JSON として読む。
	if recorder.Body.Len() > 0 {
		// ボディを map に読み込む。JSON でなければ、確認を続けられないので止める。
		if unmarshalError := json.Unmarshal(recorder.Body.Bytes(), &decoded); unmarshalError != nil {
			// JSON でなかった内容を表示して、テストを止める。
			client.testingT.Fatalf("ボディが JSON ではない: %v\nボディ: %s", unmarshalError, recorder.Body.String())
		}
	}
	// ステータスコードと、読み込んだボディを返す。
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
	// error の中身を取り出す。無ければ nil の map になる。
	errorObject, _ := responseBody["error"].(map[string]any)
	// code を文字列として取り出す。取れなければ空文字になる。
	code, _ := errorObject["code"].(string)
	// 取り出した code を返す。
	return code
}

// TestProjectAPICreate は、プロジェクト作成(POST /projects)の、正常・異常・境界値を確認する。
func TestProjectAPICreate(testingT *testing.T) {
	// ユーザー a として呼ぶ道具を作る。ケースは順に実行し、最後のケースの重複は、最初のケースで作った my-app で起きる。
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{})

	// ケースの一覧(正常、異常、境界値)。名前と、リクエストのボディ、期待するステータスと code を持つ。
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
	// ケースを1つずつ、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// 作成を呼ぶ。
			statusCode, responseBody := apiClient.call(http.MethodPost, "/projects", testCase.body)

			// ステータスコードが期待と同じであることを確認する。以降の確認ができないので止める。
			if statusCode != testCase.expectedStatusCode {
				subTest.Fatalf("ステータスコードが違う: 期待 %d、実際 %d、ボディ %v", testCase.expectedStatusCode, statusCode, responseBody)
			}
			// 異常系では、エラーの code が期待どおりであることを確認する。
			if testCase.expectedErrorCode != "" && errorCodeOf(responseBody) != testCase.expectedErrorCode {
				subTest.Errorf("code が違う: 期待 %q、実際 %v", testCase.expectedErrorCode, responseBody)
			}
			// 成功したときは、projectId が project- で始まる形であることを確認する。
			if statusCode == 200 {
				// レスポンスから projectId を取り出す。
				projectID, _ := responseBody["projectId"].(string)
				// projectId の形が違えば失敗にする。
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
	// ユーザーが空(未認証)のクライアントを作る。
	apiClient := newProjectAPIClient(testingT, "", Dependencies{})

	// 全てのプロジェクトの API の、メソッド・パス・ボディの一覧。
	requests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/projects", `{"name":"my-app"}`},
		{http.MethodGet, "/projects", ""},
		{http.MethodGet, "/projects/project-x", ""},
		{http.MethodPatch, "/projects/project-x", `{"name":"renamed"}`},
		{http.MethodDelete, "/projects/project-x", ""},
	}
	// API を1つずつ、サブテストとして呼ぶ。
	for _, request := range requests {
		testingT.Run(request.method+" "+request.path, func(subTest *testing.T) {
			// 認証無しで呼ぶ。
			statusCode, responseBody := apiClient.call(request.method, request.path, request.body)
			// 401 と unauthorized の code であることを確認する。
			if statusCode != http.StatusUnauthorized || errorCodeOf(responseBody) != "unauthorized" {
				subTest.Errorf("401 unauthorized になるはずが、違う: %d %v", statusCode, responseBody)
			}
		})
	}
}

// TestProjectAPIGetListAndRename は、作成したプロジェクトが、取得・一覧に出て、名前を変えられ、
// 他のユーザーからは見えず(404)、変更もできない(404)ことを確認する。
func TestProjectAPIGetListAndRename(testingT *testing.T) {
	// 2人のユーザーが同じデータベースを使うよう、1つ作る。
	database := testdatabase.New(testingT)
	// 持ち主(ユーザー a)として呼ぶ道具を作る。
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database})
	// 他のユーザー(ユーザー b)として呼ぶ道具を作る。
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database})

	// 持ち主が、プロジェクトを1つ作る。
	_, createdBody := ownerClient.call(http.MethodPost, "/projects", `{"name":"first"}`)
	// 以降のケースで使う、作成したプロジェクトの ID を取り出す。
	projectID := createdBody["projectId"].(string)

	// 作成直後に、取得できることを確認する。
	testingT.Run("作成した name と active で取得できる", func(subTest *testing.T) {
		// 取得を呼ぶ。
		statusCode, responseBody := ownerClient.call(http.MethodGet, "/projects/"+projectID, "")
		// 名前、状態、ID が期待どおりであることを確認する。
		if statusCode != 200 || responseBody["name"] != "first" || responseBody["status"] != "active" || responseBody["projectId"] != projectID {
			subTest.Errorf("取得の結果が違う: %d %v", statusCode, responseBody)
		}
	})
	// 存在しない ID が 404 になることを確認する。
	testingT.Run("存在しない projectId は 404", func(subTest *testing.T) {
		// 存在しない ID で取得を呼ぶ。
		statusCode, responseBody := ownerClient.call(http.MethodGet, "/projects/project-nothing", "")
		// 404 と not_found であることを確認する。
		if statusCode != 404 || errorCodeOf(responseBody) != "not_found" {
			subTest.Errorf("404 のはずが、違う: %d %v", statusCode, responseBody)
		}
	})
	// 他のユーザーからは、存在しないのと同じに見えることを確認する。
	testingT.Run("他のユーザーのプロジェクトは、取得も変更も 404", func(subTest *testing.T) {
		// 他のユーザーが、取得を呼ぶ。
		getStatus, _ := otherClient.call(http.MethodGet, "/projects/"+projectID, "")
		// 他のユーザーが、名前の変更を呼ぶ。
		patchStatus, _ := otherClient.call(http.MethodPatch, "/projects/"+projectID, `{"name":"stolen"}`)
		// どちらも 404 であることを確認する。
		if getStatus != 404 || patchStatus != 404 {
			subTest.Errorf("どちらも 404 のはず: GET %d、PATCH %d", getStatus, patchStatus)
		}
	})
	// 名前の変更が、取得に反映されることを確認する。
	testingT.Run("名前を変えると、取得に反映される", func(subTest *testing.T) {
		// 名前を変更する。
		patchStatus, patchBody := ownerClient.call(http.MethodPatch, "/projects/"+projectID, `{"name":"renamed"}`)
		// 変更後の内容を取得する。
		_, getBody := ownerClient.call(http.MethodGet, "/projects/"+projectID, "")
		// 変更が 200 で空のボディを返し、取得の名前が変わっていることを確認する。
		if patchStatus != 200 || len(patchBody) != 0 || getBody["name"] != "renamed" {
			subTest.Errorf("名前の変更が反映されていない: PATCH %d %v、GET %v", patchStatus, patchBody, getBody)
		}
	})
	// 不正な名前への変更が 400 になることを確認する。
	testingT.Run("変更後の name が不正なら 400", func(subTest *testing.T) {
		// 空白を含む不正な名前に変更を試みる。
		patchStatus, patchBody := ownerClient.call(http.MethodPatch, "/projects/"+projectID, `{"name":"bad name"}`)
		// 400 と invalid_request であることを確認する。
		if patchStatus != 400 || errorCodeOf(patchBody) != "invalid_request" {
			subTest.Errorf("400 のはずが、違う: %d %v", patchStatus, patchBody)
		}
	})
	// 他のプロジェクトと同じ名前への変更が 409 になることを確認する。
	testingT.Run("他のプロジェクトと重複する名前に変えると 409", func(subTest *testing.T) {
		// 重複させる相手として、2つ目のプロジェクトを作る。
		ownerClient.call(http.MethodPost, "/projects", `{"name":"second"}`)
		// 1つ目の名前を、2つ目と同じにしようとする。
		patchStatus, patchBody := ownerClient.call(http.MethodPatch, "/projects/"+projectID, `{"name":"second"}`)
		// 409 と conflict であることを確認する。
		if patchStatus != 409 || errorCodeOf(patchBody) != "conflict" {
			subTest.Errorf("409 のはずが、違う: %d %v", patchStatus, patchBody)
		}
	})
	// 一覧に、自分のものだけが新しい順で出ることを確認する。
	testingT.Run("一覧には自分のものだけが、新しい順に出る", func(subTest *testing.T) {
		// 持ち主の一覧を取得する。
		statusCode, responseBody := ownerClient.call(http.MethodGet, "/projects", "")
		// projects を配列として取り出す。
		projects, _ := responseBody["projects"].([]any)
		// 作成した2件が出ていることを確認する。違えば以降の確認ができないので止める。
		if statusCode != 200 || len(projects) != 2 {
			subTest.Fatalf("2件のはず: %d %v", statusCode, responseBody)
		}
		// 先頭の要素を取り出す。
		firstProject := projects[0].(map[string]any)
		// 新しい順(先頭が second)で、最後のページなので nextCursor が null であることを確認する。
		if firstProject["name"] != "second" || responseBody["nextCursor"] != nil {
			subTest.Errorf("新しい順で、nextCursor は null のはず: %v", responseBody)
		}
		// 他のユーザーの一覧を取得する。
		_, otherBody := otherClient.call(http.MethodGet, "/projects", "")
		// 他のユーザーの一覧が空であることを確認する。
		if otherProjects, _ := otherBody["projects"].([]any); len(otherProjects) != 0 {
			subTest.Errorf("他のユーザーの一覧は空のはず: %v", otherBody)
		}
	})
}

// TestProjectAPIListPaging は、limit とカーソルで全件を重複なくたどれることと、
// 不正な limit・cursor が 400 になることを確認する。
func TestProjectAPIListPaging(testingT *testing.T) {
	// ユーザー a として呼ぶ道具を作る。
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{})
	// ページングを確認するため、5件のプロジェクトを作る。
	for index := 1; index <= 5; index++ {
		// app-1 から app-5 まで、順に作る。
		apiClient.call(http.MethodPost, "/projects", fmt.Sprintf(`{"name":"app-%d"}`, index))
	}

	// limit=2 で、カーソルをたどって全件を集める。
	testingT.Run("limit=2 で、3ページに分かれ、全件を重複なくたどれる", func(subTest *testing.T) {
		// たどって集めた名前を入れる。
		var collectedNames []string
		// 最初のページのパスから始める。
		path := "/projects?limit=2"
		// ページ数を数える。
		pageCount := 0
		// 次のページが無くなる(パスが空になる)まで繰り返す。
		for path != "" {
			// 現在のページを取得する。
			statusCode, responseBody := apiClient.call(http.MethodGet, path, "")
			// 200 でなければ、たどれないので止める。
			if statusCode != 200 {
				subTest.Fatalf("200 のはず: %d %v", statusCode, responseBody)
			}
			// 取得できたページを数える。
			pageCount++
			// このページの名前を、集めた一覧に足す。
			for _, rawProject := range responseBody["projects"].([]any) {
				// 1件の名前を追加する。
				collectedNames = append(collectedNames, rawProject.(map[string]any)["name"].(string))
			}
			// 次のカーソルが無ければ、ここで終わるよう、先にパスを空にする。
			path = ""
			// 次のページのカーソルがあるときは、それを使って次のパスを作る。
			if nextCursor, hasNext := responseBody["nextCursor"].(string); hasNext {
				// カーソルを付けた、次のページのパスにする。
				path = "/projects?limit=2&cursor=" + nextCursor
			}
		}
		// 3ページで、新しい順に全件が重複なく集まったことを確認する。
		if pageCount != 3 || fmt.Sprint(collectedNames) != "[app-5 app-4 app-3 app-2 app-1]" {
			subTest.Errorf("ページ数 %d、名前 %v", pageCount, collectedNames)
		}
	})
	// 不正な limit と cursor の指定を、1つずつサブテストにする。
	for _, invalidPath := range []string{"/projects?limit=0", "/projects?limit=101", "/projects?limit=abc", "/projects?cursor=!!!"} {
		// 不正な指定が 400 になることを確認する。
		testingT.Run("不正な指定は 400: "+invalidPath, func(subTest *testing.T) {
			// 不正な指定で一覧を呼ぶ。
			statusCode, responseBody := apiClient.call(http.MethodGet, invalidPath, "")
			// 400 と invalid_request であることを確認する。
			if statusCode != 400 || errorCodeOf(responseBody) != "invalid_request" {
				subTest.Errorf("400 のはずが、違う: %d %v", statusCode, responseBody)
			}
		})
	}
}

// TestProjectAPIDelete は、削除の受け付け(DELETE /projects/:projectId)で、すぐに 200 を返し、
// status が deleting になり、Workflow が開始され、以後の変更や再度の削除が 409 になることを確認する。
func TestProjectAPIDelete(testingT *testing.T) {
	// 持ち主と他のユーザーが、同じデータベースを使う。削除の開始は、記録する fake にする。
	database := testdatabase.New(testingT)
	fakeStarter := &fakeDeletionStarter{}
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database, ProjectDeletionStarter: fakeStarter})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database, ProjectDeletionStarter: fakeStarter})
	_, createdBody := ownerClient.call(http.MethodPost, "/projects", `{"name":"to-delete"}`)
	projectID := createdBody["projectId"].(string)

	// 他のユーザーは、存在しないものとして 404 になり、Workflow も開始されない。
	testingT.Run("他のユーザーのプロジェクトは 404", func(subTest *testing.T) {
		statusCode, responseBody := otherClient.call(http.MethodDelete, "/projects/"+projectID, "")
		if statusCode != 404 || errorCodeOf(responseBody) != "not_found" || len(fakeStarter.startedProjectIDs) != 0 {
			subTest.Errorf("404 で、開始しないはず: %d %v、開始 %v", statusCode, responseBody, fakeStarter.startedProjectIDs)
		}
	})
	// 存在しない ID は 404。
	testingT.Run("存在しない projectId は 404", func(subTest *testing.T) {
		statusCode, _ := ownerClient.call(http.MethodDelete, "/projects/project-nothing", "")
		if statusCode != 404 {
			subTest.Errorf("404 のはずが %d", statusCode)
		}
	})
	// 持ち主の削除は、すぐ 200 で、Workflow が1回開始される。
	testingT.Run("削除を受け付けると 200 で、Workflow が開始される", func(subTest *testing.T) {
		statusCode, responseBody := ownerClient.call(http.MethodDelete, "/projects/"+projectID, "")
		if statusCode != 200 || len(responseBody) != 0 {
			subTest.Fatalf("200 と空のボディのはず: %d %v", statusCode, responseBody)
		}
		if len(fakeStarter.startedProjectIDs) != 1 || fakeStarter.startedProjectIDs[0] != projectID {
			subTest.Errorf("Workflow を1回開始するはず: %v", fakeStarter.startedProjectIDs)
		}
	})
	// 受け付けた直後の GET は、deleting を返す。
	testingT.Run("直後の取得は status が deleting", func(subTest *testing.T) {
		statusCode, responseBody := ownerClient.call(http.MethodGet, "/projects/"+projectID, "")
		if statusCode != 200 || responseBody["status"] != "deleting" {
			subTest.Errorf("deleting のはず: %d %v", statusCode, responseBody)
		}
	})
	// 削除中は、更新も再度の削除も 409 で、Workflow は増えない。
	testingT.Run("削除中の更新と再度の削除は 409", func(subTest *testing.T) {
		patchStatus, patchBody := ownerClient.call(http.MethodPatch, "/projects/"+projectID, `{"name":"renamed"}`)
		deleteStatus, deleteBody := ownerClient.call(http.MethodDelete, "/projects/"+projectID, "")
		if patchStatus != 409 || errorCodeOf(patchBody) != "conflict" || deleteStatus != 409 || errorCodeOf(deleteBody) != "conflict" {
			subTest.Errorf("どちらも 409 のはず: PATCH %d、DELETE %d", patchStatus, deleteStatus)
		}
		if len(fakeStarter.startedProjectIDs) != 1 {
			subTest.Errorf("Workflow は増えないはず: %v", fakeStarter.startedProjectIDs)
		}
	})
}

// TestProjectAPIDeleteWhenWorkflowCannotStart は、Workflow を開始できなかったとき、
// 500 を返し、プロジェクトが削除中のまま残らず active に戻ることを確認する。
func TestProjectAPIDeleteWhenWorkflowCannotStart(testingT *testing.T) {
	// 開始に必ず失敗する fake を使う。
	fakeStarter := &fakeDeletionStarter{startError: errors.New("temporal down")}
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{ProjectDeletionStarter: fakeStarter})
	_, createdBody := apiClient.call(http.MethodPost, "/projects", `{"name":"keep"}`)
	projectID := createdBody["projectId"].(string)

	// 削除は 500 になる。
	statusCode, responseBody := apiClient.call(http.MethodDelete, "/projects/"+projectID, "")
	if statusCode != 500 || errorCodeOf(responseBody) != "internal_error" {
		testingT.Fatalf("500 internal_error のはず: %d %v", statusCode, responseBody)
	}
	// プロジェクトは active のままで、もう一度削除を試せる(409 にならない)。
	_, getBody := apiClient.call(http.MethodGet, "/projects/"+projectID, "")
	if getBody["status"] != "active" {
		testingT.Errorf("active に戻っているはず: %v", getBody)
	}
}

// TestProjectAPIDeleteFailedProject は、削除に失敗して止まっている(delete_failed)プロジェクトが、
// 取得で status が見え、変更は 409、もう一度の削除は受け付けられて deleting に戻り、
// 再度の開始に失敗したときは delete_failed に戻る(active にはならない)ことを確認する。
func TestProjectAPIDeleteFailedProject(testingT *testing.T) {
	// プロジェクトを作り、ワーカーが失敗を記録した状態(delete_failed)を、直接作る。
	database := testdatabase.New(testingT)
	fakeStarter := &fakeDeletionStarter{}
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database, ProjectDeletionStarter: fakeStarter})
	_, createdBody := apiClient.call(http.MethodPost, "/projects", `{"name":"stuck"}`)
	projectID := createdBody["projectId"].(string)
	if updateError := database.Exec("UPDATE projects SET status = 'delete_failed' WHERE id = ?", projectID).Error; updateError != nil {
		testingT.Fatalf("状態を変えられませんでした: %v", updateError)
	}

	// 取得で、delete_failed が見える。
	testingT.Run("取得で status が delete_failed と見える", func(subTest *testing.T) {
		statusCode, responseBody := apiClient.call(http.MethodGet, "/projects/"+projectID, "")
		if statusCode != 200 || responseBody["status"] != "delete_failed" {
			subTest.Errorf("delete_failed のはず: %d %v", statusCode, responseBody)
		}
	})
	// 名前の変更は、409 になる。
	testingT.Run("delete_failed の間の更新は 409", func(subTest *testing.T) {
		statusCode, responseBody := apiClient.call(http.MethodPatch, "/projects/"+projectID, `{"name":"renamed"}`)
		if statusCode != 409 || errorCodeOf(responseBody) != "conflict" {
			subTest.Errorf("409 のはず: %d %v", statusCode, responseBody)
		}
	})
	// 開始に失敗したときは、delete_failed のまま(active に戻らない)。
	testingT.Run("やり直しの開始に失敗すると、500 で、delete_failed のまま", func(subTest *testing.T) {
		fakeStarter.startError = errors.New("temporal down")
		statusCode, _ := apiClient.call(http.MethodDelete, "/projects/"+projectID, "")
		_, getBody := apiClient.call(http.MethodGet, "/projects/"+projectID, "")
		if statusCode != 500 || getBody["status"] != "delete_failed" {
			subTest.Errorf("500 で、delete_failed のままのはず: %d %v", statusCode, getBody)
		}
	})
	// 開始できれば、受け付けられて、deleting になる。
	testingT.Run("もう一度の削除を受け付けて、deleting になる", func(subTest *testing.T) {
		fakeStarter.startError = nil
		statusCode, _ := apiClient.call(http.MethodDelete, "/projects/"+projectID, "")
		_, getBody := apiClient.call(http.MethodGet, "/projects/"+projectID, "")
		if statusCode != 200 || getBody["status"] != "deleting" {
			subTest.Errorf("200 で deleting になるはず: %d %v", statusCode, getBody)
		}
	})
}
