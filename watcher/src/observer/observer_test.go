package observer

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"gorm.io/gorm"

	"launchs/shared/database/testdatabase"
	"launchs/shared/k8s"
	"launchs/shared/logger"
	"launchs/shared/model"
	"launchs/shared/repository"
)

// waitFor は、条件が成り立つまで、短い間隔で待つ。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - description: 待っているものの説明(失敗したときの表示)。
//   - condition: 成り立つべき条件。
func waitFor(testingT *testing.T, description string, condition func() bool) {
	// 失敗したとき、この関数ではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()
	// 期限まで、条件を確かめ続ける。
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	testingT.Fatalf("期限までに成り立たなかった: %s", description)
}

// newObserverTestEnvironment は、使い捨てのデータベースに deploy を1件入れ、fake の k8s と、それをつなぐ Observer を作る。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - データベース。
//   - fake の k8s のクライアント。
//   - Observer。
func newObserverTestEnvironment(testingT *testing.T) (*gorm.DB, *fake.Clientset, *Observer) {
	// deploy が1件ある状態のデータベースを作る。
	database := testdatabase.New(testingT)
	database.Create(&model.Deploy{
		ID: "deploy-1", ProjectID: "project-1", UserID: "user-a", ImageRef: "nginx", Spec: "small",
		Replicas: 1, Status: model.DeployStatusActive, Env: []byte(`{}`),
	})
	clientset := fake.NewSimpleClientset()
	testLogger := logger.New(&bytes.Buffer{}, "test", slog.LevelInfo)
	return database, clientset, New(clientset, repository.NewDeployObservationRepository(database), testLogger)
}

// runObserver は、Observer をバックグラウンドで動かし、最初の全件の反映が終わるまで待つ。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - observer: 動かす Observer。
//
// 戻り値:
//   - 止めるための関数(テストの終わりに自動でも呼ばれる)。
func runObserver(testingT *testing.T, observer *Observer) context.CancelFunc {
	// テストが終わるとき、止める。
	ctx, cancel := context.WithCancel(context.Background())
	testingT.Cleanup(cancel)
	go func() { _ = observer.Run(ctx) }()
	// 最初の反映が終わるまで待つ。
	select {
	case <-observer.Synced():
	case <-time.After(10 * time.Second):
		testingT.Fatalf("最初の反映が終わらなかった")
	}
	// fake の k8s は、一覧と watch の間に起きた変更を取りこぼすことがあるので、watch が始まるのを少し待つ。
	time.Sleep(300 * time.Millisecond)
	return cancel
}

// podTypes は、deploy-1 の Pod のイベントの種類を、古い順に返す。
//
// 引数:
//   - database: データベース。
//
// 戻り値:
//   - イベントの種類。
func podTypes(database *gorm.DB) []string {
	// 連番の順に、種類だけを読む。
	var types []string
	database.Table("deploy_pod_events").Where("deploy_id = ?", "deploy-1").Order("id").Pluck("type", &types)
	return types
}

// TestObserverFollowsClusterEvents は、k8s の Deployment と Pod の変化(作成、Ready、クラッシュ、削除)が、
// DB の観測値と、Pod のイベントに反映されることを確認する。
func TestObserverFollowsClusterEvents(testingT *testing.T) {
	database, clientset, observer := newObserverTestEnvironment(testingT)
	runObserver(testingT, observer)
	ctx := context.Background()

	// Deployment が作られると、観測値が DB に入る。
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "deploy-1", Namespace: "project-1", Generation: 1, Labels: map[string]string{k8s.DeployIDLabel: "deploy-1"}},
		Status:     appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, ReadyReplicas: 0},
	}
	if _, createError := clientset.AppsV1().Deployments("project-1").Create(ctx, deployment, metav1.CreateOptions{}); createError != nil {
		testingT.Fatalf("作れるはず: %v", createError)
	}
	waitFor(testingT, "Deployment の観測値が入る", func() bool {
		var count int64
		database.Table("deploy_observations").Where("deploy_id = ? AND ready_replicas = 0", "deploy-1").Count(&count)
		return count == 1
	})
	// Deployment の status が変わると、観測値が更新される。
	deployment.Status.ReadyReplicas, deployment.Status.AvailableReplicas = 1, 1
	_, _ = clientset.AppsV1().Deployments("project-1").Update(ctx, deployment, metav1.UpdateOptions{})
	waitFor(testingT, "Deployment の観測値が更新される", func() bool {
		var count int64
		database.Table("deploy_observations").Where("deploy_id = ? AND ready_replicas = 1", "deploy-1").Count(&count)
		return count == 1
	})

	// Pod が作られる(まだ起動していない)と、行と created のイベントが入る。
	pendingPod := newTestPodObject(func(pod *corev1.Pod) {
		pod.Status.Phase, pod.Status.Conditions, pod.Status.ContainerStatuses = corev1.PodPending, nil, nil
	})
	if _, createError := clientset.CoreV1().Pods("project-1").Create(ctx, pendingPod, metav1.CreateOptions{}); createError != nil {
		testingT.Fatalf("作れるはず: %v", createError)
	}
	waitFor(testingT, "Pod の行と created が入る", func() bool { return len(podTypes(database)) == 1 })

	// Pod が Ready になると、ready のイベントが入り、行が更新される。
	readyPod := newTestPodObject(nil)
	_, _ = clientset.CoreV1().Pods("project-1").Update(ctx, readyPod, metav1.UpdateOptions{})
	waitFor(testingT, "ready が入る", func() bool { return len(podTypes(database)) == 2 })

	// クラッシュして再起動を繰り返すと、not_ready、restarted、waiting が入る。
	crashedPod := newTestPodObject(func(pod *corev1.Pod) {
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name: k8s.AppContainerName, RestartCount: 1,
			State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1}},
		}}
	})
	_, _ = clientset.CoreV1().Pods("project-1").Update(ctx, crashedPod, metav1.UpdateOptions{})
	waitFor(testingT, "クラッシュのイベントが入る", func() bool { return len(podTypes(database)) == 5 })
	types := podTypes(database)
	if types[2] != "not_ready" || types[3] != "restarted" || types[4] != "waiting" {
		testingT.Errorf("not_ready、restarted、waiting の順のはず: %v", types)
	}
	var restarted model.DeployPodEvent
	database.Where("deploy_id = ? AND type = ?", "deploy-1", "restarted").Take(&restarted)
	if restarted.Reason != "Error" || restarted.Message == "" {
		testingT.Errorf("理由と終了コードが入るはず: %+v", restarted)
	}

	// Pod が消えると、行が消え、deleted が入る。Deployment が消えると、観測値も消える。
	_ = clientset.CoreV1().Pods("project-1").Delete(ctx, "pod-1", metav1.DeleteOptions{})
	waitFor(testingT, "Pod の行が消えて deleted が入る", func() bool {
		var count int64
		database.Table("deploy_pods").Count(&count)
		types := podTypes(database)
		return count == 0 && types[len(types)-1] == "deleted"
	})
	_ = clientset.AppsV1().Deployments("project-1").Delete(ctx, "deploy-1", metav1.DeleteOptions{})
	waitFor(testingT, "Deployment の観測値が消える", func() bool {
		var count int64
		database.Table("deploy_observations").Count(&count)
		return count == 0
	})
}

// TestObserverIgnoresUnrelatedObjects は、deploy の ID のラベルが無い Pod が、DB に入らないことを確認する。
func TestObserverIgnoresUnrelatedObjects(testingT *testing.T) {
	database, clientset, observer := newObserverTestEnvironment(testingT)
	runObserver(testingT, observer)
	unrelated := newTestPodObject(func(pod *corev1.Pod) {
		pod.Name, pod.UID, pod.Labels = "other", "uid-other", map[string]string{"app": "other"}
	})
	_, _ = clientset.CoreV1().Pods("project-1").Create(context.Background(), unrelated, metav1.CreateOptions{})
	time.Sleep(500 * time.Millisecond)
	var count int64
	database.Table("deploy_pods").Count(&count)
	if count != 0 {
		testingT.Errorf("ラベルの無い Pod は入らないはず: %d", count)
	}
}

// TestObserverTakeover は、引き継いだ watcher が、止まっていた間の変化を、最初の全件の反映で拾うことを確認する。
// DB にだけある古い Pod と観測値は消え、k8s にある Pod の変化は、差分のイベントになる。
func TestObserverTakeover(testingT *testing.T) {
	database, clientset, observer := newObserverTestEnvironment(testingT)
	now := time.Now()
	ctx := context.Background()
	// 前の watcher が残した DB の状態: (1) 起動待ちだった Pod、(2) もう k8s に無い Pod、(3) もう k8s に無い Deployment の観測値。
	database.Create(&model.DeployPod{PodUID: "uid-1", DeployID: "deploy-1", ProjectID: "project-1", PodName: "pod-1", Status: model.PodStatusPending, Phase: "Pending", PodCreatedAt: now, ObservedAt: now})
	database.Create(&model.DeployPod{PodUID: "uid-gone", DeployID: "deploy-1", ProjectID: "project-1", PodName: "pod-gone", Status: model.PodStatusRunning, Phase: "Running", Ready: true, PodCreatedAt: now, ObservedAt: now})
	database.Create(&model.DeployObservation{DeployID: "deploy-1", Generation: 1, ObservedGeneration: 1, ObservedAt: now})
	// k8s の今の状態: pod-1 は、Ready になっている。pod-gone と Deployment は無い。
	if _, createError := clientset.CoreV1().Pods("project-1").Create(ctx, newTestPodObject(nil), metav1.CreateOptions{}); createError != nil {
		testingT.Fatalf("作れるはず: %v", createError)
	}

	// 起動する(最初の全件の反映が終わるまで待つ)。
	runObserver(testingT, observer)

	// Ready になったことが、差分のイベントとして残り、もう無い Pod と観測値は片付く。
	waitFor(testingT, "ready のイベントが入る", func() bool {
		var count int64
		database.Table("deploy_pod_events").Where("pod_uid = ? AND type = ?", "uid-1", "ready").Count(&count)
		return count == 1
	})
	var goneCount, observationCount, deletedEvents int64
	database.Table("deploy_pods").Where("pod_uid = ?", "uid-gone").Count(&goneCount)
	database.Table("deploy_observations").Count(&observationCount)
	database.Table("deploy_pod_events").Where("pod_uid = ? AND type = ?", "uid-gone", "deleted").Count(&deletedEvents)
	if goneCount != 0 || observationCount != 0 || deletedEvents != 1 {
		testingT.Errorf("古い行は片付き、deleted が残るはず: pod=%d 観測値=%d deleted=%d", goneCount, observationCount, deletedEvents)
	}
}

// TestObserverSendsHeartbeat は、最初の全件の反映が終わったあと、心拍が DB に書かれることを確認する。
// 心拍は、ワーカーが、観測値を信用してよいかの判断に使う。
func TestObserverSendsHeartbeat(testingT *testing.T) {
	database, _, observer := newObserverTestEnvironment(testingT)
	// 起動前は、心拍が無い。
	var before int64
	database.Table("watcher_heartbeats").Count(&before)
	if before != 0 {
		testingT.Fatalf("起動前は心拍が無いはず: %d", before)
	}

	runObserver(testingT, observer)

	// 全件の反映が終わると、すぐに、心拍が書かれる。
	waitFor(testingT, "心拍が書かれる", func() bool {
		var count int64
		database.Table("watcher_heartbeats").Count(&count)
		return count == 1
	})
}
