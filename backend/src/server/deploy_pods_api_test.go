package server

import (
	"launchs/shared/database/testdatabase"
	"launchs/shared/model"
	"net/http"
	"testing"
	"time"
)

// TestDeployAPIGetPods は、deploy の詳細の pods が、watcher が DB に書いた Pod の状態(podId は「pod-」+ UID)になり、
// 他の deploy の Pod は含まれず、他のユーザーには見えないことを確認する。
func TestDeployAPIGetPods(testingT *testing.T) {
	// deploy を2件作る。
	database := testdatabase.New(testingT)
	ownerClient := newProjectAPIClient(testingT, "user-a", Dependencies{Database: database})
	otherClient := newProjectAPIClient(testingT, "user-b", Dependencies{Database: database})
	projectID := createProjectFor(ownerClient, "my-app")
	deployID := createDeployFor(ownerClient, projectID)
	otherDeployID := createDeployFor(ownerClient, projectID)
	podsOf := func(client *projectAPIClient, id string) ([]any, int) {
		statusCode, body := client.call(http.MethodGet, "/deploy/"+id, "")
		pods, _ := body["pods"].([]any)
		return pods, statusCode
	}

	// watcher がまだ何も書いていないときは、空の配列。
	testingT.Run("Pod が無いときは、空の配列", func(subTest *testing.T) {
		pods, statusCode := podsOf(ownerClient, deployID)
		if statusCode != 200 || pods == nil || len(pods) != 0 {
			subTest.Errorf("空の配列のはず: %d %v", statusCode, pods)
		}
	})

	// watcher が書いた Pod(作られた順に pending、running、terminating)と、別の deploy の Pod。
	now := time.Now()
	for index, row := range []struct{ uid, deploy, status string }{
		{"11111111-1111-4111-8111-111111111111", deployID, "pending"},
		{"22222222-2222-4222-8222-222222222222", deployID, "running"},
		{"33333333-3333-4333-8333-333333333333", deployID, "terminating"},
		{"44444444-4444-4444-8444-444444444444", otherDeployID, "running"},
	} {
		database.Create(&model.DeployPod{
			PodUID: row.uid, DeployID: row.deploy, ProjectID: projectID, PodName: "pod-" + row.uid[:4], Status: row.status, Phase: "Running",
			PodCreatedAt: now.Add(time.Duration(index) * time.Second), ObservedAt: now,
		})
	}
	testingT.Run("その deploy の Pod だけが、作られた順に、podId と status で返る", func(subTest *testing.T) {
		pods, statusCode := podsOf(ownerClient, deployID)
		if statusCode != 200 || len(pods) != 3 {
			subTest.Fatalf("3件のはず: %d %v", statusCode, pods)
		}
		expected := [][2]string{
			{"pod-11111111-1111-4111-8111-111111111111", "pending"},
			{"pod-22222222-2222-4222-8222-222222222222", "running"},
			{"pod-33333333-3333-4333-8333-333333333333", "terminating"},
		}
		for index, want := range expected {
			pod, _ := pods[index].(map[string]any)
			if pod["podId"] != want[0] || pod["status"] != want[1] || len(pod) != 2 {
				subTest.Errorf("%d 番目が違う: 期待 %v、実際 %v", index, want, pod)
			}
		}
	})
	testingT.Run("他のユーザーには 404 で、Pod は見えない", func(subTest *testing.T) {
		if _, statusCode := podsOf(otherClient, deployID); statusCode != 404 {
			subTest.Errorf("404 のはず: %d", statusCode)
		}
	})
	testingT.Run("config の取得には、pods は含まれない", func(subTest *testing.T) {
		statusCode, body := ownerClient.call(http.MethodGet, "/deploy/"+deployID+"/config", "")
		if _, exists := body["pods"]; statusCode != 200 || exists {
			subTest.Errorf("pods は含まれないはず: %d %v", statusCode, body)
		}
	})
}
