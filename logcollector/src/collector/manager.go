package collector

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"launchs/shared/model"
	"launchs/shared/repository"
)

// ManagerStore は、Manager が使う、DB の操作(FollowerStore に、Pod とストリームの一覧が加わる)。実体は repository.DeployLogRepository。
type ManagerStore interface {
	FollowerStore

	// ListLivePods は、watcher が観測している Pod の全件を返す。
	ListLivePods(requestContext context.Context) ([]model.DeployPod, error)

	// EnsureStream は、Pod のストリームが無ければ作り、あるものを返す。
	EnsureStream(requestContext context.Context, pod model.DeployPod) (model.DeployLogStream, error)

	// ListOpenStreams は、収集中のストリームを返す。
	ListOpenStreams(requestContext context.Context) ([]model.DeployLogStream, error)
}

// ManagerSettings は、Manager の設定。
type ManagerSettings struct {
	// ReconcileInterval は、追う Pod を決め直す間隔。
	ReconcileInterval time.Duration

	// MaxStreams は、同時に追う Pod の数の上限。
	MaxStreams int

	// Follower は、Pod ごとの Follower の設定。
	Follower FollowerSettings

	// StopTimeout は、止める合図の後、Follower が書き終えるのを待つ時間の上限。
	StopTimeout time.Duration
}

// followerEntry は、動いている Follower の記録。
type followerEntry struct {
	cancel context.CancelFunc
	// removed は、Pod が deploy_pods から消えたため、Manager が止めたか。
	removed atomic.Bool
}

// Manager は、追う Pod の集合を決め直し(deploy_pods との比較)、Pod ごとに Follower を動かす。リーダーの間だけ動かす。
type Manager struct {
	store     ManagerStore
	source    LogSource
	settings  ManagerSettings
	appLogger *slog.Logger

	mutex     sync.Mutex
	followers map[string]*followerEntry
	group     sync.WaitGroup
}

// NewManager は、Manager を作る。
//
// 引数:
//   - store: DB の操作。
//   - source: ログの取得元。
//   - settings: 設定。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した Manager。
func NewManager(store ManagerStore, source LogSource, settings ManagerSettings, appLogger *slog.Logger) *Manager {
	// 依存するものを持たせて返す。
	return &Manager{store: store, source: source, settings: settings, appLogger: appLogger, followers: map[string]*followerEntry{}}
}

// Run は、ctx が終わるまで、決め直しを繰り返す。終わるときは、Follower が書き終えるのを(時間を限って)待つ。
//
// 引数:
//   - ctx: 止める合図(リーダーでなくなったときも、キャンセルされる)。
func (manager *Manager) Run(ctx context.Context) {
	// 始めに1回、そのあとは、間隔ごとに決め直す。起動したとき(引き継いだときも同じ)は、保存してある位置から続ける。
	manager.reconcile(ctx)
	ticker := time.NewTicker(manager.settings.ReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			manager.waitForFollowers()
			return
		case <-ticker.C:
			manager.reconcile(ctx)
		}
	}
}

// waitForFollowers は、全ての Follower が終わるのを、時間を限って待つ(書いていない分を、書き終えさせるため)。
func (manager *Manager) waitForFollowers() {
	// 別の goroutine で待ち、時間切れなら、待つのをやめる。
	finished := make(chan struct{})
	go func() {
		manager.group.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(manager.settings.StopTimeout):
		manager.appLogger.Warn("ログの収集の終了を待ちきれなかった")
	}
}

// reconcile は、deploy_pods とストリームを見て、追う Pod の集合を、いまの状態に合わせる。
//
//   - deploy_pods から消えた Pod の Follower は、止めて、ストリームを終了にする。
//   - deploy_pods にある Pod で、Follower が無く、終了していないものは、Follower を始める(同時の数の上限まで)。
//   - 収集中のまま残っているストリームのうち、Pod が消え、Follower も無いもの(前のリーダーが止まっていた間に消えた Pod)は、終了にする。
//
// 引数:
//   - ctx: 止める合図。Follower の context の元になる。
func (manager *Manager) reconcile(ctx context.Context) {
	// いまの Pod の一覧を読む。
	livePods, listError := manager.store.ListLivePods(ctx)
	if listError != nil {
		if ctx.Err() == nil {
			manager.appLogger.Warn("Pod の一覧を読めませんでした。次の間隔でやり直します", "error", listError)
		}
		return
	}
	liveUIDs := make(map[string]bool, len(livePods))
	for _, pod := range livePods {
		liveUIDs[pod.PodUID] = true
	}
	// Pod が消えた Follower を止める(止めた後の終了の処理は、Follower の goroutine が行う)。
	manager.mutex.Lock()
	for podUID, entry := range manager.followers {
		if !liveUIDs[podUID] {
			entry.removed.Store(true)
			entry.cancel()
		}
	}
	manager.mutex.Unlock()
	// Follower が無い Pod に、Follower を始める。
	limitReached := false
	for _, pod := range livePods {
		if ctx.Err() != nil {
			return
		}
		manager.mutex.Lock()
		_, running := manager.followers[pod.PodUID]
		count := len(manager.followers)
		manager.mutex.Unlock()
		if running {
			continue
		}
		stream, ensureError := manager.store.EnsureStream(ctx, pod)
		if errors.Is(ensureError, repository.ErrLogStreamGone) {
			continue
		}
		if ensureError != nil {
			manager.appLogger.Warn("ログのストリームを用意できませんでした", "pod_uid", pod.PodUID, "error", ensureError)
			continue
		}
		// 終了しているストリーム(コンテナが終わった Pod)は、もう追わない。
		if stream.FinishedAt != nil {
			continue
		}
		if count >= manager.settings.MaxStreams {
			limitReached = true
			continue
		}
		manager.startFollower(ctx, stream)
	}
	if limitReached {
		manager.appLogger.Warn("同時に追える Pod の数の上限に達した。残りの Pod は、空きができるまで追わない", "max_streams", manager.settings.MaxStreams)
	}
	// Pod が消えて、Follower も無い、収集中のストリームを、終了にする。
	openStreams, openError := manager.store.ListOpenStreams(ctx)
	if openError != nil {
		return
	}
	for _, stream := range openStreams {
		if liveUIDs[stream.PodUID] {
			continue
		}
		manager.mutex.Lock()
		_, running := manager.followers[stream.PodUID]
		manager.mutex.Unlock()
		if !running {
			_ = manager.store.FinishStream(ctx, stream.PodUID)
		}
	}
}

// startFollower は、Pod の Follower を、別の goroutine で始める。
//
// 引数:
//   - ctx: 止める合図の元(Manager が止まると、Follower も止まる)。
//   - stream: 追う Pod のストリーム。
func (manager *Manager) startFollower(ctx context.Context, stream model.DeployLogStream) {
	// この Follower だけを止められる context を作って、記録する。
	followerContext, cancel := context.WithCancel(ctx)
	entry := &followerEntry{cancel: cancel}
	manager.mutex.Lock()
	manager.followers[stream.PodUID] = entry
	manager.mutex.Unlock()
	manager.group.Add(1)
	go func() {
		defer manager.group.Done()
		defer cancel()
		NewFollower(manager.store, manager.source, manager.settings.Follower, manager.appLogger, stream).Run(followerContext)
		// Pod が消えたために止めたなら、ストリームを終了にする。
		if entry.removed.Load() {
			finishContext, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer finishCancel()
			_ = manager.store.FinishStream(finishContext, stream.PodUID)
		}
		manager.mutex.Lock()
		delete(manager.followers, stream.PodUID)
		manager.mutex.Unlock()
	}()
}
