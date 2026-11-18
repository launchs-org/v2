package observer

import (
	"context"
	"launchs/shared/model"
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// sendHeartbeats は、生きていることを示す心拍を、すぐに1回、そのあとは一定の間隔で、DB に書く。ctx がキャンセルされるまで続ける。
//
// 書けなくても止まらない(次の間隔でやり直す)。書けない状態が続くと、ワーカーは、心拍が古いとみなして、
// 観測値を信用せず、k8s に直接問い合わせる(安全な側に倒れる)。
//
// 引数:
//   - ctx: 止めるための context。
func (observer *Observer) sendHeartbeats(ctx context.Context) {
	// 一定の間隔で書く。始めに1回、すぐに書く。
	ticker := time.NewTicker(model.WatcherHeartbeatInterval)
	defer ticker.Stop()
	for {
		if touchError := observer.store.TouchHeartbeat(ctx); touchError != nil && ctx.Err() == nil {
			observer.appLogger.Warn("心拍を書けませんでした", "error", touchError)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// startWorkers は、Pod と Deployment のキューを、それぞれ別に、順番に処理し始める。
//
// 引数:
//   - ctx: 止めるための context。
//   - set: informer とキューのまとまり。
func (observer *Observer) startWorkers(ctx context.Context, set *watchSet) {
	// Pod のキュー: いまの Pod の状態を調べて、DB に反映する。
	go observer.process(ctx, set.podQueue, func(key string) error {
		return observer.syncPod(ctx, set.podInformer.GetIndexer(), key)
	})
	// Deployment のキュー: いまの Deployment の状態を調べて、DB に反映する。
	go observer.process(ctx, set.deploymentQueue, func(key string) error {
		return observer.syncDeployment(ctx, set.deploymentInformer.GetIndexer(), key)
	})
}

// enqueue は、変更のあったオブジェクトの名前を、キューに入れる。削除のイベントも、同じ形で扱う。
//
// 引数:
//   - queue: 入れるキュー。
//   - object: 変更のあったオブジェクト(削除のときは、削除の印が付いている場合がある)。
func enqueue(queue workqueue.TypedRateLimitingInterface[string], object any) {
	// 「namespace/名前」の形のキーを作って入れる。
	key, keyError := cache.DeletionHandlingMetaNamespaceKeyFunc(object)
	if keyError != nil {
		return
	}
	queue.Add(key)
}

// process は、キューからキーを取り出して、処理し続ける。失敗したものは、間隔を空けてやり直す。
//
// 引数:
//   - ctx: 止めるための context。
//   - queue: 処理するキュー。
//   - handle: 1つのキーの処理。
func (observer *Observer) process(ctx context.Context, queue workqueue.TypedRateLimitingInterface[string], handle func(key string) error) {
	// キューが閉じられるまで、取り出して処理する。
	for {
		key, shutdown := queue.Get()
		if shutdown {
			return
		}
		// 処理して、失敗したら、間隔を空けて入れ直す。成功したら、やり直しの記録を消す。
		if handleError := handle(key); handleError != nil && ctx.Err() == nil {
			observer.appLogger.Warn("観測値を反映できなかったので、やり直す", "key", key, "error", handleError)
			queue.AddRateLimited(key)
		} else {
			queue.Forget(key)
		}
		queue.Done(key)
	}
}
