package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestDeployFilesAPI は、ファイルの取得・保存(pending)・適用・破棄、deploy の詳細への反映、検査(400)、
// 状態(409)、持ち主(404)、認証(401)、マウントとの重なりを確認する。
func TestDeployFilesAPI(testingT *testing.T) {
	const oneFile = `{"files":[{"path":"/etc/app.conf","content":"key=value\n"}]}`
	testingT.Run("取得は、files が空の配列、pending_files が null", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		statusCode, body := environment.ownerClient.call(http.MethodGet, "/deploy/"+environment.deployID+"/files", "")
		files, isArray := body["files"].([]any)
		if statusCode != 200 || !isArray || len(files) != 0 || body["pending_files"] != nil {
			subTest.Errorf("空の配列と null のはず: %d %v", statusCode, body)
		}
	})
	testingT.Run("保存すると pending_files に入り、files は変わらず、Workflow は開始されない。内容も、そのまま返る", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		path := "/deploy/" + environment.deployID + "/files"
		statusCode, response := environment.ownerClient.call(http.MethodPut, path, oneFile)
		_, detail := environment.ownerClient.call(http.MethodGet, path, "")
		pending, _ := detail["pending_files"].([]any)
		current, _ := detail["files"].([]any)
		if statusCode != 200 || len(response) != 0 || len(pending) != 1 || len(current) != 0 || len(environment.deployStarter.filesDeployIDs) != 0 {
			subTest.Fatalf("pending だけが入るはず: %d %v", statusCode, detail)
		}
		first, _ := pending[0].(map[string]any)
		if first["path"] != "/etc/app.conf" || first["content"] != "key=value\n" {
			subTest.Errorf("内容がそのまま返るはず: %v", first)
		}
		// 現在(ファイル無し)と同じ内容(空の配列)を送ると、pending は消える。
		environment.ownerClient.call(http.MethodPut, path, `{"files":[]}`)
		_, same := environment.ownerClient.call(http.MethodGet, path, "")
		if same["pending_files"] != nil {
			subTest.Errorf("現在と同じ内容なら pending は null のはず: %v", same)
		}
	})
	testingT.Run("deploy の詳細にも、files と pending_files が入る", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		_, before := environment.ownerClient.call(http.MethodGet, "/deploy/"+environment.deployID, "")
		beforeFiles, _ := before["files"].(map[string]any)
		if beforeFiles == nil || beforeFiles["pending_files"] != nil {
			subTest.Fatalf("保存前は pending_files が null のはず: %v", before["files"])
		}
		environment.ownerClient.call(http.MethodPut, "/deploy/"+environment.deployID+"/files", oneFile)
		_, after := environment.ownerClient.call(http.MethodGet, "/deploy/"+environment.deployID, "")
		afterFiles, _ := after["files"].(map[string]any)
		pending, _ := afterFiles["pending_files"].([]any)
		if len(pending) != 1 {
			subTest.Errorf("保存した内容が pending_files に入るはず: %v", afterFiles)
		}
	})
	testingT.Run("適用すると applying になり Workflow が開始され、pending が無いと 409", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		path := "/deploy/" + environment.deployID + "/files"
		if statusCode, body := environment.ownerClient.call(http.MethodPost, path+"/apply", ""); statusCode != 409 || errorCodeOf(body) != "conflict" {
			subTest.Fatalf("pending が無ければ 409 のはず: %d %v", statusCode, body)
		}
		environment.ownerClient.call(http.MethodPut, path, oneFile)
		statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		var status string
		environment.database.Table("deploys").Select("status").Where("id = ?", environment.deployID).Scan(&status)
		if statusCode != 200 || status != "applying" || len(environment.deployStarter.filesDeployIDs) != 1 {
			subTest.Errorf("applying になり、Workflow が 1 回開始されるはず: %d %s %v", statusCode, status, environment.deployStarter.filesDeployIDs)
		}
		// 反映中は、保存も、破棄も、もう一度の適用も、できない(409)。
		for _, request := range []struct{ method, suffix, body string }{{http.MethodPut, "", oneFile}, {http.MethodPost, "/discard", ""}, {http.MethodPost, "/apply", ""}} {
			if code, _ := environment.ownerClient.call(request.method, path+request.suffix, request.body); code != 409 {
				subTest.Errorf("反映中の %s %s は 409 のはず: %d", request.method, request.suffix, code)
			}
		}
	})
	testingT.Run("Workflow を開始できなかったら、500 で、状態は元に戻る", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		path := "/deploy/" + environment.deployID + "/files"
		environment.ownerClient.call(http.MethodPut, path, oneFile)
		environment.deployStarter.startError = fmt.Errorf("temporal down")
		statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		var status string
		environment.database.Table("deploys").Select("status").Where("id = ?", environment.deployID).Scan(&status)
		if statusCode != 500 || status != "active" {
			subTest.Errorf("500 で、active に戻るはず: %d %s", statusCode, status)
		}
	})
	testingT.Run("破棄すると pending が消え、もう一度は 409", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		path := "/deploy/" + environment.deployID + "/files"
		environment.ownerClient.call(http.MethodPut, path, oneFile)
		if statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/discard", ""); statusCode != 200 {
			subTest.Fatalf("破棄できるはず: %d", statusCode)
		}
		if statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/discard", ""); statusCode != 409 {
			subTest.Errorf("2 回目は 409 のはず: %d", statusCode)
		}
	})
	testingT.Run("入力が不正なら 400(保存されない)", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		path := "/deploy/" + environment.deployID + "/files"
		tooMany := make([]string, 0, 21)
		for index := 0; index < 21; index++ {
			tooMany = append(tooMany, fmt.Sprintf(`{"path":"/f%d","content":""}`, index))
		}
		for name, requestBody := range map[string]string{
			"files が未指定":      `{}`,
			"JSON でない":        `not json`,
			"path が / で始まらない": `{"files":[{"path":"etc/a","content":""}]}`,
			"path に .. がある":   `{"files":[{"path":"/etc/../a","content":""}]}`,
			"path が重複":        `{"files":[{"path":"/a","content":""},{"path":"/a","content":""}]}`,
			"内容が 64KiB を超える":  `{"files":[{"path":"/a","content":"` + strings.Repeat("x", 64*1024+1) + `"}]}`,
			"21 個":            `{"files":[` + strings.Join(tooMany, ",") + `]}`,
		} {
			if statusCode, body := environment.ownerClient.call(http.MethodPut, path, requestBody); statusCode != 400 || errorCodeOf(body) != "invalid_request" {
				subTest.Errorf("%s は 400 のはず: %d %v", name, statusCode, body)
			}
		}
		if _, detail := environment.ownerClient.call(http.MethodGet, path, ""); detail["pending_files"] != nil {
			subTest.Errorf("不正な入力は、保存されないはず: %v", detail)
		}
	})
	testingT.Run("マウント先と、まったく同じ path のファイルは 400。マウント先の下は、保存できる", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		volumeID := createVolume(environment, "a", 10)
		environment.ownerClient.call(http.MethodPut, "/deploy/"+environment.deployID+"/mounts", fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/data"}]}`, volumeID))
		path := "/deploy/" + environment.deployID + "/files"
		if statusCode, _ := environment.ownerClient.call(http.MethodPut, path, `{"files":[{"path":"/data","content":"x"}]}`); statusCode != 400 {
			subTest.Errorf("マウント先と同じ path は 400 のはず: %d", statusCode)
		}
		if statusCode, _ := environment.ownerClient.call(http.MethodPut, path, `{"files":[{"path":"/data/init.sql","content":"x"}]}`); statusCode != 200 {
			subTest.Errorf("マウント先の下は保存できるはず: %d", statusCode)
		}
		// 逆の向き: pending のファイルと、まったく同じマウント先は、400。
		environment.ownerClient.call(http.MethodPut, path, `{"files":[{"path":"/etc/conf","content":"x"}]}`)
		mountBody := fmt.Sprintf(`{"mounts":[{"volumeId":"%s","mountPath":"/etc/conf"}]}`, volumeID)
		if statusCode, _ := environment.ownerClient.call(http.MethodPut, "/deploy/"+environment.deployID+"/mounts", mountBody); statusCode != 400 {
			subTest.Errorf("ファイルと同じマウント先は 400 のはず: %d", statusCode)
		}
	})
	testingT.Run("他のユーザーの deploy と、存在しない deploy は 404。未認証は 401", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		path := "/deploy/" + environment.deployID + "/files"
		for _, request := range []struct{ method, suffix, body string }{{http.MethodGet, "", ""}, {http.MethodPut, "", oneFile}, {http.MethodPost, "/apply", ""}, {http.MethodPost, "/discard", ""}} {
			if code, _ := environment.otherClient.call(request.method, path+request.suffix, request.body); code != 404 {
				subTest.Errorf("他のユーザーの %s %s は 404 のはず: %d", request.method, request.suffix, code)
			}
			if code, _ := environment.ownerClient.call(request.method, "/deploy/deploy-nothing/files"+request.suffix, request.body); code != 404 {
				subTest.Errorf("存在しない deploy の %s %s は 404 のはず: %d", request.method, request.suffix, code)
			}
		}
	})
	testingT.Run("未認証は 401", func(subTest *testing.T) {
		// DEV_USER_ID が空のサーバーを使う。
		apiClient := newProjectAPIClient(subTest, "", Dependencies{})
		for _, request := range []struct{ method, suffix, body string }{{http.MethodGet, "", ""}, {http.MethodPut, "", oneFile}, {http.MethodPost, "/apply", ""}, {http.MethodPost, "/discard", ""}} {
			statusCode, responseBody := apiClient.call(request.method, "/deploy/deploy-x/files"+request.suffix, request.body)
			if statusCode != http.StatusUnauthorized || errorCodeOf(responseBody) != "unauthorized" {
				subTest.Errorf("%s %s は 401 のはず: %d %v", request.method, request.suffix, statusCode, responseBody)
			}
		}
	})
}
