package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"launchs/shared/model"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// resyncPeriod は、informer が、念のため全件を再確認する間隔。通常は、イベントの通知で足りる。
const resyncPeriod = 10 * time.Minute

// EventStore は、Collector が必要とする、Event の保存先の操作。
//
// 実体は shared/repository の ProjectEventRepository。
type EventStore interface {
	// UpsertEvent は、Event を保存する。同じ UID のものがあれば、回数と時刻を進める。
	UpsertEvent(requestContext context.Context, event model.ProjectEvent) error
}

// Collector は、プロジェクトの namespace の k8s の Event を観測し、DB に保存する。
//
// 起動したとき(別の watcher から引き継いだときも同じ)は、いまの k8s の全件を、保存する。
// k8s の Event は、約 1 時間で消えるが、DB からは消さない(保存期間と件数の上限で、別に掃除する)。
type Collector struct {
	clientset kubernetes.Interface
	store     EventStore
	appLogger *slog.Logger
	// synced は、最初の全件の読み込みが終わったときに閉じる。
	synced chan struct{}
}

// New は、Collector を作る。
//
// 引数:
//   - clientset: k8s のクライアント(読み取りだけを行う)。
//   - store: Event の保存先。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した Collector。
func New(clientset kubernetes.Interface, store EventStore, appLogger *slog.Logger) *Collector {
	// 依存するものを持たせて返す。
	return &Collector{clientset: clientset, store: store, appLogger: appLogger, synced: make(chan struct{})}
}

// Synced は、最初の全件の読み込みが終わったときに閉じる channel を返す。
//
// 戻り値:
//   - 読み込みが終わると閉じる channel。
func (collector *Collector) Synced() <-chan struct{} {
	// 閉じる側は Run なので、読み取り専用で返す。
	return collector.synced
}

// Run は、Event を観測し続ける。ctx がキャンセルされるまで戻らない。
//
// 処理の流れ:
//  1. informer を作り、変更の通知が、作業キューに入るようにする
//  2. 起動して、全件を読み込む(全件は、「追加」の通知として、キューに入る)
//  3. キューの Event を、順に、DB へ保存する(以降の変化も、同じ道を通る)
//
// 引数:
//   - ctx: 止めるための context。
//
// 戻り値:
//   - 準備に失敗した、または、読み込みの間に止められたときのエラー。止める合図で終わったときは nil。
func (collector *Collector) Run(ctx context.Context) error {
	// ===== 1. 準備 =====
	factory := informers.NewSharedInformerFactory(collector.clientset, resyncPeriod)
	informer := factory.Core().V1().Events().Informer()
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	defer queue.ShutDown()
	// 追加と更新の通知を、キューに入れる(削除は、DB に残すので、扱わない)。
	if _, handlerError := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(object any) { enqueue(queue, object) },
		UpdateFunc: func(_ any, object any) { enqueue(queue, object) },
	}); handlerError != nil {
		return fmt.Errorf("イベントの受け取りを登録できませんでした: %w", handlerError)
	}

	// ===== 2. 全件の読み込み =====
	factory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		return errors.New("k8s の Event の全件の読み込みを待っている間に止められました")
	}
	collector.appLogger.Info("k8s の Event の全件を DB に保存するのを始めた")
	close(collector.synced)

	// ===== 3. 保存 =====
	go collector.work(ctx, queue, informer.GetIndexer())
	// 止める合図を待つ。
	<-ctx.Done()
	return nil
}

// enqueue は、k8s のオブジェクトの「namespace/名前」を、作業キューに入れる。
//
// 引数:
//   - queue: 作業キュー。
//   - object: informer から渡されたオブジェクト。
func enqueue(queue workqueue.TypedRateLimitingInterface[string], object any) {
	// キーが作れないものは、無視する。
	key, keyError := cache.MetaNamespaceKeyFunc(object)
	if keyError != nil {
		return
	}
	queue.Add(key)
}

// work は、作業キューから Event を取り出して、保存し続ける。ctx がキャンセルされるか、キューが閉じると戻る。
//
// 引数:
//   - ctx: 止めるための context。
//   - queue: 作業キュー。
//   - indexer: informer の手元の写し(キーから、いまの Event を引く)。
func (collector *Collector) work(ctx context.Context, queue workqueue.TypedRateLimitingInterface[string], indexer cache.Indexer) {
	// 止める合図で、キューを閉じて、待っている取り出しを終わらせる。
	go func() { <-ctx.Done(); queue.ShutDown() }()
	for {
		key, shutdown := queue.Get()
		if shutdown {
			return
		}
		// 保存に失敗したら、間隔を空けて、やり直す。成功したら、やり直しの数え直しを止める。
		if saveError := collector.save(ctx, indexer, key); saveError != nil {
			if ctx.Err() == nil {
				collector.appLogger.Warn("k8s の Event を保存できませんでした。やり直します", "key", key, "error", saveError)
				queue.AddRateLimited(key)
			}
		} else {
			queue.Forget(key)
		}
		queue.Done(key)
	}
}

// save は、キーの Event を、手元の写しから引いて、DB に保存する。
//
// 引数:
//   - ctx: キャンセルや期限を伝える context。
//   - indexer: informer の手元の写し。
//   - key: 「namespace/名前」。
//
// 戻り値:
//   - 保存に失敗したときのエラー。写しに無い(消えた)もの、保存の対象でないものは、何もせず nil。
func (collector *Collector) save(ctx context.Context, indexer cache.Indexer, key string) error {
	// 写しから、いまの Event を引く。無ければ(k8s から消えた)、DB には残すので、何もしない。
	object, exists, getError := indexer.GetByKey(key)
	if getError != nil {
		return getError
	}
	if !exists {
		return nil
	}
	event, isEvent := object.(*corev1.Event)
	if !isEvent {
		return nil
	}
	// 保存する形に変換できないもの(プロジェクト以外の namespace など)は、保存しない。
	projectEvent, ok := ToProjectEvent(event)
	if !ok {
		return nil
	}
	return collector.store.UpsertEvent(ctx, projectEvent)
}
