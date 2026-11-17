package events

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"launchs/shared/logger"
	"launchs/shared/model"
)

// テストに使う、プロジェクトの namespace と、deploy の ID。
const (
	testNamespace = "project-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	testDeployID  = "deploy-11111111-2222-3333-4444-555555555555"
)

// TestDeployIDFromObjectName は、対象の名前から、deploy の ID が取り出せること(Deployment、ReplicaSet、Pod)と、
// deploy の名前でないもの(PVC など)は空になることを確認する。
func TestDeployIDFromObjectName(testingT *testing.T) {
	// 名前と、期待する deploy の ID の表。
	testCases := []struct {
		name string
		in   string
		want string
	}{
		{"Deployment の名前は、deploy の ID そのもの", testDeployID, testDeployID},
		{"ReplicaSet は、ハッシュが付く", testDeployID + "-5bdf7f4589", testDeployID},
		{"Pod は、ハッシュと乱数が付く", testDeployID + "-5bdf7f4589-bnhdv", testDeployID},
		{"PVC(volume の名前)は、deploy ではない", "volume-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", ""},
		{"deploy- で始まるが、UUID の形でない名前は、空", "deploy-nothing", ""},
		{"UUID が、途中で切れた名前は、空", "deploy-11111111-2222-3333-4444-5555", ""},
		{"空の名前は、空", "", ""},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			if got := deployIDFromObjectName(testCase.in); got != testCase.want {
				subTest.Errorf("%q → %q のはず(実際 %q)", testCase.in, testCase.want, got)
			}
		})
	}
}

// TestIsProjectNamespace は、プロジェクトの namespace の形だけが、保存の対象になることを確認する。
func TestIsProjectNamespace(testingT *testing.T) {
	// namespace と、期待する結果の表。
	for namespace, want := range map[string]bool{
		testNamespace:           true,
		"kube-system":           false,
		"traefik":               false,
		"project-":              false,
		"project-aaaa":          false,
		testNamespace + "-x":    false,
		"x" + testNamespace:     false,
		"PROJECT-AAAAAAAA-BBBB": false,
	} {
		if got := IsProjectNamespace(namespace); got != want {
			testingT.Errorf("%q は %v のはず(実際 %v)", namespace, want, got)
		}
	}
}

// TestToProjectEvent は、k8s の Event が、保存する形に変換されること(時刻の欠け、回数 0、対象の種類、対象外の namespace)を確認する。
func TestToProjectEvent(testingT *testing.T) {
	// 基準の時刻。
	base := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	// 変換元の Event を作る関数(必要な項目だけ、呼び出し側で上書きする)。
	build := func(modify func(event *corev1.Event)) *corev1.Event {
		event := &corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Namespace: testNamespace, Name: "e1", UID: types.UID("uid-1"), CreationTimestamp: metav1.NewTime(base.Add(-time.Hour))},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: testDeployID + "-5bdf7f4589-bnhdv"},
			Type:           "Warning", Reason: "BackOff", Message: "Back-off pulling image",
			Count: 3, FirstTimestamp: metav1.NewTime(base), LastTimestamp: metav1.NewTime(base.Add(time.Minute)),
		}
		if modify != nil {
			modify(event)
		}
		return event
	}

	testingT.Run("全ての項目が、そのまま変換される(Pod の Event から、deploy の ID も分かる)", func(subTest *testing.T) {
		converted, ok := ToProjectEvent(build(nil))
		want := model.ProjectEvent{ProjectID: testNamespace, EventUID: "uid-1", Type: "Warning", Reason: "BackOff", Message: "Back-off pulling image", ObjectKind: "Pod", ObjectName: testDeployID + "-5bdf7f4589-bnhdv", DeployID: testDeployID, Count: 3, FirstSeenAt: base, LastSeenAt: base.Add(time.Minute)}
		if !ok || converted.ProjectID != want.ProjectID || converted.EventUID != want.EventUID || converted.Type != want.Type || converted.Reason != want.Reason || converted.Message != want.Message ||
			converted.ObjectKind != want.ObjectKind || converted.ObjectName != want.ObjectName || converted.DeployID != want.DeployID || converted.Count != want.Count ||
			!converted.FirstSeenAt.Equal(want.FirstSeenAt) || !converted.LastSeenAt.Equal(want.LastSeenAt) {
			subTest.Errorf("変換が違う: %v %+v", ok, converted)
		}
	})
	testingT.Run("回数が 0 なら、1 回として扱う", func(subTest *testing.T) {
		converted, ok := ToProjectEvent(build(func(event *corev1.Event) { event.Count = 0 }))
		if !ok || converted.Count != 1 {
			subTest.Errorf("1 のはず: %v %d", ok, converted.Count)
		}
	})
	testingT.Run("firstTimestamp と lastTimestamp が無ければ、eventTime を使う", func(subTest *testing.T) {
		converted, ok := ToProjectEvent(build(func(event *corev1.Event) {
			event.FirstTimestamp, event.LastTimestamp = metav1.Time{}, metav1.Time{}
			event.EventTime = metav1.NewMicroTime(base.Add(5 * time.Minute))
		}))
		if !ok || !converted.FirstSeenAt.Equal(base.Add(5*time.Minute)) || !converted.LastSeenAt.Equal(base.Add(5*time.Minute)) {
			subTest.Errorf("eventTime のはず: %v %v %v", ok, converted.FirstSeenAt, converted.LastSeenAt)
		}
	})
	testingT.Run("時刻が全て無ければ、作成時刻を使う。lastTimestamp だけが無ければ、最初の時刻を使う", func(subTest *testing.T) {
		onlyCreated, okCreated := ToProjectEvent(build(func(event *corev1.Event) { event.FirstTimestamp, event.LastTimestamp = metav1.Time{}, metav1.Time{} }))
		if !okCreated || !onlyCreated.FirstSeenAt.Equal(base.Add(-time.Hour)) || !onlyCreated.LastSeenAt.Equal(base.Add(-time.Hour)) {
			subTest.Errorf("作成時刻のはず: %v %v %v", okCreated, onlyCreated.FirstSeenAt, onlyCreated.LastSeenAt)
		}
		noLast, okNoLast := ToProjectEvent(build(func(event *corev1.Event) { event.LastTimestamp = metav1.Time{} }))
		if !okNoLast || !noLast.LastSeenAt.Equal(base) {
			subTest.Errorf("最後は、最初と同じのはず: %v %v", okNoLast, noLast.LastSeenAt)
		}
	})
	testingT.Run("最後が最初より前なら、最後を最初に合わせる", func(subTest *testing.T) {
		converted, ok := ToProjectEvent(build(func(event *corev1.Event) { event.LastTimestamp = metav1.NewTime(base.Add(-time.Minute)) }))
		if !ok || !converted.LastSeenAt.Equal(converted.FirstSeenAt) {
			subTest.Errorf("同じのはず: %v %v %v", ok, converted.FirstSeenAt, converted.LastSeenAt)
		}
	})
	testingT.Run("対象の種類ごとに、deploy の ID が決まる(Deployment、ReplicaSet は、ID を持つ。PVC は、持たない)", func(subTest *testing.T) {
		for _, testCase := range []struct{ kind, name, want string }{
			{"Deployment", testDeployID, testDeployID},
			{"ReplicaSet", testDeployID + "-5bdf7f4589", testDeployID},
			{"PersistentVolumeClaim", "volume-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", ""},
		} {
			converted, ok := ToProjectEvent(build(func(event *corev1.Event) {
				event.InvolvedObject = corev1.ObjectReference{Kind: testCase.kind, Name: testCase.name}
			}))
			if !ok || converted.DeployID != testCase.want || converted.ObjectKind != testCase.kind {
				subTest.Errorf("%s: %q のはず(実際 %q)", testCase.kind, testCase.want, converted.DeployID)
			}
		}
	})
	testingT.Run("プロジェクト以外の namespace、UID の無いもの、nil は、保存の対象外", func(subTest *testing.T) {
		if _, ok := ToProjectEvent(build(func(event *corev1.Event) { event.Namespace = "kube-system" })); ok {
			subTest.Error("kube-system は、対象外のはず")
		}
		if _, ok := ToProjectEvent(build(func(event *corev1.Event) { event.UID = "" })); ok {
			subTest.Error("UID が無ければ、対象外のはず")
		}
		if _, ok := ToProjectEvent(nil); ok {
			subTest.Error("nil は、対象外のはず")
		}
	})
}

// fakeEventStore は、テスト用の EventStore。保存された Event を、UID ごとに、メモリに持つ。
type fakeEventStore struct {
	mutex  sync.Mutex
	events map[string]model.ProjectEvent
}

// UpsertEvent は、同じ UID を、新しい値で、上書きして覚える。
func (fakeStore *fakeEventStore) UpsertEvent(_ context.Context, event model.ProjectEvent) error {
	fakeStore.mutex.Lock()
	defer fakeStore.mutex.Unlock()
	fakeStore.events[event.EventUID] = event
	return nil
}

// get は、UID の Event を返す。
func (fakeStore *fakeEventStore) get(uid string) (model.ProjectEvent, bool) {
	fakeStore.mutex.Lock()
	defer fakeStore.mutex.Unlock()
	event, ok := fakeStore.events[uid]
	return event, ok
}

// waitFor は、条件が成り立つまで、短い間隔で待つ。
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

// TestCollectorRun は、起動時に、既にある Event が保存され、あとから増えた・更新された Event も保存され、
// プロジェクト以外の namespace の Event は保存されず、k8s から消えても DB の記録は残る(消されない)ことを確認する。
func TestCollectorRun(testingT *testing.T) {
	// 起動の前から、k8s にある Event(プロジェクトのものと、kube-system のもの)を用意する。
	base := time.Now().Truncate(time.Second)
	newEvent := func(namespace string, uid string, count int32) *corev1.Event {
		return &corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Namespace: namespace, Name: "event-" + uid, UID: types.UID(uid)},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: testDeployID + "-5bdf7f4589-bnhdv"},
			Type:           "Normal", Reason: "Pulling", Message: "Pulling image", Count: count,
			FirstTimestamp: metav1.NewTime(base), LastTimestamp: metav1.NewTime(base),
		}
	}
	clientset := fake.NewSimpleClientset(newEvent(testNamespace, "uid-before", 1), newEvent("kube-system", "uid-system", 1))
	fakeStore := &fakeEventStore{events: map[string]model.ProjectEvent{}}
	collector := New(clientset, fakeStore, logger.New(&bytes.Buffer{}, "test", slog.LevelInfo))
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- collector.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			testingT.Error("止める合図で戻るはず")
		}
	}()
	// 全件の読み込みが終わるのを待つ。
	select {
	case <-collector.Synced():
	case <-time.After(10 * time.Second):
		testingT.Fatal("全件の読み込みが終わるはず")
	}

	testingT.Run("起動の前からある Event が、保存される。deploy の ID も付く", func(subTest *testing.T) {
		waitFor(subTest, "uid-before が保存される", func() bool { _, ok := fakeStore.get("uid-before"); return ok })
		saved, _ := fakeStore.get("uid-before")
		if saved.ProjectID != testNamespace || saved.DeployID != testDeployID {
			subTest.Errorf("保存の内容が違う: %+v", saved)
		}
	})
	testingT.Run("プロジェクト以外の namespace の Event は、保存されない", func(subTest *testing.T) {
		// 保存されるものが出揃うのを待ってから、kube-system のものが無いことを確かめる。
		waitFor(subTest, "uid-before が保存される", func() bool { _, ok := fakeStore.get("uid-before"); return ok })
		if _, ok := fakeStore.get("uid-system"); ok {
			subTest.Error("kube-system の Event は、保存されないはず")
		}
	})
	testingT.Run("あとから増えた Event と、更新された Event(回数が増える)が、保存される", func(subTest *testing.T) {
		if _, createError := clientset.CoreV1().Events(testNamespace).Create(ctx, newEvent(testNamespace, "uid-after", 1), metav1.CreateOptions{}); createError != nil {
			subTest.Fatal(createError)
		}
		waitFor(subTest, "uid-after が保存される", func() bool { _, ok := fakeStore.get("uid-after"); return ok })
		updated := newEvent(testNamespace, "uid-after", 7)
		if _, updateError := clientset.CoreV1().Events(testNamespace).Update(ctx, updated, metav1.UpdateOptions{}); updateError != nil {
			subTest.Fatal(updateError)
		}
		waitFor(subTest, "回数が 7 になる", func() bool { saved, _ := fakeStore.get("uid-after"); return saved.Count == 7 })
	})
	testingT.Run("k8s から Event が消えても、DB の記録は消さない", func(subTest *testing.T) {
		if deleteError := clientset.CoreV1().Events(testNamespace).Delete(ctx, "event-uid-before", metav1.DeleteOptions{}); deleteError != nil {
			subTest.Fatal(deleteError)
		}
		// 削除の通知が処理される時間を、少し置く。
		time.Sleep(300 * time.Millisecond)
		if _, ok := fakeStore.get("uid-before"); !ok {
			subTest.Error("消えた Event も、DB には残るはず")
		}
	})
}
