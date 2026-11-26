package server

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"launchs/shared/credential"
	"launchs/shared/googledrive/fakegoogle"
	"launchs/shared/safehttp"
)

// newExportTestEnvironment は、アクセスログの転送の試験の環境(暗号化の鍵、偽の Google、内部の宛先を許す設定)を用意する。
//
// 引数:
//   - testingT: テスト。
//   - withGoogle: Google の OAuth を設定するか(false なら、Drive は使えない)。
//
// 戻り値:
//   - ingress の API の環境と、偽の Google。
func newExportTestEnvironment(testingT *testing.T, withGoogle bool) (*ingressTestEnvironment, *fakegoogle.Server) {
	// 暗号化の鍵と、偽の Google を用意する。
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))
	cipher, _ := credential.NewCipher(key)
	fake := fakegoogle.New()
	testingT.Cleanup(fake.HTTP.Close)
	dependencies := &ExportDependencies{Cipher: cipher, CredentialKey: key, SafeOptions: safehttp.Options{AllowHTTP: true, AllowPrivateNetworks: true}}
	if withGoogle {
		dependencies.Google = fake.Config()
	}
	return newIngressTestEnvironmentWithExport(testingT, dependencies), fake
}

// TestLogExportWebhookAPI は、webhook の作成(秘密は 1 回だけ)、一覧(URL のクエリは伏せる)、変更、テスト送信、削除、他人からの見え方を確かめる。
func TestLogExportWebhookAPI(testingT *testing.T) {
	environment, _ := newExportTestEnvironment(testingT, false)
	ingressID := createIngress(environment)
	path := environment.basePath + "/" + ingressID + "/log-exports"
	// 受け口(テスト送信を受け取る)。
	receiver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) }))
	defer receiver.Close()
	// 作れる(201)。署名の秘密が、このレスポンスにだけ入る。
	statusCode, body := environment.ownerClient.call(http.MethodPost, path, `{"kind":"webhook","url":"`+receiver.URL+`/hook?token=SECRETQUERY","intervalSeconds":60}`)
	if statusCode != http.StatusCreated || body["kind"] != "webhook" || body["enabled"] != true || body["status"] != "active" || body["intervalSeconds"] != float64(60) || !strings.HasPrefix(body["secret"].(string), "whsec_") {
		testingT.Fatalf("作れるはず: %d %v", statusCode, body)
	}
	// URL のクエリは、伏せられる。
	if shown := body["url"].(string); strings.Contains(shown, "SECRETQUERY") || !strings.HasSuffix(shown, "/hook?…") {
		testingT.Errorf("クエリは伏せるはず: %s", shown)
	}
	logExportID := body["logExportId"].(string)
	// 一覧には、秘密が出ない。
	statusCode, body = environment.ownerClient.call(http.MethodGet, path, "")
	logExports, _ := body["logExports"].([]any)
	if statusCode != http.StatusOK || len(logExports) != 1 || logExports[0].(map[string]any)["secret"] != nil {
		testingT.Fatalf("一覧に 1 つ、秘密なしで出るはず: %d %v", statusCode, body)
	}
	// 不正な入力は 400: 間隔、種類、URL なし。
	for _, invalid := range []string{`{"kind":"webhook","url":"` + receiver.URL + `","intervalSeconds":7}`, `{"kind":"smtp"}`, `{"kind":"webhook"}`, `{}`} {
		if invalidStatus, _ := environment.ownerClient.call(http.MethodPost, path, invalid); invalidStatus != http.StatusBadRequest {
			testingT.Errorf("%s は 400 のはず: %d", invalid, invalidStatus)
		}
	}
	// 変更できる(無効にする。間隔を変える)。
	statusCode, body = environment.ownerClient.call(http.MethodPatch, path+"/"+logExportID, `{"enabled":false,"intervalSeconds":10}`)
	if statusCode != http.StatusOK || body["enabled"] != false || body["intervalSeconds"] != float64(10) {
		testingT.Errorf("変更できるはず: %d %v", statusCode, body)
	}
	if badStatus, _ := environment.ownerClient.call(http.MethodPatch, path+"/"+logExportID, `{"intervalSeconds":11}`); badStatus != http.StatusBadRequest {
		testingT.Errorf("不正な間隔は 400 のはず: %d", badStatus)
	}
	// テスト送信は、200 で、ok。受け口が落ちていれば、ok=false と理由(URL は含まない)。
	statusCode, body = environment.ownerClient.call(http.MethodPost, path+"/"+logExportID+"/test", "")
	if statusCode != http.StatusOK || body["ok"] != true {
		testingT.Errorf("テスト送信は ok のはず: %d %v", statusCode, body)
	}
	receiver.Close()
	statusCode, body = environment.ownerClient.call(http.MethodPost, path+"/"+logExportID+"/test", "")
	if statusCode != http.StatusOK || body["ok"] != false || body["message"] == "" || strings.Contains(body["message"].(string), "SECRETQUERY") {
		testingT.Errorf("失敗は ok=false と理由のはず: %d %v", statusCode, body)
	}
	// 他人からは、見えず、触れない。
	for _, attempt := range []struct{ method, target string }{{http.MethodGet, path}, {http.MethodPatch, path + "/" + logExportID}, {http.MethodDelete, path + "/" + logExportID}, {http.MethodPost, path + "/" + logExportID + "/test"}} {
		if otherStatus, _ := environment.otherClient.call(attempt.method, attempt.target, "{}"); otherStatus != http.StatusNotFound {
			testingT.Errorf("他人の %s %s は 404 のはず: %d", attempt.method, attempt.target, otherStatus)
		}
	}
	// 削除できる。無いものは 404。
	if deleteStatus, _ := environment.ownerClient.call(http.MethodDelete, path+"/"+logExportID, ""); deleteStatus != http.StatusOK {
		testingT.Errorf("削除できるはず: %d", deleteStatus)
	}
	if missingStatus, _ := environment.ownerClient.call(http.MethodDelete, path+"/"+logExportID, ""); missingStatus != http.StatusNotFound {
		testingT.Errorf("無いものは 404 のはず: %d", missingStatus)
	}
}

// TestLogExportStrictURLAPI は、本番と同じ(ゆるめない)設定で、http と内部の宛先の URL を断ることを確かめる。
func TestLogExportStrictURLAPI(testingT *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))
	cipher, _ := credential.NewCipher(key)
	environment := newIngressTestEnvironmentWithExport(testingT, &ExportDependencies{Cipher: cipher, CredentialKey: key})
	ingressID := createIngress(environment)
	path := environment.basePath + "/" + ingressID + "/log-exports"
	for _, invalid := range []string{"http://example.com/x", "https://localhost/x", "https://127.0.0.1/x", "https://169.254.169.254/latest", "https://10.0.0.5/x", "https://user:pass@example.com/x", "ftp://example.com/x"} {
		if statusCode, _ := environment.ownerClient.call(http.MethodPost, path, `{"kind":"webhook","url":"`+invalid+`"}`); statusCode != http.StatusBadRequest {
			testingT.Errorf("%s は 400 のはず: %d", invalid, statusCode)
		}
	}
	if statusCode, _ := environment.ownerClient.call(http.MethodPost, path, `{"kind":"webhook","url":"https://example.com/hook"}`); statusCode != http.StatusCreated {
		testingT.Errorf("公開の https は作れるはず: %d", statusCode)
	}
}

// TestLogExportLimitAPI は、アカウントごとに 10 個までで、超えると 409 になることを確かめる。
func TestLogExportLimitAPI(testingT *testing.T) {
	environment, _ := newExportTestEnvironment(testingT, false)
	ingressID := createIngress(environment)
	path := environment.basePath + "/" + ingressID + "/log-exports"
	for index := 0; index < 10; index++ {
		if statusCode, body := environment.ownerClient.call(http.MethodPost, path, `{"kind":"webhook","url":"https://example.com/h`+strings.Repeat("x", index)+`"}`); statusCode != http.StatusCreated {
			testingT.Fatalf("10 個までは作れるはず(%d): %d %v", index, statusCode, body)
		}
	}
	if statusCode, _ := environment.ownerClient.call(http.MethodPost, path, `{"kind":"webhook","url":"https://example.com/over"}`); statusCode != http.StatusConflict {
		testingT.Errorf("11 個目は 409 のはず: %d", statusCode)
	}
}

// TestLogExportDisabledAPI は、暗号化の鍵が無いとき、作る・テストが 503 になり、状態は返ることを確かめる。
func TestLogExportDisabledAPI(testingT *testing.T) {
	environment := newIngressTestEnvironmentWithExport(testingT, &ExportDependencies{})
	ingressID := createIngress(environment)
	path := environment.basePath + "/" + ingressID + "/log-exports"
	if statusCode, _ := environment.ownerClient.call(http.MethodPost, path, `{"kind":"webhook","url":"https://example.com/h"}`); statusCode != http.StatusServiceUnavailable {
		testingT.Errorf("機能が無効なら 503 のはず: %d", statusCode)
	}
	if statusCode, _ := environment.ownerClient.call(http.MethodGet, path, ""); statusCode != http.StatusOK {
		testingT.Errorf("一覧は返るはず: %d", statusCode)
	}
	statusCode, body := environment.ownerClient.call(http.MethodGet, "/account/google-drive", "")
	if statusCode != http.StatusOK || body["available"] != false || body["connected"] != false {
		testingT.Errorf("状態は、使えない旨で返るはず: %d %v", statusCode, body)
	}
	if authorizeStatus, _ := environment.ownerClient.call(http.MethodPost, "/account/google-drive/authorize", ""); authorizeStatus != http.StatusServiceUnavailable {
		testingT.Errorf("Drive が使えなければ 503 のはず: %d", authorizeStatus)
	}
}

// TestGoogleDriveAPI は、接続の流れ(同意の URL、戻り先、状態、Drive の転送、解除で転送も消える)を、偽の Google で確かめる。
func TestGoogleDriveAPI(testingT *testing.T) {
	environment, fake := newExportTestEnvironment(testingT, true)
	ingressID := createIngress(environment)
	path := environment.basePath + "/" + ingressID + "/log-exports"
	// つなぐ前: 使えるが、つないでいない。Drive の転送は、作れない(409)。
	statusCode, body := environment.ownerClient.call(http.MethodGet, "/account/google-drive", "")
	if statusCode != http.StatusOK || body["available"] != true || body["connected"] != false {
		testingT.Fatalf("つなぐ前の状態が違う: %d %v", statusCode, body)
	}
	if driveStatus, _ := environment.ownerClient.call(http.MethodPost, path, `{"kind":"google_drive"}`); driveStatus != http.StatusConflict {
		testingT.Errorf("つなぐ前は 409 のはず: %d", driveStatus)
	}
	// 同意の URL を得る。state が入っている。
	statusCode, body = environment.ownerClient.call(http.MethodPost, "/account/google-drive/authorize", "")
	authorizeURL, _ := body["url"].(string)
	parsed, _ := url.Parse(authorizeURL)
	state := parsed.Query().Get("state")
	if statusCode != http.StatusOK || state == "" || !strings.Contains(authorizeURL, "drive.file") {
		testingT.Fatalf("同意の URL に state が入るはず: %d %s", statusCode, authorizeURL)
	}
	// 戻り先(認証なし)。不正な state は error、合っていれば connected(画面へのリダイレクト)。
	callback := func(query string) string {
		recorder := httptest.NewRecorder()
		environment.ownerClient.httpServer.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/export/google/callback?"+query, nil))
		if recorder.Code != http.StatusFound {
			testingT.Fatalf("戻り先は 302 のはず: %d", recorder.Code)
		}
		return recorder.Header().Get("Location")
	}
	if location := callback("code=abc&state=tampered"); location != "/?google=error" {
		testingT.Errorf("不正な state は error のはず: %s", location)
	}
	if location := callback("error=access_denied&state=" + url.QueryEscape(state)); location != "/?google=denied" {
		testingT.Errorf("同意しなかったときは denied のはず: %s", location)
	}
	if location := callback("code=abc&state=" + url.QueryEscape(state)); location != "/?google=connected" {
		testingT.Fatalf("つながるはず: %s", location)
	}
	// つながった。メールアドレスが出る。合鍵は、暗号化されて保存される(平文が DB に無い)。
	statusCode, body = environment.ownerClient.call(http.MethodGet, "/account/google-drive", "")
	if statusCode != http.StatusOK || body["connected"] != true || body["accountEmail"] != "me@example.com" || body["status"] != "active" {
		testingT.Fatalf("つながった状態のはず: %d %v", statusCode, body)
	}
	var stored []byte
	environment.database.Raw("SELECT credential FROM google_drive_connections WHERE user_id = 'user-a'").Scan(&stored)
	if len(stored) == 0 || strings.Contains(string(stored), "refresh-abc") {
		testingT.Errorf("合鍵は、暗号化されて保存されるはず")
	}
	// 他人の状態には、影響しない。
	if _, otherBody := environment.otherClient.call(http.MethodGet, "/account/google-drive", ""); otherBody["connected"] != false {
		testingT.Errorf("他人は、つながっていないはず: %v", otherBody)
	}
	// Drive の転送を作れる。同じ ingress に 2 つ目は 409。テスト送信で、Drive にファイルができる。
	statusCode, body = environment.ownerClient.call(http.MethodPost, path, `{"kind":"google_drive"}`)
	if statusCode != http.StatusCreated || body["kind"] != "google_drive" || body["secret"] != nil || body["url"] != nil {
		testingT.Fatalf("Drive の転送を作れるはず: %d %v", statusCode, body)
	}
	logExportID := body["logExportId"].(string)
	if duplicateStatus, _ := environment.ownerClient.call(http.MethodPost, path, `{"kind":"google_drive"}`); duplicateStatus != http.StatusConflict {
		testingT.Errorf("Drive の 2 つ目は 409 のはず: %d", duplicateStatus)
	}
	if urlStatus, _ := environment.ownerClient.call(http.MethodPatch, path+"/"+logExportID, `{"url":"https://example.com/x"}`); urlStatus != http.StatusBadRequest {
		testingT.Errorf("Drive の URL は変えられないはず: %d", urlStatus)
	}
	statusCode, body = environment.ownerClient.call(http.MethodPost, path+"/"+logExportID+"/test", "")
	if statusCode != http.StatusOK || body["ok"] != true || len(fake.ChildrenNamed(fake.ChildrenNamed(fake.ChildrenNamed("", "launchs-access-logs")[0].ID, "my-app")[0].ID, "launchs-connection-test.txt")) != 1 {
		testingT.Errorf("テスト送信で、ファイルができるはず: %d %v", statusCode, body)
	}
	// 解除すると、Google の側で取り消され、Drive の転送も消える。つないでいないので、もう一度の解除は 404。
	if disconnectStatus, _ := environment.ownerClient.call(http.MethodDelete, "/account/google-drive", ""); disconnectStatus != http.StatusOK {
		testingT.Fatalf("解除できるはず: %d", disconnectStatus)
	}
	if len(fake.RevokedTokens) != 1 || fake.RevokedTokens[0] != "refresh-abc" {
		testingT.Errorf("Google の側で、取り消されるはず: %v", fake.RevokedTokens)
	}
	if _, listBody := environment.ownerClient.call(http.MethodGet, path, ""); len(listBody["logExports"].([]any)) != 0 {
		testingT.Errorf("Drive の転送も消えるはず: %v", listBody)
	}
	if againStatus, _ := environment.ownerClient.call(http.MethodDelete, "/account/google-drive", ""); againStatus != http.StatusNotFound {
		testingT.Errorf("つないでいなければ 404 のはず: %d", againStatus)
	}
}
