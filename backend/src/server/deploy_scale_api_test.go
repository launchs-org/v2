package server

import (
	"errors"
	"launchs/shared/database/testdatabase"
	"net/http"
	"testing"
)

// TestDeployAPIScale は、スケーリング(POST .../scale)の、受け付け・同じ数・拒否・入力の検証を確認する。
func TestDeployAPIScale(testingT *testing.T) {
	// active の deploy を用意する(replicas は 1)。
	database := testdatabase.New(testingT)
	fakeStarter := &fakeDeployWorkflowStarter{}
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	deployID := createDeployFor(ownerClient, createProjectFor(ownerClient, "my-app"))
	scalePath := "/deploy/" + deployID + "/scale"
	detail := func() map[string]any {
		_, body := ownerClient.call(http.MethodGet, "/deploy/"+deployID, "")
		return body
	}

	// 起動中(pending)は 409。
	testingT.Run("起動中(pending)の deploy は 409", func(subTest *testing.T) {
		if statusCode, _ := ownerClient.call(http.MethodPost, scalePath, `{"replicas":2}`); statusCode != 409 {
			subTest.Errorf("409 のはず: %d", statusCode)
		}
	})
	database.Exec("UPDATE deploys SET status = 'active' WHERE id = ?", deployID)

	// 入力の検証。
	for _, invalidCase := range []struct{ name, body string }{
		{"replicas が未指定なら 400", `{}`},
		{"replicas が負なら 400", `{"replicas":-1}`},
		{"replicas が上限(5)を超えると 400", `{"replicas":6}`},
		{"replicas が大きくても 400", `{"replicas":1000}`},
		{"replicas が文字列なら 400", `{"replicas":"2"}`},
		{"JSON でないボディは 400", `not json`},
	} {
		testingT.Run(invalidCase.name, func(subTest *testing.T) {
			if statusCode, _ := ownerClient.call(http.MethodPost, scalePath, invalidCase.body); statusCode != 400 {
				subTest.Errorf("400 のはず: %d", statusCode)
			}
		})
	}
	// 他人と存在しない ID は 404。
	testingT.Run("他人・存在しない deploy は 404", func(subTest *testing.T) {
		otherStatus, _ := otherClient.call(http.MethodPost, scalePath, `{"replicas":2}`)
		missingStatus, _ := ownerClient.call(http.MethodPost, "/deploy/deploy-nothing/scale", `{"replicas":2}`)
		if otherStatus != 404 || missingStatus != 404 {
			subTest.Errorf("どちらも 404 のはず: %d %d", otherStatus, missingStatus)
		}
	})
	// 同じ数なら、何も変えず、Workflow も開始しない。
	testingT.Run("今と同じ replicas なら 200 で、Workflow は開始されない", func(subTest *testing.T) {
		statusCode, body := ownerClient.call(http.MethodPost, scalePath, `{"replicas":1}`)
		if statusCode != 200 || body["replicas"] != float64(1) || body["deployId"] != deployID || len(fakeStarter.scaledDeployIDs) != 0 || detail()["status"] != "active" {
			subTest.Errorf("200 で、何も起きないはず: %d %v %v", statusCode, body, fakeStarter.scaledDeployIDs)
		}
	})
	// 0 にできる(replicas が 0 でも受け付ける)。
	testingT.Run("replicas を 0 にすると、applying になり、Workflow が開始される", func(subTest *testing.T) {
		statusCode, body := ownerClient.call(http.MethodPost, scalePath, `{"replicas":0}`)
		if statusCode != 200 || body["replicas"] != float64(0) {
			subTest.Fatalf("200 のはず: %d %v", statusCode, body)
		}
		current := detail()
		if current["status"] != "applying" || current["replicas"] != float64(0) || len(fakeStarter.scaledDeployIDs) != 1 {
			subTest.Errorf("applying・replicas 0・開始1回のはず: %v %v", current, fakeStarter.scaledDeployIDs)
		}
	})
	// 反映中は 409(多重不可)。
	testingT.Run("applying 中のスケーリングは 409", func(subTest *testing.T) {
		if statusCode, _ := ownerClient.call(http.MethodPost, scalePath, `{"replicas":3}`); statusCode != 409 || len(fakeStarter.scaledDeployIDs) != 1 {
			subTest.Errorf("409 で、増えないはず: %d %v", statusCode, fakeStarter.scaledDeployIDs)
		}
	})
	// 削除の途中は 409。
	for _, blockedStatus := range []string{"deleting", "delete_failed"} {
		testingT.Run(blockedStatus+" のスケーリングは 409", func(subTest *testing.T) {
			database.Exec("UPDATE deploys SET status = ? WHERE id = ?", blockedStatus, deployID)
			if statusCode, _ := ownerClient.call(http.MethodPost, scalePath, `{"replicas":3}`); statusCode != 409 {
				subTest.Errorf("409 のはず: %d", statusCode)
			}
		})
	}
	// failed は、同じ数でもスケーリングできる(再試行になる)。scaled_zero からも増やせる。
	for _, allowedStatus := range []string{"failed", "scaled_zero"} {
		testingT.Run(allowedStatus+" の deploy はスケーリングできる", func(subTest *testing.T) {
			database.Exec("UPDATE deploys SET status = ?, replicas = 0 WHERE id = ?", allowedStatus, deployID)
			before := len(fakeStarter.scaledDeployIDs)
			newReplicas := `{"replicas":2}`
			if allowedStatus == "failed" {
				newReplicas = `{"replicas":0}`
			}
			if statusCode, _ := ownerClient.call(http.MethodPost, scalePath, newReplicas); statusCode != 200 || len(fakeStarter.scaledDeployIDs) != before+1 {
				subTest.Errorf("200 で、開始されるはず: %d", statusCode)
			}
		})
	}
}

// TestDeployAPIScaleRevertsWhenWorkflowCannotStart は、Workflow を開始できなかったとき、500 を返し、
// replicas と状態が元に戻ることを確認する。
func TestDeployAPIScaleRevertsWhenWorkflowCannotStart(testingT *testing.T) {
	// deploy を active にして、そのあとから、開始に失敗する状態にする。
	database := testdatabase.New(testingT)
	fakeStarter := &fakeDeployWorkflowStarter{}
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	deployID := createDeployFor(apiClient, createProjectFor(apiClient, "my-app"))
	database.Exec("UPDATE deploys SET status = 'active' WHERE id = ?", deployID)
	fakeStarter.startError = errors.New("temporal down")

	statusCode, body := apiClient.call(http.MethodPost, "/deploy/"+deployID+"/scale", `{"replicas":0}`)
	if statusCode != 500 || errorCodeOf(body) != "internal_error" {
		testingT.Fatalf("500 internal_error のはず: %d %v", statusCode, body)
	}
	_, deployBody := apiClient.call(http.MethodGet, "/deploy/"+deployID, "")
	if deployBody["status"] != "active" || deployBody["replicas"] != float64(1) {
		testingT.Errorf("active と replicas 1 に戻るはず: %v", deployBody)
	}
}
