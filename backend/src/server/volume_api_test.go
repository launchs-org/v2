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

// fakeVolumeWorkflowStarter は、テスト用の VolumeWorkflowStarter。開始を頼まれた ID を覚え、決めたエラーを返す。
type fakeVolumeWorkflowStarter struct {
	appliedVolumeIDs []string
	deletedVolumeIDs []string
	startError       error
}

// StartSizeApply は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeVolumeWorkflowStarter) StartSizeApply(_ context.Context, volumeID string) error {
	fakeStarter.appliedVolumeIDs = append(fakeStarter.appliedVolumeIDs, volumeID)
	return fakeStarter.startError
}

// StartVolumeDeletion は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeVolumeWorkflowStarter) StartVolumeDeletion(_ context.Context, volumeID string) error {
	fakeStarter.deletedVolumeIDs = append(fakeStarter.deletedVolumeIDs, volumeID)
	return fakeStarter.startError
}

// volumeTestEnvironment は、volume の API のテストに必要なものの一式。
type volumeTestEnvironment struct {
	database      *gorm.DB
	volumeStarter *fakeVolumeWorkflowStarter
	deployStarter *fakeDeployWorkflowStarter
	ownerClient   *projectAPIClient
	otherClient   *projectAPIClient
	projectID     string
	deployID      string
	basePath      string
}

// newVolumeTestEnvironment は、プロジェクトと、active の deploy を用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - 用意した環境。
func newVolumeTestEnvironment(testingT *testing.T) *volumeTestEnvironment {
	// 依存するものを差し替えて、2人のユーザーのクライアントを作る。
	database := testdatabase.New(testingT)
	volumeStarter := &fakeVolumeWorkflowStarter{}
	deployStarter := &fakeDeployWorkflowStarter{}
	dependencies := Dependencies{Database: database, DeployWorkflowStarter: deployStarter, VolumeWorkflowStarter: volumeStarter}
	ownerClient := newProjectAPIClient(testingT, "user-a", dependencies)
	otherClient := newProjectAPIClient(testingT, "user-b", dependencies)
	// プロジェクトと deploy を作り、deploy を active にする。
	projectID := createProjectFor(ownerClient, "my-app")
	deployID := createDeployFor(ownerClient, projectID)
	database.Exec("UPDATE deploys SET status = 'active' WHERE id = ?", deployID)
	return &volumeTestEnvironment{
		database: database, volumeStarter: volumeStarter, deployStarter: deployStarter, ownerClient: ownerClient, otherClient: otherClient,
		projectID: projectID, deployID: deployID, basePath: "/projects/" + projectID + "/volumes",
	}
}

// createVolume は、volume を1つ作って、ID を返す。
//
// 引数:
//   - environment: テストの環境。
//   - name: volume の名前。
//   - sizeMB: 大きさ(MB)。
//
// 戻り値:
//   - volume の ID。
func createVolume(environment *volumeTestEnvironment, name string, sizeMB int) string {
	// 持ち主として作る。
	_, body := environment.ownerClient.call(http.MethodPost, environment.basePath, fmt.Sprintf(`{"name":"%s","sizeMB":%d}`, name, sizeMB))
	return body["volumeId"].(string)
}

// TestVolumeAPICreateListGet は、作成・一覧・取得を確認する。
func TestVolumeAPICreateListGet(testingT *testing.T) {
	testingT.Run("作成すると、active で、pending_sizeMB は null で、PVC の Workflow は開始されない", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.basePath, `{"name":"db-data","sizeMB":5120}`)
		if statusCode != 200 || !strings.HasPrefix(body["volumeId"].(string), "volume-") || body["name"] != "db-data" ||
			body["status"] != "active" || body["pending_sizeMB"] != nil || body["sizeMB"] != float64(5120) {
			subTest.Fatalf("仕様の形のはず: %d %v", statusCode, body)
		}
		if _, hasMountedBy := body["mountedBy"]; hasMountedBy {
			subTest.Errorf("作成では mountedBy を返さないはず: %v", body)
		}
	})
	for _, testCase := range []struct {
		name string
		body string
		want int
	}{
		{"name が未指定なら 400", `{"sizeMB":10}`, 400},
		{"sizeMB が未指定なら 400", `{"name":"a"}`, 400},
		{"JSON でないなら 400", `not json`, 400},
		{"name に _ があると 400", `{"name":"a_b","sizeMB":10}`, 400},
		{"name に空白があると 400", `{"name":"a b","sizeMB":10}`, 400},
		{"name が 65 文字なら 400", `{"name":"` + strings.Repeat("a", 65) + `","sizeMB":10}`, 400},
		{"name が 64 文字なら成功", `{"name":"` + strings.Repeat("a", 64) + `","sizeMB":10}`, 200},
		{"sizeMB が 0 なら 400", `{"name":"a","sizeMB":0}`, 400},
		{"sizeMB が負なら 400", `{"name":"a","sizeMB":-1}`, 400},
		{"sizeMB が上限(5120)なら成功", `{"name":"a","sizeMB":5120}`, 200},
		{"sizeMB が上限を超えると 400", `{"name":"a","sizeMB":5121}`, 400},
		{"sizeMB が小数なら 400", `{"name":"a","sizeMB":1.5}`, 400},
	} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			environment := newVolumeTestEnvironment(subTest)
			if statusCode, _ := environment.ownerClient.call(http.MethodPost, environment.basePath, testCase.body); statusCode != testCase.want {
				subTest.Errorf("期待 %d、実際 %d", testCase.want, statusCode)
			}
		})
	}
	testingT.Run("同じプロジェクトで名前が重複すると 409。別のプロジェクトなら作れる", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		createVolume(environment, "data", 10)
		statusCode, body := environment.ownerClient.call(http.MethodPost, environment.basePath, `{"name":"data","sizeMB":10}`)
		otherProject := createProjectFor(environment.ownerClient, "other-app")
		otherStatus, _ := environment.ownerClient.call(http.MethodPost, "/projects/"+otherProject+"/volumes", `{"name":"data","sizeMB":10}`)
		if statusCode != 409 || errorCodeOf(body) != "conflict" || otherStatus != 200 {
			subTest.Errorf("409 と 200 のはず: %d %v %d", statusCode, body, otherStatus)
		}
	})
	testingT.Run("存在しない・他人のプロジェクトは 404、削除中のプロジェクトは 409", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		missingStatus, _ := environment.ownerClient.call(http.MethodPost, "/projects/project-nothing/volumes", `{"name":"a","sizeMB":10}`)
		otherStatus, _ := environment.otherClient.call(http.MethodPost, environment.basePath, `{"name":"a","sizeMB":10}`)
		environment.database.Exec("UPDATE projects SET status = 'deleting' WHERE id = ?", environment.projectID)
		deletingStatus, _ := environment.ownerClient.call(http.MethodPost, environment.basePath, `{"name":"a","sizeMB":10}`)
		if missingStatus != 404 || otherStatus != 404 || deletingStatus != 409 {
			subTest.Errorf("404、404、409 のはず: %d %d %d", missingStatus, otherStatus, deletingStatus)
		}
	})
	testingT.Run("一覧は新しい順で、ページングできる。他人は 404、limit が範囲外なら 400", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		for _, name := range []string{"a", "b", "c"} {
			createVolume(environment, name, 10)
		}
		statusCode, body := environment.ownerClient.call(http.MethodGet, environment.basePath+"?limit=2", "")
		items, _ := body["volumes"].([]any)
		if statusCode != 200 || len(items) != 2 || body["nextCursor"] == nil || items[0].(map[string]any)["name"] != "c" {
			subTest.Fatalf("新しい順で2件、次のページがあるはず: %d %v", statusCode, body)
		}
		_, nextBody := environment.ownerClient.call(http.MethodGet, environment.basePath+"?limit=2&cursor="+body["nextCursor"].(string), "")
		nextItems, _ := nextBody["volumes"].([]any)
		if len(nextItems) != 1 || nextBody["nextCursor"] != nil {
			subTest.Errorf("最後の1件のはず: %v", nextBody)
		}
		otherStatus, _ := environment.otherClient.call(http.MethodGet, environment.basePath, "")
		badStatus, _ := environment.ownerClient.call(http.MethodGet, environment.basePath+"?limit=0", "")
		if otherStatus != 404 || badStatus != 400 {
			subTest.Errorf("404 と 400 のはず: %d %d", otherStatus, badStatus)
		}
	})
	testingT.Run("取得は mountedBy を返し(マウント中の deploy)、他人と存在しない ID は 404", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "data", 10)
		_, empty := environment.ownerClient.call(http.MethodGet, environment.basePath+"/"+volumeID, "")
		if mounted, ok := empty["mountedBy"].([]any); !ok || len(mounted) != 0 {
			subTest.Errorf("空の配列のはず: %v", empty)
		}
		environment.database.Exec(`UPDATE deploys SET mounts = ? WHERE id = ?`, fmt.Sprintf(`[{"volumeId":"%s","mountPath":"/data"}]`, volumeID), environment.deployID)
		_, body := environment.ownerClient.call(http.MethodGet, environment.basePath+"/"+volumeID, "")
		mounted, _ := body["mountedBy"].([]any)
		if len(mounted) != 1 || mounted[0] != environment.deployID {
			subTest.Errorf("deploy が入るはず: %v", body)
		}
		otherStatus, _ := environment.otherClient.call(http.MethodGet, environment.basePath+"/"+volumeID, "")
		missingStatus, _ := environment.ownerClient.call(http.MethodGet, environment.basePath+"/volume-nothing", "")
		if otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("404 のはず: %d %d", otherStatus, missingStatus)
		}
	})
}

// TestVolumeAPIRename は、名前の変更(即時反映)を確認する。
func TestVolumeAPIRename(testingT *testing.T) {
	testingT.Run("名前が変わり、重複は 409、不正は 400", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		createVolume(environment, "b", 10)
		path := environment.basePath + "/" + volumeID
		okStatus, body := environment.ownerClient.call(http.MethodPatch, path, `{"name":"renamed"}`)
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		duplicateStatus, _ := environment.ownerClient.call(http.MethodPatch, path, `{"name":"b"}`)
		badStatus, _ := environment.ownerClient.call(http.MethodPatch, path, `{"name":"a_b"}`)
		sameStatus, _ := environment.ownerClient.call(http.MethodPatch, path, `{"name":"renamed"}`)
		if okStatus != 200 || len(body) != 0 || detail["name"] != "renamed" || duplicateStatus != 409 || badStatus != 400 || sameStatus != 200 {
			subTest.Errorf("結果が違う: %d %v %d %d %d", okStatus, detail, duplicateStatus, badStatus, sameStatus)
		}
	})
	for _, blocked := range []string{"applying", "deleting"} {
		testingT.Run("volume が "+blocked+" の間は 409", func(subTest *testing.T) {
			environment := newVolumeTestEnvironment(subTest)
			volumeID := createVolume(environment, "a", 10)
			environment.database.Exec("UPDATE volumes SET status = ?", blocked)
			if statusCode, _ := environment.ownerClient.call(http.MethodPatch, environment.basePath+"/"+volumeID, `{"name":"x"}`); statusCode != 409 {
				subTest.Errorf("409 のはず: %d", statusCode)
			}
		})
	}
	testingT.Run("他人と存在しない volume は 404", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		otherStatus, _ := environment.otherClient.call(http.MethodPatch, environment.basePath+"/"+volumeID, `{"name":"x"}`)
		missingStatus, _ := environment.ownerClient.call(http.MethodPatch, environment.basePath+"/volume-nothing", `{"name":"x"}`)
		if otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("404 のはず: %d %d", otherStatus, missingStatus)
		}
	})
}

// TestVolumeAPIResize は、拡張(size の保存・適用・破棄)を確認する。
func TestVolumeAPIResize(testingT *testing.T) {
	testingT.Run("保存すると pending_sizeMB に入り、sizeMB は変わらない。現在と同じなら pending は null。縮小と上限超過は 400", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 1000)
		path := environment.basePath + "/" + volumeID
		statusCode, body := environment.ownerClient.call(http.MethodPatch, path+"/size", `{"sizeMB":2000}`)
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		if statusCode != 200 || len(body) != 0 || detail["pending_sizeMB"] != float64(2000) || detail["sizeMB"] != float64(1000) || len(environment.volumeStarter.appliedVolumeIDs) != 0 {
			subTest.Fatalf("pending だけが入るはず: %d %v", statusCode, detail)
		}
		environment.ownerClient.call(http.MethodPatch, path+"/size", `{"sizeMB":1000}`)
		_, same := environment.ownerClient.call(http.MethodGet, path, "")
		shrinkStatus, _ := environment.ownerClient.call(http.MethodPatch, path+"/size", `{"sizeMB":999}`)
		overStatus, _ := environment.ownerClient.call(http.MethodPatch, path+"/size", `{"sizeMB":5121}`)
		zeroStatus, _ := environment.ownerClient.call(http.MethodPatch, path+"/size", `{"sizeMB":0}`)
		if same["pending_sizeMB"] != nil || shrinkStatus != 400 || overStatus != 400 || zeroStatus != 400 {
			subTest.Errorf("結果が違う: %v %d %d %d", same, shrinkStatus, overStatus, zeroStatus)
		}
	})
	testingT.Run("apply は applying にして Workflow を開始する。その間の操作は 409。pending が無いと 409", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 1000)
		path := environment.basePath + "/" + volumeID
		noPending, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		noDiscard, _ := environment.ownerClient.call(http.MethodPost, path+"/discard", "")
		environment.ownerClient.call(http.MethodPatch, path+"/size", `{"sizeMB":2000}`)
		statusCode, body := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		if noPending != 409 || noDiscard != 409 || statusCode != 200 || len(body) != 0 || detail["status"] != "applying" || detail["pending_sizeMB"] != float64(2000) {
			subTest.Fatalf("applying で pending が残るはず: %d %v", statusCode, detail)
		}
		if len(environment.volumeStarter.appliedVolumeIDs) != 1 || environment.volumeStarter.appliedVolumeIDs[0] != volumeID {
			subTest.Errorf("その ID で Workflow が開始されるはず: %v", environment.volumeStarter.appliedVolumeIDs)
		}
		for name, status := range map[string]int{
			"apply":   statusOfVolume(environment, http.MethodPost, path+"/apply", ""),
			"discard": statusOfVolume(environment, http.MethodPost, path+"/discard", ""),
			"size":    statusOfVolume(environment, http.MethodPatch, path+"/size", `{"sizeMB":3000}`),
			"delete":  statusOfVolume(environment, http.MethodDelete, path, ""),
		} {
			if status != 409 {
				subTest.Errorf("%s は 409 のはず: %d", name, status)
			}
		}
	})
	testingT.Run("Workflow を開始できなかったら、500 で、状態を元に戻す(pending は残る)", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 1000)
		path := environment.basePath + "/" + volumeID
		environment.ownerClient.call(http.MethodPatch, path+"/size", `{"sizeMB":2000}`)
		environment.volumeStarter.startError = errors.New("temporal が落ちている")
		statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		if statusCode != 500 || detail["status"] != "active" || detail["pending_sizeMB"] != float64(2000) {
			subTest.Errorf("500 で、active に戻り、pending が残るはず: %d %v", statusCode, detail)
		}
	})
	testingT.Run("discard は pending を消す。failed からも、保存・apply し直せる。他人は 404", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 1000)
		path := environment.basePath + "/" + volumeID
		environment.ownerClient.call(http.MethodPatch, path+"/size", `{"sizeMB":2000}`)
		discardStatus, _ := environment.ownerClient.call(http.MethodPost, path+"/discard", "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		if discardStatus != 200 || detail["pending_sizeMB"] != nil {
			subTest.Fatalf("pending が消えるはず: %d %v", discardStatus, detail)
		}
		environment.ownerClient.call(http.MethodPatch, path+"/size", `{"sizeMB":2000}`)
		environment.database.Exec("UPDATE volumes SET status = 'failed'")
		applyStatus, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		otherStatus, _ := environment.otherClient.call(http.MethodPost, path+"/apply", "")
		if applyStatus != 200 || otherStatus != 404 {
			subTest.Errorf("200 と 404 のはず: %d %d", applyStatus, otherStatus)
		}
	})
}

// statusOfVolume は、持ち主として呼び出して、ステータスコードだけを返す。
//
// 引数:
//   - environment: テストの環境。
//   - method: HTTP のメソッド。
//   - path: パス。
//   - body: ボディ。
//
// 戻り値:
//   - ステータスコード。
func statusOfVolume(environment *volumeTestEnvironment, method string, path string, body string) int {
	// 呼び出して、ステータスコードだけを取り出す。
	statusCode, _ := environment.ownerClient.call(method, path, body)
	return statusCode
}

// TestVolumeAPIDelete は、削除の受け付け・拒否(マウント中を含む)・巻き戻しを確認する。
func TestVolumeAPIDelete(testingT *testing.T) {
	testingT.Run("削除すると deleting になり、Workflow が開始される。再削除は 409", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		path := environment.basePath + "/" + volumeID
		statusCode, body := environment.ownerClient.call(http.MethodDelete, path, "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		if statusCode != 200 || len(body) != 0 || detail["status"] != "deleting" ||
			len(environment.volumeStarter.deletedVolumeIDs) != 1 || environment.volumeStarter.deletedVolumeIDs[0] != volumeID {
			subTest.Fatalf("deleting になり、Workflow が開始されるはず: %d %v", statusCode, detail)
		}
		if again := statusOfVolume(environment, http.MethodDelete, path, ""); again != 409 {
			subTest.Errorf("再削除は 409 のはず: %d", again)
		}
	})
	testingT.Run("マウント中(現在、または適用待ち)の volume は 409。外せば削除できる", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		path := environment.basePath + "/" + volumeID
		mounts := fmt.Sprintf(`[{"volumeId":"%s","mountPath":"/data"}]`, volumeID)
		environment.database.Exec("UPDATE deploys SET mounts = ? WHERE id = ?", mounts, environment.deployID)
		mountedStatus, body := environment.ownerClient.call(http.MethodDelete, path, "")
		environment.database.Exec("UPDATE deploys SET mounts = NULL, pending_mounts = ? WHERE id = ?", mounts, environment.deployID)
		pendingStatus, _ := environment.ownerClient.call(http.MethodDelete, path, "")
		environment.database.Exec("UPDATE deploys SET pending_mounts = NULL WHERE id = ?", environment.deployID)
		freeStatus, _ := environment.ownerClient.call(http.MethodDelete, path, "")
		if mountedStatus != 409 || errorCodeOf(body) != "conflict" || pendingStatus != 409 || freeStatus != 200 {
			subTest.Errorf("409、409、200 のはず: %d %d %d", mountedStatus, pendingStatus, freeStatus)
		}
	})
	testingT.Run("failed の volume は、もう一度削除できる。開始に失敗したら 500 で元に戻る", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		path := environment.basePath + "/" + volumeID
		environment.volumeStarter.startError = errors.New("temporal が落ちている")
		failStatus, _ := environment.ownerClient.call(http.MethodDelete, path, "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		environment.volumeStarter.startError = nil
		environment.database.Exec("UPDATE volumes SET status = 'failed'")
		retryStatus, _ := environment.ownerClient.call(http.MethodDelete, path, "")
		if failStatus != 500 || detail["status"] != "active" || retryStatus != 200 {
			subTest.Errorf("500、active、200 のはず: %d %v %d", failStatus, detail, retryStatus)
		}
	})
	testingT.Run("他人と存在しない volume は 404。プロジェクトのレコードを消すと、volume も消える", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		otherStatus, _ := environment.otherClient.call(http.MethodDelete, environment.basePath+"/"+volumeID, "")
		missingStatus, _ := environment.ownerClient.call(http.MethodDelete, environment.basePath+"/volume-nothing", "")
		if otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("404 のはず: %d %d", otherStatus, missingStatus)
		}
		environment.database.Exec("UPDATE projects SET status = 'deleting' WHERE id = ?", environment.projectID)
		if recordError := repository.NewProjectRepository(environment.database).DeleteRecord(context.Background(), environment.projectID); recordError != nil {
			subTest.Fatalf("削除できるはず: %v", recordError)
		}
		var count int64
		environment.database.Table("volumes").Count(&count)
		if count != 0 {
			subTest.Errorf("volume も消えるはず: %d", count)
		}
	})
}

// TestDeployMountsAPI は、deploy のマウント(取得・保存・適用・破棄)を確認する。
func TestDeployMountsAPI(testingT *testing.T) {
	testingT.Run("取得は、mounts が空の配列、pending_mounts が null", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		statusCode, body := environment.ownerClient.call(http.MethodGet, "/deploy/"+environment.deployID+"/mounts", "")
		mounts, isArray := body["mounts"].([]any)
		if statusCode != 200 || !isArray || len(mounts) != 0 || body["pending_mounts"] != nil {
			subTest.Errorf("空の配列と null のはず: %d %v", statusCode, body)
		}
	})
	testingT.Run("保存すると pending_mounts に入り、mounts は変わらず、Workflow は開始されない。現在と同じ内容なら null", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		path := "/deploy/" + environment.deployID + "/mounts"
		body := fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/data"}]}`, volumeID)
		statusCode, response := environment.ownerClient.call(http.MethodPut, path, body)
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		pending, _ := detail["pending_mounts"].([]any)
		current, _ := detail["mounts"].([]any)
		if statusCode != 200 || len(response) != 0 || len(pending) != 1 || len(current) != 0 || len(environment.deployStarter.mountedDeployIDs) != 0 {
			subTest.Fatalf("pending だけが入るはず: %d %v", statusCode, detail)
		}
		// 現在(マウント無し)と同じ内容(空の配列)を送ると、pending は消える。
		environment.ownerClient.call(http.MethodPut, path, `{"mounts":[]}`)
		_, same := environment.ownerClient.call(http.MethodGet, path, "")
		if same["pending_mounts"] != nil {
			subTest.Errorf("現在と同じ内容なら pending は null のはず: %v", same)
		}
	})
	testingT.Run("入力が不正なら 400(保存されない)", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		otherVolume := createVolume(environment, "b", 10)
		path := "/deploy/" + environment.deployID + "/mounts"
		many := make([]string, 0, 11)
		for index := 0; index < 11; index++ {
			many = append(many, fmt.Sprintf(`{"volumeId":"%s","mountPath":"/p%d"}`, volumeID, index))
		}
		for name, requestBody := range map[string]string{
			"mounts が未指定":          `{}`,
			"JSON でない":             `not json`,
			"mountPath が / で始まらない": fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"data"}]}`, volumeID),
			"mountPath が / だけ":     fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/"}]}`, volumeID),
			"mountPath に ..":       fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/a/../b"}]}`, volumeID),
			"volumeId の形":          `{"mounts":[{"volumeId":"nope","mountPath":"/data"}]}`,
			"volumeId の重複":         fmt.Sprintf(`{"mounts":[{"volumeId":"%[1]s","mountPath":"/a"},{"volumeId":"%[1]s","mountPath":"/b"}]}`, volumeID),
			"mountPath の重複":        fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/a"},{"volumeId":"%s","mountPath":"/a"}]}`, volumeID, otherVolume),
			"11件":                  `{"mounts":[` + strings.Join(many, ",") + `]}`,
			"存在しない volume":         `{"mounts":[{"volumeId":"volume-00000000-0000-4000-8000-000000000000","mountPath":"/data"}]}`,
		} {
			statusCode, _ := environment.ownerClient.call(http.MethodPut, path, requestBody)
			if statusCode != 400 {
				subTest.Errorf("%s: 400 のはず: %d", name, statusCode)
			}
		}
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		if detail["pending_mounts"] != nil {
			subTest.Errorf("保存されないはず: %v", detail)
		}
	})
	testingT.Run("別のプロジェクトの volume と、削除中の volume は 400", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		otherProject := createProjectFor(environment.ownerClient, "other-app")
		_, otherVolume := environment.ownerClient.call(http.MethodPost, "/projects/"+otherProject+"/volumes", `{"name":"x","sizeMB":10}`)
		deletingVolume := createVolume(environment, "d", 10)
		environment.database.Exec("UPDATE volumes SET status = 'deleting' WHERE id = ?", deletingVolume)
		path := "/deploy/" + environment.deployID + "/mounts"
		crossStatus, _ := environment.ownerClient.call(http.MethodPut, path, fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/data"}]}`, otherVolume["volumeId"]))
		deletingStatus, _ := environment.ownerClient.call(http.MethodPut, path, fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/data"}]}`, deletingVolume))
		if crossStatus != 400 || deletingStatus != 400 {
			subTest.Errorf("400 のはず: %d %d", crossStatus, deletingStatus)
		}
	})
	for _, blocked := range []string{"applying", "deleting", "delete_failed"} {
		testingT.Run("deploy が "+blocked+" の間は、保存・破棄が 409", func(subTest *testing.T) {
			environment := newVolumeTestEnvironment(subTest)
			environment.database.Exec("UPDATE deploys SET status = ? WHERE id = ?", blocked, environment.deployID)
			path := "/deploy/" + environment.deployID + "/mounts"
			putStatus, _ := environment.ownerClient.call(http.MethodPut, path, `{"mounts":[]}`)
			discardStatus, _ := environment.ownerClient.call(http.MethodPost, path+"/discard", "")
			if putStatus != 409 || discardStatus != 409 {
				subTest.Errorf("409 のはず: %d %d", putStatus, discardStatus)
			}
		})
	}
	testingT.Run("apply は deploy を applying にして Workflow を開始する。pending が無いと 409。多重 apply は 409", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		path := "/deploy/" + environment.deployID + "/mounts"
		noPending, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		environment.ownerClient.call(http.MethodPut, path, fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/data"}]}`, volumeID))
		statusCode, body := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		again, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		if noPending != 409 || statusCode != 200 || len(body) != 0 || deployStatusOf(environment) != "applying" || detail["pending_mounts"] == nil || again != 409 {
			subTest.Fatalf("applying で pending が残るはず: %d %v", statusCode, detail)
		}
		if len(environment.deployStarter.mountedDeployIDs) != 1 || environment.deployStarter.mountedDeployIDs[0] != environment.deployID {
			subTest.Errorf("その ID で Workflow が開始されるはず: %v", environment.deployStarter.mountedDeployIDs)
		}
	})
	testingT.Run("保存のあとに volume が削除中になっていたら、apply は 409(applying にならない)", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		path := "/deploy/" + environment.deployID + "/mounts"
		environment.ownerClient.call(http.MethodPut, path, fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/data"}]}`, volumeID))
		environment.database.Exec("UPDATE volumes SET status = 'deleting'")
		statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		if statusCode != 409 || deployStatusOf(environment) != "active" || len(environment.deployStarter.mountedDeployIDs) != 0 {
			subTest.Errorf("409 で、active のままのはず: %d", statusCode)
		}
	})
	testingT.Run("起動中(pending)の deploy は apply できない(409)。Workflow を開始できなかったら 500 で元に戻る", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		path := "/deploy/" + environment.deployID + "/mounts"
		environment.ownerClient.call(http.MethodPut, path, fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/data"}]}`, volumeID))
		environment.database.Exec("UPDATE deploys SET status = 'pending' WHERE id = ?", environment.deployID)
		startingStatus, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		environment.database.Exec("UPDATE deploys SET status = 'scaled_zero' WHERE id = ?", environment.deployID)
		environment.deployStarter.startError = errors.New("temporal が落ちている")
		failStatus, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		if startingStatus != 409 || failStatus != 500 || deployStatusOf(environment) != "scaled_zero" {
			subTest.Errorf("409、500、scaled_zero のはず: %d %d %s", startingStatus, failStatus, deployStatusOf(environment))
		}
	})
	testingT.Run("discard は pending を消す。pending が無いと 409。他人と存在しない deploy は 404", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		path := "/deploy/" + environment.deployID + "/mounts"
		noPending, _ := environment.ownerClient.call(http.MethodPost, path+"/discard", "")
		environment.ownerClient.call(http.MethodPut, path, fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/data"}]}`, volumeID))
		discardStatus, _ := environment.ownerClient.call(http.MethodPost, path+"/discard", "")
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		otherStatus, _ := environment.otherClient.call(http.MethodGet, path, "")
		missingStatus, _ := environment.ownerClient.call(http.MethodPut, "/deploy/deploy-nothing/mounts", `{"mounts":[]}`)
		if noPending != 409 || discardStatus != 200 || detail["pending_mounts"] != nil || otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("結果が違う: %d %d %v %d %d", noPending, discardStatus, detail, otherStatus, missingStatus)
		}
	})
}

// deployStatusOf は、deploy の status を返す。
//
// 引数:
//   - environment: テストの環境。
//
// 戻り値:
//   - deploy の status。
func deployStatusOf(environment *volumeTestEnvironment) string {
	// 持ち主として deploy の詳細を取得する。
	_, body := environment.ownerClient.call(http.MethodGet, "/deploy/"+environment.deployID, "")
	return body["status"].(string)
}
