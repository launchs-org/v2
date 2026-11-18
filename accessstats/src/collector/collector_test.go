package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"launchs/shared/accessstats"
)

// fakeSource は、試験用の Source。Pod ごとに決めたログを返す。
type fakeSource struct {
	mutex sync.Mutex
	pods  []string
	logs  map[string]string
	opens map[string]int
}

// ListPods は、決めた Pod の一覧を返す。
func (source *fakeSource) ListPods(context.Context) ([]string, error) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	return append([]string{}, source.pods...), nil
}

// OpenLog は、決めたログを返し、開いた回数を数える。
func (source *fakeSource) OpenLog(_ context.Context, podName string, _ int64) (io.ReadCloser, error) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	source.opens[podName]++
	return io.NopCloser(strings.NewReader(source.logs[podName])), nil
}

// fakeStore は、保存された集計を覚える。
type fakeStore struct {
	mutex   sync.Mutex
	records []accessstats.Record
	logged  int
}

// UpsertMinute は、集計を覚える。
func (store *fakeStore) UpsertMinute(_ context.Context, record accessstats.Record) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.records = append(store.records, record)
	return nil
}

// InsertLogs は、記録の数を覚える。
func (store *fakeStore) InsertLogs(_ context.Context, record accessstats.Record) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.logged += len(record.Requests)
	return nil
}

// count は、保存された数を返す。
func (store *fakeStore) count() int {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return len(store.records)
}

// TestManagerIngestsLines は、集計の行だけが、Pod の名前つきで保存され、他の行は無視されることを確認する。
func TestManagerIngestsLines(testingT *testing.T) {
	line := `LAUNCHS_STATS {"count":2,"ingressId":"ingress-a","intervalSec":60,"latency":[2,0,0,0,0,0,0,0,0,0,0],"start":"2026-11-18T09:50:00Z","status":{"2xx":2},"methods":{"GET":2},"requests":[{"t":1700000000000,"m":"GET","u":"/?a=1","s":200,"d":1,"ip":"1.1.1.1","b":1},{"t":1700000000001,"m":"GET","u":"/","s":200,"d":1,"ip":"1.1.1.1","b":1}],"paths":[{"key":"/","count":2}],"clients":[{"key":"1.1.1.1","count":2}],"uniqueClients":1}`
	source := &fakeSource{pods: []string{"traefik-0"}, logs: map[string]string{"traefik-0": "普通のログ\n" + line + "\n{壊れた}\nLAUNCHS_STATS {oops\n"}, opens: map[string]int{}}
	store := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	manager := NewManager(source, store, Settings{ReconcileInterval: 20 * time.Millisecond, KeepClients: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan struct{})
	go func() { manager.Run(ctx); close(done) }()
	// 保存されるのを待つ。
	deadline := time.After(3 * time.Second)
	for store.count() == 0 {
		select {
		case <-deadline:
			testingT.Fatal("保存されない")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.records[0].Replica != "traefik-0" || store.records[0].IngressID != "ingress-a" || store.records[0].Clients[0].Key != "1.1.1.1" {
		testingT.Errorf("Pod の名前つきで保存されるはず: %+v", store.records[0])
	}
	if store.logged < 2 {
		testingT.Errorf("1 リクエストごとの記録も保存されるはず: %d", store.logged)
	}
	for _, record := range store.records {
		if record.IngressID != "ingress-a" {
			testingT.Errorf("集計の行だけが保存されるはず: %+v", record)
		}
	}
}

// TestManagerFollowsEachPodOnce は、同じ Pod を二重に追わないことを確認する。
func TestManagerFollowsEachPodOnce(testingT *testing.T) {
	source := &fakeSource{pods: []string{"traefik-0", "traefik-1"}, logs: map[string]string{}, opens: map[string]int{}}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	NewManager(source, &fakeStore{}, Settings{ReconcileInterval: 10 * time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil))).Run(ctx)
	source.mutex.Lock()
	defer source.mutex.Unlock()
	// 再接続の待ち(3 秒)より短いので、開くのは 1 回ずつのはず。
	if source.opens["traefik-0"] != 1 || source.opens["traefik-1"] != 1 {
		testingT.Errorf("1 回ずつ開くはず: %v", source.opens)
	}
}

// errPodSource は、Pod が無いと答える Source。
type errPodSource struct{ fakeSource }

// OpenLog は、Pod が無いと答える。
func (*errPodSource) OpenLog(context.Context, string, int64) (io.ReadCloser, error) {
	return nil, errors.Join(ErrPodGone)
}

// TestFollowStopsWhenPodGone は、Pod が無くなったら、追うのをやめることを確認する。
func TestFollowStopsWhenPodGone(testingT *testing.T) {
	manager := NewManager(&errPodSource{}, &fakeStore{}, Settings{ReconcileInterval: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan struct{})
	go func() { manager.follow(context.Background(), "gone"); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		testingT.Fatal("Pod が無ければ戻るはず")
	}
}
