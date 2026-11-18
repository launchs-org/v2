package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"launchs/shared/repository"
)

// createConfigMapIn は、volume のテストの環境(プロジェクトと active な deploy)のプロジェクトに、ConfigMap を作って、ID を返す。
//
// 引数:
//   - environment: テストの環境。
//   - name: ConfigMap の名前。
//   - filesJSON: files の JSON。
//
// 戻り値:
//   - ConfigMap の ID。
func createConfigMapIn(environment *volumeTestEnvironment, name string, filesJSON string) string {
	// 持ち主として作る。
	_, body := environment.ownerClient.call(http.MethodPost, "/projects/"+environment.projectID+"/configmaps", fmt.Sprintf(`{"name":"%s","files":%s}`, name, filesJSON))
	return body["configMapId"].(string)
}

// commitConfigMapMounts は、適用(applying)した ConfigMap のマウントを、成功として確定する(Workflow の代わり)。
//
// 引数:
//   - environment: テストの環境。
func commitConfigMapMounts(environment *volumeTestEnvironment) {
	// worker の Activity と同じ操作を、直接呼ぶ。
	repository.NewDeployRepository(environment.database).CommitConfigMapMountsResult(context.Background(), environment.deployID, true)
}

// TestDeployConfigMapMountsAPI は、取得・保存(pending、版の固定)・適用・破棄、検査、重なり、状態、持ち主を確認する。
func TestDeployConfigMapMountsAPI(testingT *testing.T) {
	const configFiles = `[{"key":"app.conf","content":"a=1\n"},{"key":"other.conf","content":"b=2\n"}]`
	mountOne := func(configMapID string, extra string) string {
		return `{"mounts":[{"configMapId":"` + configMapID + `","key":"app.conf","path":"/etc/app/app.conf"` + extra + `}]}`
	}
	testingT.Run("取得は、mounts が空の配列、pending_mounts が null", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		statusCode, body := environment.ownerClient.call(http.MethodGet, "/deploy/"+environment.deployID+"/configmap-mounts", "")
		mounts, isArray := body["mounts"].([]any)
		if statusCode != 200 || !isArray || len(mounts) != 0 || body["pending_mounts"] != nil {
			subTest.Errorf("空の配列と null のはず: %d %v", statusCode, body)
		}
	})
	testingT.Run("保存すると、版は最新に決まり、mode は 0644 になり、pending に入る。Workflow は開始されない", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		path := "/deploy/" + environment.deployID + "/configmap-mounts"
		statusCode, response := environment.ownerClient.call(http.MethodPut, path, mountOne(configMapID, ""))
		_, body := environment.ownerClient.call(http.MethodGet, path, "")
		pending, _ := body["pending_mounts"].([]any)
		if statusCode != 200 || len(response) != 0 || len(pending) != 1 || len(body["mounts"].([]any)) != 0 || len(environment.deployStarter.configMapMountsDeployIDs) != 0 {
			subTest.Fatalf("pending だけが入るはず: %d %v", statusCode, body)
		}
		first := pending[0].(map[string]any)
		if first["configMapId"] != configMapID || first["revision"] != float64(1) || first["latestRevision"] != float64(1) ||
			first["configMapName"] != "app-config" || first["mode"] != "0644" || first["key"] != "app.conf" || first["path"] != "/etc/app/app.conf" {
			subTest.Errorf("版 1、mode 0644、名前つきのはず: %v", first)
		}
	})
	testingT.Run("deploy の詳細にも、configmapMounts が入る", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		environment.ownerClient.call(http.MethodPut, "/deploy/"+environment.deployID+"/configmap-mounts", mountOne(configMapID, ""))
		_, detail := environment.ownerClient.call(http.MethodGet, "/deploy/"+environment.deployID, "")
		mounts, _ := detail["configmapMounts"].(map[string]any)
		pending, _ := mounts["pending_mounts"].([]any)
		if len(mounts["mounts"].([]any)) != 0 || len(pending) != 1 {
			subTest.Errorf("pending に入るはず: %v", detail["configmapMounts"])
		}
	})
	testingT.Run("版の指定が無ければ、すでにマウントしている版を保ち、明示すれば、その版にする(最新にする)", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		path := "/deploy/" + environment.deployID + "/configmap-mounts"
		environment.ownerClient.call(http.MethodPut, path, mountOne(configMapID, ""))
		environment.ownerClient.call(http.MethodPut, "/configmaps/"+configMapID, `{"files":[{"key":"app.conf","content":"a=2\n"}]}`)
		environment.ownerClient.call(http.MethodPut, "/configmaps/"+configMapID, `{"files":[{"key":"app.conf","content":"a=3\n"}]}`)
		// mode だけ変えて、版は省略する → 版 1 のまま。
		environment.ownerClient.call(http.MethodPut, path, mountOne(configMapID, `,"mode":"0600"`))
		_, kept := environment.ownerClient.call(http.MethodGet, path, "")
		keptMount := kept["pending_mounts"].([]any)[0].(map[string]any)
		// 版を明示する → 版 3(latestRevision も 3)。
		environment.ownerClient.call(http.MethodPut, path, mountOne(configMapID, `,"revision":3,"mode":"0600"`))
		_, latest := environment.ownerClient.call(http.MethodGet, path, "")
		latestMount := latest["pending_mounts"].([]any)[0].(map[string]any)
		if keptMount["revision"] != float64(1) || keptMount["latestRevision"] != float64(3) || keptMount["mode"] != "0600" || latestMount["revision"] != float64(3) {
			subTest.Errorf("版 1 を保ち、明示すると版 3 のはず: %v %v", keptMount, latestMount)
		}
	})
	testingT.Run("現在と同じ内容を保存すると、pending は消える", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		path := "/deploy/" + environment.deployID + "/configmap-mounts"
		environment.ownerClient.call(http.MethodPut, path, mountOne(configMapID, ""))
		environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		commitConfigMapMounts(environment)
		environment.ownerClient.call(http.MethodPut, path, mountOne(configMapID, ""))
		_, body := environment.ownerClient.call(http.MethodGet, path, "")
		if body["pending_mounts"] != nil || len(body["mounts"].([]any)) != 1 {
			subTest.Errorf("mounts に 1 件、pending は null のはず: %v", body)
		}
		// 空の配列は、「全て外す」変更として pending に残る。
		environment.ownerClient.call(http.MethodPut, path, `{"mounts":[]}`)
		_, removed := environment.ownerClient.call(http.MethodGet, path, "")
		pending, isArray := removed["pending_mounts"].([]any)
		if !isArray || len(pending) != 0 {
			subTest.Errorf("pending は空の配列のはず: %v", removed)
		}
	})
	testingT.Run("入力が不正なら 400(保存されない)", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		otherProject := createProjectFor(environment.ownerClient, "other-app")
		_, otherBody := environment.ownerClient.call(http.MethodPost, "/projects/"+otherProject+"/configmaps", `{"name":"x","files":[{"key":"app.conf","content":"1"}]}`)
		otherProjectConfigMap := otherBody["configMapId"].(string)
		path := "/deploy/" + environment.deployID + "/configmap-mounts"
		mount := func(fields string) string { return `{"mounts":[{` + fields + `}]}` }
		base := `"configMapId":"` + configMapID + `","key":"app.conf","path":"/etc/a"`
		tooMany := make([]string, 0, 31)
		for index := 0; index < 31; index++ {
			tooMany = append(tooMany, fmt.Sprintf(`{"configMapId":"%s","key":"app.conf","path":"/etc/f%d"}`, configMapID, index))
		}
		for name, requestBody := range map[string]string{
			"mounts が未指定":          `{}`,
			"JSON でない":             `not json`,
			"31 件":                 `{"mounts":[` + strings.Join(tooMany, ",") + `]}`,
			"configMapId の形が不正":    mount(`"configMapId":"volume-1","key":"app.conf","path":"/etc/a"`),
			"ConfigMap が無い":        mount(`"configMapId":"configmap-nothing","key":"app.conf","path":"/etc/a"`),
			"別のプロジェクトの ConfigMap":  mount(`"configMapId":"` + otherProjectConfigMap + `","key":"app.conf","path":"/etc/a"`),
			"key が、ConfigMap に無い":  mount(`"configMapId":"` + configMapID + `","key":"nothing.conf","path":"/etc/a"`),
			"key が不正(/)":           mount(`"configMapId":"` + configMapID + `","key":"a/b","path":"/etc/a"`),
			"path が相対パス":           mount(`"configMapId":"` + configMapID + `","key":"app.conf","path":"etc/a"`),
			"path に .. がある":        mount(`"configMapId":"` + configMapID + `","key":"app.conf","path":"/etc/../a"`),
			"path の末尾が /":          mount(`"configMapId":"` + configMapID + `","key":"app.conf","path":"/etc/"`),
			"mode が 8 進数でない(0999)": mount(base + `,"mode":"0999"`),
			"mode が 3 桁":           mount(base + `,"mode":"644"`),
			"mode が 5 桁":           mount(base + `,"mode":"00644"`),
			"revision が負":          mount(base + `,"revision":-1`),
			"revision が無い版(99)":    mount(base + `,"revision":99`),
			"path が重複":             `{"mounts":[{` + base + `},{` + base + `,"key":"other.conf"}]}`,
		} {
			if statusCode, _ := environment.ownerClient.call(http.MethodPut, path, requestBody); statusCode != 400 {
				subTest.Errorf("%s は 400 のはず: %d", name, statusCode)
			}
		}
		_, body := environment.ownerClient.call(http.MethodGet, path, "")
		if body["pending_mounts"] != nil {
			subTest.Errorf("不正な入力は保存されないはず: %v", body)
		}
	})
	testingT.Run("同じファイルを、別の path に 2 か所マウントできる。mode 0000 と 0777 も、書式として許す", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		body := `{"mounts":[{"configMapId":"` + configMapID + `","key":"app.conf","path":"/etc/a","mode":"0000"},{"configMapId":"` + configMapID + `","key":"app.conf","path":"/etc/b","mode":"0777"}]}`
		if statusCode, response := environment.ownerClient.call(http.MethodPut, "/deploy/"+environment.deployID+"/configmap-mounts", body); statusCode != 200 {
			subTest.Errorf("保存できるはず: %d %v", statusCode, response)
		}
	})
	testingT.Run("ファイルや、ボリュームのマウント先と、まったく同じ path は 400。その下は許す。逆向き(files、mounts の保存)も 400", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		volumeID := createVolume(environment, "data", 10)
		deployPath := "/deploy/" + environment.deployID
		environment.ownerClient.call(http.MethodPut, deployPath+"/files", `{"files":[{"path":"/etc/file.conf","content":"x"}]}`)
		environment.ownerClient.call(http.MethodPut, deployPath+"/mounts", `{"mounts":[{"volumeId":"`+volumeID+`","mountPath":"/data"}]}`)
		put := func(mountPath string) int {
			code, _ := environment.ownerClient.call(http.MethodPut, deployPath+"/configmap-mounts", `{"mounts":[{"configMapId":"`+configMapID+`","key":"app.conf","path":"`+mountPath+`"}]}`)
			return code
		}
		if put("/etc/file.conf") != 400 || put("/data") != 400 || put("/data/app.conf") != 200 {
			subTest.Fatalf("ファイル・マウント先と同じは 400、その下は 200 のはず")
		}
		// 逆向き: ConfigMap のマウント(/data/app.conf)と同じ path のファイル、同じマウント先は 400。
		fileCode, _ := environment.ownerClient.call(http.MethodPut, deployPath+"/files", `{"files":[{"path":"/data/app.conf","content":"x"}]}`)
		mountCode, _ := environment.ownerClient.call(http.MethodPut, deployPath+"/mounts", `{"mounts":[{"volumeId":"`+volumeID+`","mountPath":"/data/app.conf"}]}`)
		if fileCode != 400 || mountCode != 400 {
			subTest.Errorf("逆向きも 400 のはず: %d %d", fileCode, mountCode)
		}
	})
	testingT.Run("ConfigMap のマウントと files の数の合計は 40 まで", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		deployPath := "/deploy/" + environment.deployID
		files := make([]string, 0, 11)
		for index := 0; index < 11; index++ {
			files = append(files, fmt.Sprintf(`{"path":"/files/f%d","content":"x"}`, index))
		}
		mounts := make([]string, 0, 30)
		for index := 0; index < 30; index++ {
			mounts = append(mounts, fmt.Sprintf(`{"configMapId":"%s","key":"app.conf","path":"/cm/f%d"}`, configMapID, index))
		}
		// 30 件のマウントを保存したあと、files を 11 件にすると 41 件になるので、400。10 件なら 40 件で 200。
		okCode, _ := environment.ownerClient.call(http.MethodPut, deployPath+"/configmap-mounts", `{"mounts":[`+strings.Join(mounts, ",")+`]}`)
		overCode, _ := environment.ownerClient.call(http.MethodPut, deployPath+"/files", `{"files":[`+strings.Join(files, ",")+`]}`)
		limitCode, _ := environment.ownerClient.call(http.MethodPut, deployPath+"/files", `{"files":[`+strings.Join(files[:10], ",")+`]}`)
		if okCode != 200 || overCode != 400 || limitCode != 200 {
			subTest.Errorf("200、400、200 のはず: %d %d %d", okCode, overCode, limitCode)
		}
	})
	testingT.Run("適用すると applying になり Workflow が開始され、pending が無いと 409。反映中は、保存も破棄も適用も 409", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		path := "/deploy/" + environment.deployID + "/configmap-mounts"
		if statusCode, body := environment.ownerClient.call(http.MethodPost, path+"/apply", ""); statusCode != 409 || errorCodeOf(body) != "conflict" {
			subTest.Fatalf("pending が無ければ 409 のはず: %d %v", statusCode, body)
		}
		environment.ownerClient.call(http.MethodPut, path, mountOne(configMapID, ""))
		statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		var status string
		environment.database.Table("deploys").Select("status").Where("id = ?", environment.deployID).Scan(&status)
		if statusCode != 200 || status != "applying" || len(environment.deployStarter.configMapMountsDeployIDs) != 1 {
			subTest.Fatalf("applying になり、Workflow が 1 回開始されるはず: %d %s %v", statusCode, status, environment.deployStarter.configMapMountsDeployIDs)
		}
		for _, request := range []struct{ method, suffix, body string }{{http.MethodPut, "", mountOne(configMapID, "")}, {http.MethodPost, "/discard", ""}, {http.MethodPost, "/apply", ""}} {
			if code, _ := environment.ownerClient.call(request.method, path+request.suffix, request.body); code != 409 {
				subTest.Errorf("反映中の %s %s は 409 のはず: %d", request.method, request.suffix, code)
			}
		}
	})
	testingT.Run("Workflow を開始できなかったら、500 で、状態は元に戻る", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		path := "/deploy/" + environment.deployID + "/configmap-mounts"
		environment.ownerClient.call(http.MethodPut, path, mountOne(configMapID, ""))
		environment.deployStarter.startError = fmt.Errorf("temporal down")
		statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		var status string
		environment.database.Table("deploys").Select("status").Where("id = ?", environment.deployID).Scan(&status)
		if statusCode != 500 || status != "active" {
			subTest.Errorf("500 で、active に戻るはず: %d %s", statusCode, status)
		}
	})
	testingT.Run("保存したあとに ConfigMap が消えて(別の経路で)いたら、適用は 409", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		path := "/deploy/" + environment.deployID + "/configmap-mounts"
		environment.ownerClient.call(http.MethodPut, path, mountOne(configMapID, ""))
		environment.database.Exec("DELETE FROM configmaps WHERE id = ?", configMapID)
		statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/apply", "")
		var status string
		environment.database.Table("deploys").Select("status").Where("id = ?", environment.deployID).Scan(&status)
		if statusCode != 409 || status != "active" {
			subTest.Errorf("409 で、状態は変わらないはず: %d %s", statusCode, status)
		}
	})
	testingT.Run("破棄すると pending が消え、もう一度は 409", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		path := "/deploy/" + environment.deployID + "/configmap-mounts"
		environment.ownerClient.call(http.MethodPut, path, mountOne(configMapID, ""))
		if statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/discard", ""); statusCode != 200 {
			subTest.Fatalf("破棄できるはず: %d", statusCode)
		}
		if statusCode, _ := environment.ownerClient.call(http.MethodPost, path+"/discard", ""); statusCode != 409 {
			subTest.Errorf("2 回目は 409 のはず: %d", statusCode)
		}
	})
	testingT.Run("他人の deploy は 404", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		path := "/deploy/" + environment.deployID + "/configmap-mounts"
		for _, request := range []struct{ method, suffix, body string }{{http.MethodGet, "", ""}, {http.MethodPut, "", mountOne(configMapID, "")}, {http.MethodPost, "/apply", ""}, {http.MethodPost, "/discard", ""}} {
			if code, _ := environment.otherClient.call(request.method, path+request.suffix, request.body); code != 404 {
				subTest.Errorf("他人の %s %s は 404 のはず: %d", request.method, request.suffix, code)
			}
		}
	})
}

// TestConfigMapUsageAPI は、使用中の ConfigMap の、usedBy、usage、削除の 409、使っている版の保護を確認する。
func TestConfigMapUsageAPI(testingT *testing.T) {
	const configFiles = `[{"key":"app.conf","content":"a=1\n"}]`
	mountBody := func(configMapID string) string {
		return `{"mounts":[{"configMapId":"` + configMapID + `","key":"app.conf","path":"/etc/app.conf"}]}`
	}
	testingT.Run("pending のマウントも、使用中として数え、usage に出て、削除は 409(本文に deploy の ID)。外すと削除できる", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		deployPath := "/deploy/" + environment.deployID
		environment.ownerClient.call(http.MethodPut, deployPath+"/configmap-mounts", mountBody(configMapID))
		_, usage := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID+"/usage", "")
		usages := usage["usages"].([]any)
		_, list := environment.ownerClient.call(http.MethodGet, "/projects/"+environment.projectID+"/configmaps", "")
		usedBy := list["configmaps"].([]any)[0].(map[string]any)["usedBy"]
		statusCode, body := environment.ownerClient.call(http.MethodDelete, "/configmaps/"+configMapID, "")
		first := usages[0].(map[string]any)
		if len(usages) != 1 || first["deployId"] != environment.deployID || first["pending"] != true || first["revision"] != float64(1) || first["path"] != "/etc/app.conf" || usedBy != float64(1) {
			subTest.Errorf("pending の使用が出るはず: %v usedBy=%v", usage, usedBy)
		}
		if statusCode != 409 || errorCodeOf(body) != "conflict" || !strings.Contains(fmt.Sprint(body), environment.deployID) {
			subTest.Errorf("409 で、deploy の ID を含むはず: %d %v", statusCode, body)
		}
		// pending を破棄すると、使われなくなるので、削除できる。
		environment.ownerClient.call(http.MethodPost, deployPath+"/configmap-mounts/discard", "")
		if code, _ := environment.ownerClient.call(http.MethodDelete, "/configmaps/"+configMapID, ""); code != 200 {
			subTest.Errorf("削除できるはず: %d", code)
		}
	})
	testingT.Run("適用済みのマウントも使用中で、pending: false で出る", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		deployPath := "/deploy/" + environment.deployID
		environment.ownerClient.call(http.MethodPut, deployPath+"/configmap-mounts", mountBody(configMapID))
		environment.ownerClient.call(http.MethodPost, deployPath+"/configmap-mounts/apply", "")
		commitConfigMapMounts(environment)
		_, usage := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID+"/usage", "")
		first := usage["usages"].([]any)[0].(map[string]any)
		code, _ := environment.ownerClient.call(http.MethodDelete, "/configmaps/"+configMapID, "")
		if first["pending"] != false || code != 409 {
			subTest.Errorf("適用済みは pending: false で、削除は 409 のはず: %v %d", first, code)
		}
	})
	testingT.Run("使っている版は、直近 10 版を超えても、消されない。使われていない古い版は消える", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		deployPath := "/deploy/" + environment.deployID
		environment.ownerClient.call(http.MethodPut, deployPath+"/configmap-mounts", mountBody(configMapID))
		for version := 2; version <= 12; version++ {
			environment.ownerClient.call(http.MethodPut, "/configmaps/"+configMapID, fmt.Sprintf(`{"files":[{"key":"app.conf","content":"v%d"}]}`, version))
		}
		usedStatus, _ := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID+"?revision=1", "")
		unusedStatus, _ := environment.ownerClient.call(http.MethodGet, "/configmaps/"+configMapID+"?revision=2", "")
		if usedStatus != 200 || unusedStatus != 404 {
			subTest.Errorf("使っている版 1 は残り、版 2 は消えるはず: %d %d", usedStatus, unusedStatus)
		}
	})
	testingT.Run("usage は、他人のものは 404", func(subTest *testing.T) {
		environment := newVolumeTestEnvironment(subTest)
		configMapID := createConfigMapIn(environment, "app-config", configFiles)
		if code, _ := environment.otherClient.call(http.MethodGet, "/configmaps/"+configMapID+"/usage", ""); code != 404 {
			subTest.Errorf("404 のはず: %d", code)
		}
	})
}
