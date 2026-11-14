package server

import (
	"errors"
	"launchs/shared/database/testdatabase"
	"net/http"
	"testing"
)

// TestDeployAPIDelete は、deploy の削除(DELETE /deploy/:deployId)の、受け付けと拒否を確認する。
func TestDeployAPIDelete(testingT *testing.T) {
	// deploy を用意する。
	database := testdatabase.New(testingT)
	fakeStarter := &fakeDeployWorkflowStarter{}
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	deployID := createDeployFor(ownerClient, createProjectFor(ownerClient, "my-app"))
	deployPath := "/deploy/" + deployID
	statusOf := func() string {
		_, body := ownerClient.call(http.MethodGet, deployPath, "")
		return body["status"].(string)
	}

	// 起動中(pending)と反映中(applying)は 409。
	for _, blockedStatus := range []string{"pending", "applying"} {
		testingT.Run(blockedStatus+" の削除は 409 で、状態も Workflow も変わらない", func(subTest *testing.T) {
			database.Exec("UPDATE deploys SET status = ? WHERE id = ?", blockedStatus, deployID)
			statusCode, body := ownerClient.call(http.MethodDelete, deployPath, "")
			if statusCode != 409 || errorCodeOf(body) != "conflict" || statusOf() != blockedStatus || len(fakeStarter.deletedDeployIDs) != 0 {
				subTest.Errorf("409 で、何も変わらないはず: %d %v %s", statusCode, body, statusOf())
			}
		})
	}
	// 他人と存在しない ID は 404。
	testingT.Run("他人・存在しない deploy は 404", func(subTest *testing.T) {
		database.Exec("UPDATE deploys SET status = 'active' WHERE id = ?", deployID)
		otherStatus, _ := otherClient.call(http.MethodDelete, deployPath, "")
		missingStatus, _ := ownerClient.call(http.MethodDelete, "/deploy/deploy-nothing", "")
		if otherStatus != 404 || missingStatus != 404 || statusOf() != "active" {
			subTest.Errorf("どちらも 404 で、削除されないはず: %d %d", otherStatus, missingStatus)
		}
	})
	// 稼働中(replicas 1 以上)でも、そのまま削除できる。
	testingT.Run("稼働中の deploy をそのまま削除でき、deleting になって Workflow が開始される", func(subTest *testing.T) {
		statusCode, body := ownerClient.call(http.MethodDelete, deployPath, "")
		if statusCode != 200 || len(body) != 0 || statusOf() != "deleting" || len(fakeStarter.deletedDeployIDs) != 1 || fakeStarter.deletedDeployIDs[0] != deployID {
			subTest.Errorf("200 で deleting、開始1回のはず: %d %v %s %v", statusCode, body, statusOf(), fakeStarter.deletedDeployIDs)
		}
	})
	// 削除中は、再度の削除も変更系も 409。GET はできる。
	testingT.Run("deleting 中は、再度の削除・スケール・起動設定の更新が 409 で、GET はできる", func(subTest *testing.T) {
		deleteStatus, _ := ownerClient.call(http.MethodDelete, deployPath, "")
		scaleStatus, _ := ownerClient.call(http.MethodPost, deployPath+"/scale", `{"replicas":2}`)
		putStatus, _ := ownerClient.call(http.MethodPut, deployPath+"/config", `{"env":{"A":"1"}}`)
		if deleteStatus != 409 || scaleStatus != 409 || putStatus != 409 || len(fakeStarter.deletedDeployIDs) != 1 {
			subTest.Errorf("全て 409 のはず: %d %d %d", deleteStatus, scaleStatus, putStatus)
		}
		if statusOf() != "deleting" {
			subTest.Errorf("GET は deleting を返すはず")
		}
	})
	// 削除に失敗して止まっている deploy は、もう一度削除できる(やり直し)。
	testingT.Run("delete_failed の deploy は、もう一度削除できる", func(subTest *testing.T) {
		database.Exec("UPDATE deploys SET status = 'delete_failed' WHERE id = ?", deployID)
		statusCode, _ := ownerClient.call(http.MethodDelete, deployPath, "")
		if statusCode != 200 || statusOf() != "deleting" || len(fakeStarter.deletedDeployIDs) != 2 {
			subTest.Errorf("200 で deleting、開始2回目のはず: %d %s %v", statusCode, statusOf(), fakeStarter.deletedDeployIDs)
		}
	})
	// failed と scaled_zero も削除できる。
	for _, allowedStatus := range []string{"failed", "scaled_zero"} {
		testingT.Run(allowedStatus+" の deploy も削除できる", func(subTest *testing.T) {
			database.Exec("UPDATE deploys SET status = ? WHERE id = ?", allowedStatus, deployID)
			if statusCode, _ := ownerClient.call(http.MethodDelete, deployPath, ""); statusCode != 200 {
				subTest.Errorf("200 のはず: %d", statusCode)
			}
		})
	}
}

// TestDeployAPIDeleteRevertsWhenWorkflowCannotStart は、Workflow を開始できなかったとき、
// 500 を返し、状態が元に戻る(deleting のまま残らない)ことを確認する。
func TestDeployAPIDeleteRevertsWhenWorkflowCannotStart(testingT *testing.T) {
	// deploy を failed にして、そのあとから、開始に失敗する状態にする。
	database := testdatabase.New(testingT)
	fakeStarter := &fakeDeployWorkflowStarter{}
	apiClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database, DeployWorkflowStarter: fakeStarter})
	deployID := createDeployFor(apiClient, createProjectFor(apiClient, "my-app"))
	database.Exec("UPDATE deploys SET status = 'failed' WHERE id = ?", deployID)
	fakeStarter.startError = errors.New("temporal down")

	statusCode, body := apiClient.call(http.MethodDelete, "/deploy/"+deployID, "")
	if statusCode != 500 || errorCodeOf(body) != "internal_error" {
		testingT.Fatalf("500 internal_error のはず: %d %v", statusCode, body)
	}
	_, deployBody := apiClient.call(http.MethodGet, "/deploy/"+deployID, "")
	if deployBody["status"] != "failed" {
		testingT.Errorf("failed に戻るはず: %v", deployBody)
	}
}
