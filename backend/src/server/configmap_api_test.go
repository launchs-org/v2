package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"

	"launchs/shared/database/testdatabase"
)

// configMapTestEnvironment は、ConfigMap の API のテストに必要なものの一式。
type configMapTestEnvironment struct {
	database    *gorm.DB
	ownerClient *projectAPIClient
	otherClient *projectAPIClient
	projectID   string
	basePath    string
}

// newConfigMapTestEnvironment は、2人のユーザーのクライアントと、持ち主のプロジェクトを用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - 用意した環境。
func newConfigMapTestEnvironment(testingT *testing.T) *configMapTestEnvironment {
	// 同じデータベースを使う、2人のユーザーのクライアントを作る。
	database := testdatabase.New(testingT)
	dependencies := Dependencies{Database: database}
	ownerClient := newProjectAPIClient(testingT, "user-a", dependencies)
	otherClient := newProjectAPIClient(testingT, "user-b", dependencies)
	// 持ち主のプロジェクトを作る。
	projectID := createProjectFor(ownerClient, "my-app")
	return &configMapTestEnvironment{database: database, ownerClient: ownerClient, otherClient: otherClient, projectID: projectID, basePath: "/projects/" + projectID + "/configmaps"}
}

// createConfigMap は、ConfigMap を1つ作って、ID を返す。
//
// 引数:
//   - environment: テストの環境。
//   - name: ConfigMap の名前。
//   - filesJSON: files の JSON(例: `[{"key":"a","content":"1"}]`)。
//
// 戻り値:
//   - ConfigMap の ID。
func createConfigMap(environment *configMapTestEnvironment, name string, filesJSON string) string {
	// 持ち主として作る。
	_, body := environment.ownerClient.call(http.MethodPost, environment.basePath, fmt.Sprintf(`{"name":"%s","files":%s}`, name, filesJSON))
	return body["configMapId"].(string)
}

// TestConfigMapAPICreate は、作成(文字列、ファイル、base64、上限、名前、権限)を確認する。
func TestConfigMapAPICreate(testingT *testing.T) {
	testingT.Run("文字列とバイナリ(base64)のファイルで作ると、版 1 で、key の昇順に返る。size は元のバイト数", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		binary := base64.StdEncoding.EncodeToString([]byte{0, 1, 2, 3})
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.basePath,
			`{"name":"app-config","files":[{"key":"b.bin","encoding":"base64","content":"`+binary+`"},{"key":"a.conf","content":"x=1\n"}]}`)
		if statusCode != 200 || !strings.HasPrefix(body["configMapId"].(string), "configmap-") || body["name"] != "app-config" ||
			body["currentRevision"] != float64(1) || body["revision"] != float64(1) {
			subTest.Fatalf("仕様の形のはず: %d %v", statusCode, body)
		}
		files := body["files"].([]any)
		first, second := files[0].(map[string]any), files[1].(map[string]any)
		if first["key"] != "a.conf" || first["encoding"] != "text" || first["size"] != float64(4) ||
			second["key"] != "b.bin" || second["encoding"] != "base64" || second["size"] != float64(4) {
			subTest.Errorf("key の昇順で、encoding の省略は text、size は元のバイト数のはず: %v", files)
		}
	})
	testingT.Run("files を省略しても、空の ConfigMap を作れる", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.basePath, `{"name":"empty"}`)
		if statusCode != 200 || len(body["files"].([]any)) != 0 {
			subTest.Errorf("空のファイルで作れるはず: %d %v", statusCode, body)
		}
	})
	for _, testCase := range []struct {
		name string
		body string
		want int
	}{
		{"name が未指定なら 400", `{"files":[]}`, 400},
		{"name に _ があると 400", `{"name":"a_b"}`, 400},
		{"name が 65 文字なら 400", `{"name":"` + strings.Repeat("a", 65) + `"}`, 400},
		{"JSON でないなら 400", `not json`, 400},
		{"key が未指定なら 400", `{"name":"a","files":[{"content":"x"}]}`, 400},
		{"key に / があると 400", `{"name":"a","files":[{"key":"d/a","content":"x"}]}`, 400},
		{"key が重複すると 400", `{"name":"a","files":[{"key":"k","content":"1"},{"key":"k","content":"2"}]}`, 400},
		{"encoding が不正なら 400", `{"name":"a","files":[{"key":"k","encoding":"hex","content":"00"}]}`, 400},
		{"base64 が不正なら 400", `{"name":"a","files":[{"key":"k","encoding":"base64","content":"!!"}]}`, 400},
		{"text に NUL があると 400", `{"name":"a","files":[{"key":"k","content":"a\u0000b"}]}`, 400},
		{"保存する大きさが上限(1MiB)を超えると 400", `{"name":"a","files":[{"key":"k","content":"` + strings.Repeat("a", 1024*1024) + `"}]}`, 400},
		{"保存する大きさが上限以内(1MiB から key を引いた分)なら 200", `{"name":"a","files":[{"key":"k","content":"` + strings.Repeat("a", 1024*1024-1) + `"}]}`, 200},
		{"本文が大きすぎる(3MiB 超)と 413", `{"name":"a","files":[{"key":"k","content":"` + strings.Repeat("a", 3*1024*1024) + `"}]}`, 413},
	} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			environment := newConfigMapTestEnvironment(subTest)
			if statusCode, _ := environment.ownerClient.call(http.MethodPost, environment.basePath, testCase.body); statusCode != testCase.want {
				subTest.Errorf("期待 %d、実際 %d", testCase.want, statusCode)
			}
		})
	}
	testingT.Run("同じプロジェクトで名前が重複すると 409。別のプロジェクトなら作れる", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		createConfigMap(environment, "conf", `[]`)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.basePath, `{"name":"conf"}`)
		otherProject := createProjectFor(environment.ownerClient, "other-app")
		otherStatus, _ := environment.ownerClient.call(http.MethodPost, "/projects/"+otherProject+"/configmaps", `{"name":"conf"}`)
		if statusCode != 409 || errorCodeOf(body) != "conflict" || otherStatus != 200 {
			subTest.Errorf("409 と 200 のはず: %d %v %d", statusCode, body, otherStatus)
		}
	})
	testingT.Run("1つのプロジェクトに作れるのは 50 個まで。51 個目は 409", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		for index := 0; index < 50; index++ {
			if statusCode, body := environment.ownerClient.call(http.MethodPost, environment.basePath, fmt.Sprintf(`{"name":"c%d"}`, index)); statusCode != 200 {
				subTest.Fatalf("%d 個目は作れるはず: %d %v", index+1, statusCode, body)
			}
		}
		if statusCode, _ := environment.ownerClient.call(http.MethodPost, environment.basePath, `{"name":"c50"}`); statusCode != 409 {
			subTest.Errorf("51 個目は 409 のはず: %d", statusCode)
		}
	})
	testingT.Run("存在しない・他人のプロジェクトは 404、削除中のプロジェクトは 409", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		missingStatus, _ := environment.ownerClient.call(http.MethodPost, "/projects/project-nothing/configmaps", `{"name":"a"}`)
		otherStatus, _ := environment.otherClient.call(http.MethodPost, environment.basePath, `{"name":"a"}`)
		environment.database.Exec("UPDATE projects SET status = 'deleting' WHERE id = ?", environment.projectID)
		deletingStatus, _ := environment.ownerClient.call(http.MethodPost, environment.basePath, `{"name":"a"}`)
		if missingStatus != 404 || otherStatus != 404 || deletingStatus != 409 {
			subTest.Errorf("404、404、409 のはず: %d %d %d", missingStatus, otherStatus, deletingStatus)
		}
	})
}

// TestConfigMapAPIListGet は、一覧(中身なし、ファイルの数と大きさ)、取得、版の指定、他人のものを確認する。
func TestConfigMapAPIListGet(testingT *testing.T) {
	testingT.Run("一覧は、新しい順で、中身は含まず、ファイルの数と大きさを返す", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		createConfigMap(environment, "first", `[{"key":"a","content":"12345"}]`)
		createConfigMap(environment, "second", `[{"key":"a","content":"1"},{"key":"b","content":"22"}]`)
		statusCode, body := environment.ownerClient.call(http.MethodGet, environment.basePath, "")
		items := body["configmaps"].([]any)
		first, second := items[0].(map[string]any), items[1].(map[string]any)
		if statusCode != 200 || len(items) != 2 || first["name"] != "second" || second["name"] != "first" {
			subTest.Fatalf("新しい順のはず: %d %v", statusCode, body)
		}
		if first["fileCount"] != float64(2) || first["totalBytes"] != float64(5) || first["currentRevision"] != float64(1) {
			subTest.Errorf("数と大きさ(key と内容の長さの合計)のはず: %v", first)
		}
		if _, hasFiles := first["files"]; hasFiles || body["nextCursor"] != nil {
			subTest.Errorf("中身は含めず、次のページは無いはず: %v", body)
		}
	})
	testingT.Run("ページングで、limit 件ずつ取れる", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		for index := 0; index < 3; index++ {
			createConfigMap(environment, fmt.Sprintf("c%d", index), `[]`)
		}
		_, firstPage := environment.ownerClient.call(http.MethodGet, environment.basePath+"?limit=2", "")
		cursor, _ := firstPage["nextCursor"].(string)
		_, secondPage := environment.ownerClient.call(http.MethodGet, environment.basePath+"?limit=2&cursor="+cursor, "")
		if len(firstPage["configmaps"].([]any)) != 2 || cursor == "" || len(secondPage["configmaps"].([]any)) != 1 || secondPage["nextCursor"] != nil {
			subTest.Errorf("2件と1件のはず: %v %v", firstPage, secondPage)
		}
	})
	testingT.Run("取得は、中身つき。他人のものと、無いものは 404", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		configMapID := createConfigMap(environment, "conf", `[{"key":"a","content":"hello"}]`)
		statusCode, body := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID, "")
		otherStatus, _ := environment.otherClient.call(http.MethodGet, "/configmaps/"+configMapID, "")
		missingStatus, _ := environment.ownerClient.call(http.MethodGet, "/configmaps/configmap-nothing", "")
		file := body["files"].([]any)[0].(map[string]any)
		if statusCode != 200 || file["content"] != "hello" || body["projectId"] != environment.projectID || otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("中身つきで返り、他人のものは 404 のはず: %d %v %d %d", statusCode, body, otherStatus, missingStatus)
		}
	})
	testingT.Run("一覧は、他人のプロジェクトも、無いプロジェクトも 404", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		otherStatus, _ := environment.otherClient.call(http.MethodGet, environment.basePath, "")
		missingStatus, _ := environment.ownerClient.call(http.MethodGet, "/projects/project-nothing/configmaps", "")
		if otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("404 のはず: %d %d", otherStatus, missingStatus)
		}
	})
}

// TestConfigMapAPIReplace は、全件置き換え(版が増える・同じなら増えない)、版の取得、古い版の削除を確認する。
func TestConfigMapAPIReplace(testingT *testing.T) {
	testingT.Run("違う中身なら版が +1 になり、?revision=1 で前の版を読める", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		configMapID := createConfigMap(environment, "conf", `[{"key":"a","content":"old"}]`)
		statusCode, body := environment.ownerClient.call(http.MethodPut, "/configmaps/"+configMapID, `{"files":[{"key":"a","content":"new"}]}`)
		_, oldBody := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID+"?revision=1", "")
		if statusCode != 200 || body["currentRevision"] != float64(2) || body["revision"] != float64(2) ||
			body["files"].([]any)[0].(map[string]any)["content"] != "new" ||
			oldBody["revision"] != float64(1) || oldBody["currentRevision"] != float64(2) || oldBody["files"].([]any)[0].(map[string]any)["content"] != "old" {
			subTest.Errorf("版 2 と、前の版のはず: %d %v %v", statusCode, body, oldBody)
		}
	})
	testingT.Run("同じ中身(順番が違っても)なら、版は増えず 200", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		configMapID := createConfigMap(environment, "conf", `[{"key":"a","content":"1"},{"key":"b","content":"2"}]`)
		statusCode, body := environment.ownerClient.call(http.MethodPut, "/configmaps/"+configMapID, `{"files":[{"key":"b","content":"2"},{"key":"a","content":"1"}]}`)
		if statusCode != 200 || body["currentRevision"] != float64(1) {
			subTest.Errorf("版は 1 のままのはず: %d %v", statusCode, body)
		}
	})
	testingT.Run("files を空にすると、全て外した版になる。files の省略は 400", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		configMapID := createConfigMap(environment, "conf", `[{"key":"a","content":"1"}]`)
		statusCode, body := environment.ownerClient.call(http.MethodPut, "/configmaps/"+configMapID, `{"files":[]}`)
		missingStatus, _ := environment.ownerClient.call(http.MethodPut, "/configmaps/"+configMapID, `{}`)
		if statusCode != 200 || body["currentRevision"] != float64(2) || len(body["files"].([]any)) != 0 || missingStatus != 400 {
			subTest.Errorf("空の版 2 と、400 のはず: %d %v %d", statusCode, body, missingStatus)
		}
	})
	testingT.Run("不正な中身なら 400 で、版は増えない。他人のものは 404", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		configMapID := createConfigMap(environment, "conf", `[]`)
		badStatus, _ := environment.ownerClient.call(http.MethodPut, "/configmaps/"+configMapID, `{"files":[{"key":"a/b","content":"x"}]}`)
		otherStatus, _ := environment.otherClient.call(http.MethodPut, "/configmaps/"+configMapID, `{"files":[]}`)
		_, body := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID, "")
		if badStatus != 400 || otherStatus != 404 || body["currentRevision"] != float64(1) {
			subTest.Errorf("400、404、版は 1 のはず: %d %d %v", badStatus, otherStatus, body)
		}
	})
	testingT.Run("版は、直近 10 版だけ残る(古い版は 404)", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		configMapID := createConfigMap(environment, "conf", `[{"key":"a","content":"v1"}]`)
		for version := 2; version <= 12; version++ {
			environment.ownerClient.call(http.MethodPut, "/configmaps/"+configMapID, fmt.Sprintf(`{"files":[{"key":"a","content":"v%d"}]}`, version))
		}
		oldStatus, _ := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID+"?revision=2", "")
		keptStatus, _ := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID+"?revision=3", "")
		_, listBody := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID+"/revisions", "")
		revisions := listBody["revisions"].([]any)
		if oldStatus != 404 || keptStatus != 200 || len(revisions) != 10 || revisions[0].(map[string]any)["revision"] != float64(12) || revisions[9].(map[string]any)["revision"] != float64(3) {
			subTest.Errorf("版 3〜12 の 10 件が残るはず: %d %d %v", oldStatus, keptStatus, listBody)
		}
	})
	testingT.Run("revision が不正なら 400、無い版は 404", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		configMapID := createConfigMap(environment, "conf", `[]`)
		zeroStatus, _ := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID+"?revision=0", "")
		textStatus, _ := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID+"?revision=abc", "")
		missingStatus, _ := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID+"?revision=9", "")
		if zeroStatus != 400 || textStatus != 400 || missingStatus != 404 {
			subTest.Errorf("400、400、404 のはず: %d %d %d", zeroStatus, textStatus, missingStatus)
		}
	})
}

// TestConfigMapAPIRenameDelete は、名前の変更、削除、プロジェクトの削除での連動、他人の操作を確認する。
func TestConfigMapAPIRenameDelete(testingT *testing.T) {
	testingT.Run("名前を変えられる。重複は 409、不正な名前は 400、他人は 404", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		configMapID := createConfigMap(environment, "conf", `[]`)
		createConfigMap(environment, "taken", `[]`)
		okStatus, _ := environment.ownerClient.call(http.MethodPatch, "/configmaps/"+configMapID, `{"name":"renamed"}`)
		duplicateStatus, _ := environment.ownerClient.call(http.MethodPatch, "/configmaps/"+configMapID, `{"name":"taken"}`)
		badStatus, _ := environment.ownerClient.call(http.MethodPatch, "/configmaps/"+configMapID, `{"name":"a_b"}`)
		otherStatus, _ := environment.otherClient.call(http.MethodPatch, "/configmaps/"+configMapID, `{"name":"x"}`)
		_, body := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID, "")
		if okStatus != 200 || duplicateStatus != 409 || badStatus != 400 || otherStatus != 404 || body["name"] != "renamed" {
			subTest.Errorf("200、409、400、404 のはず: %d %d %d %d %v", okStatus, duplicateStatus, badStatus, otherStatus, body)
		}
	})
	testingT.Run("削除すると、版ごと消え、同じ名前でまた作れる。他人は 404、2回目は 404", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		configMapID := createConfigMap(environment, "conf", `[{"key":"a","content":"1"}]`)
		otherStatus, _ := environment.otherClient.call(http.MethodDelete, "/configmaps/"+configMapID, "")
		deleteStatus, _ := environment.ownerClient.call(http.MethodDelete, "/configmaps/"+configMapID, "")
		secondStatus, _ := environment.ownerClient.call(http.MethodDelete, "/configmaps/"+configMapID, "")
		recreateStatus, _ := environment.ownerClient.call(http.MethodPost, environment.basePath, `{"name":"conf"}`)
		var revisionCount int64
		environment.database.Table("configmap_revisions").Where("configmap_id = ?", configMapID).Count(&revisionCount)
		if otherStatus != 404 || deleteStatus != 200 || secondStatus != 404 || recreateStatus != 200 || revisionCount != 0 {
			subTest.Errorf("404、200、404、200、版は 0 件のはず: %d %d %d %d %d", otherStatus, deleteStatus, secondStatus, recreateStatus, revisionCount)
		}
	})
	testingT.Run("版の一覧は、他人のものは 404", func(subTest *testing.T) {
		environment := newConfigMapTestEnvironment(subTest)
		configMapID := createConfigMap(environment, "conf", `[]`)
		otherStatus, _ := environment.otherClient.call(http.MethodGet, "/configmaps/"+configMapID+"/revisions", "")
		if otherStatus != 404 {
			subTest.Errorf("404 のはず: %d", otherStatus)
		}
	})
}
