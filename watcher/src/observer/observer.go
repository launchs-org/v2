package observer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"launchs/shared/k8s"
	"launchs/shared/model"
)

// resyncPeriod は、informer が、念のため全件を再確認する間隔。通常は、イベントで足りる。
const resyncPeriod = 10 * time.Minute

// ObservationStore は、Observer が必要とする、観測値の保存先の操作。
//
// 実体は shared/repository の DeployObservationRepository。
type ObservationStore interface {
	// UpsertDeployment は、Deployment の観測値を保存する。deploy の行が無ければ、保存せず false を返す。
	UpsertDeployment(requestContext context.Context, observation model.DeployObservation) (bool, error)

	// DeleteDeployment は、Deployment の観測値を削除する。
	DeleteDeployment(requestContext context.Context, deployID string) error

	// ListObservedDeployIDs は、観測値がある deploy の ID を全て返す。
	ListObservedDeployIDs(requestContext context.Context) ([]string, error)

	// ApplyPod は、Pod の状態を保存し、前の状態との差から、イベントを追記する。
	ApplyPod(requestContext context.Context, next model.DeployPod) ([]model.DeployPodEvent, error)

	// DeletePod は、消えた Pod の行を削除し、deleted のイベントを追記する。
	DeletePod(requestContext context.Context, projectID string, podName string, observedAt time.Time) (bool, error)

	// ListPods は、保存してある Pod の状態を全て返す。
	ListPods(requestContext context.Context) ([]model.DeployPod, error)
}

// Observer は、k8s の Pod と Deployment のイベントを受け取り、実際の状態を DB に反映する。
//
// 起動したとき(別の watcher から引き継いだときも同じ)は、先に、いまの k8s の全件を読み込み、DB との差分を反映する。
// そのあと、イベントの受け取り(watch)に入る。止まっていた間の変化は、全件の読み込みと DB との差分で拾う。
type Observer struct {
	clientset kubernetes.Interface
	store     ObservationStore
	appLogger *slog.Logger
	// now は、観測した時刻を返す。テストで差し替える。
	now func() time.Time
	// synced は、最初の全件の反映が終わったときに閉じる。
	synced chan struct{}
}

// New は、Observer を作る。
//
// 引数:
//   - clientset: k8s のクライアント(読み取りだけを行う)。
//   - store: 観測値の保存先。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した Observer。
func New(clientset kubernetes.Interface, store ObservationStore, appLogger *slog.Logger) *Observer {
	// 依存するものを持たせて返す。
	return &Observer{clientset: clientset, store: store, appLogger: appLogger, now: time.Now, synced: make(chan struct{})}
}

// Synced は、最初の全件の反映が終わったときに閉じる channel を返す。
//
// 戻り値:
//   - 反映が終わると閉じる channel。
func (observer *Observer) Synced() <-chan struct{} {
	// 閉じる側は Run なので、読み取り専用で返す。
	return observer.synced
}

// Run は、k8s の Pod と Deployment を観測し続ける。ctx がキャンセルされるまで戻らない。
//
// 処理の流れ:
//  1. deploy の ID のラベルがある Pod と Deployment を、全ての namespace で観測する informer を始める
//  2. 全件を読み込み終えるのを待つ
//  3. 全件を DB と突き合わせる(DB にあって k8s に無いものを削除する)。k8s にあるものは、2 の読み込みで届くイベントが反映する
//  4. 以降のイベントを、順に DB へ反映する(失敗したものは、間隔を空けてやり直す)
//
// 引数:
//   - ctx: 止めるための context。
//
// 戻り値:
//   - 全件の読み込みを待っている間に止められたときのエラー。止める合図で終わったときは nil。
func (observer *Observer) Run(ctx context.Context) error {
	// deploy の ID のラベルがあるものだけを、全 namespace で観測する。
	factory := informers.NewSharedInformerFactoryWithOptions(observer.clientset, resyncPeriod,
		informers.WithTweakListOptions(func(options *metav1ListOptions) { options.LabelSelector = k8s.DeployIDLabel }))
	podInformer := factory.Core().V1().Pods().Informer()
	deploymentInformer := factory.Apps().V1().Deployments().Informer()

	// 変更のあったものの名前を、キューに入れる(処理は、キューから取り出して行う。同じものは1つにまとまる)。
	podQueue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	deploymentQueue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	defer podQueue.ShutDown()
	defer deploymentQueue.ShutDown()
	for informer, queue := range map[cache.SharedIndexInformer]workqueue.TypedRateLimitingInterface[string]{podInformer: podQueue, deploymentInformer: deploymentQueue} {
		queue := queue
		if _, handlerError := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    func(object any) { enqueue(queue, object) },
			UpdateFunc: func(_ any, object any) { enqueue(queue, object) },
			DeleteFunc: func(object any) { enqueue(queue, object) },
		}); handlerError != nil {
			return fmt.Errorf("イベントの受け取りを登録できませんでした: %w", handlerError)
		}
	}

	// informer を始め、最初の全件の読み込みを待つ。
	factory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), podInformer.HasSynced, deploymentInformer.HasSynced) {
		return errors.New("k8s の全件の読み込みを待っている間に止められました")
	}

	// DB にあって k8s に無いものを削除する。
	if reconcileError := observer.removeStale(ctx, podInformer.GetIndexer(), deploymentInformer.GetIndexer()); reconcileError != nil {
		return fmt.Errorf("DB との突き合わせに失敗しました: %w", reconcileError)
	}
	observer.appLogger.Info("k8s の全件を DB に反映するのを始めた")
	close(observer.synced)

	// イベントを処理する(Pod と Deployment は、別々に、順番に処理する)。
	go observer.process(ctx, podQueue, func(key string) error { return observer.syncPod(ctx, podInformer.GetIndexer(), key) })
	go observer.process(ctx, deploymentQueue, func(key string) error { return observer.syncDeployment(ctx, deploymentInformer.GetIndexer(), key) })
	// 止める合図を待つ。
	<-ctx.Done()
	return nil
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

// syncPod は、1つの Pod の、いまの k8s の状態を DB に反映する。k8s に無ければ、DB の行を消す。
//
// 引数:
//   - ctx: キャンセルや期限を伝える context。
//   - indexer: informer が持つ Pod の全件。
//   - key: 「namespace/名前」の形のキー。
//
// 戻り値:
//   - DB への反映に失敗したときのエラー。
func (observer *Observer) syncPod(ctx context.Context, indexer cache.Indexer, key string) error {
	// 最新の状態を、informer の持つ全件から探す。
	object, exists, getError := indexer.GetByKey(key)
	if getError != nil {
		return getError
	}
	namespace, name, splitError := cache.SplitMetaNamespaceKey(key)
	if splitError != nil {
		return splitError
	}
	// k8s にもう無いときは、DB の行を消して、deleted のイベントを残す。
	if !exists {
		deleted, deleteError := observer.store.DeletePod(ctx, namespace, name, observer.now())
		if deleteError == nil && deleted {
			observer.appLogger.Info("Pod が消えた", "project_id", namespace, "pod", name)
		}
		return deleteError
	}
	// 状態を作って、DB に反映する。deploy のものでなければ、何もしない。
	pod, isPod := object.(podObject)
	if !isPod {
		return nil
	}
	snapshot, relevant := BuildPodSnapshot(pod, observer.now())
	if !relevant {
		return nil
	}
	events, applyError := observer.store.ApplyPod(ctx, snapshot)
	if applyError != nil {
		return applyError
	}
	// 状態の変化は、運用で追えるよう、ログにも出す。
	for _, event := range events {
		observer.appLogger.Info("Pod の状態が変わった", "deploy_id", event.DeployID, "pod", event.PodName,
			"event", event.Type, "reason", event.Reason, "message", event.Message)
	}
	return nil
}

// syncDeployment は、1つの Deployment の、いまの k8s の状態を DB に反映する。k8s に無ければ、観測値を消す。
//
// 引数:
//   - ctx: キャンセルや期限を伝える context。
//   - indexer: informer が持つ Deployment の全件。
//   - key: 「namespace/名前」の形のキー(名前は deploy の ID)。
//
// 戻り値:
//   - DB への反映に失敗したときのエラー。
func (observer *Observer) syncDeployment(ctx context.Context, indexer cache.Indexer, key string) error {
	// 最新の状態を、informer の持つ全件から探す。
	object, exists, getError := indexer.GetByKey(key)
	if getError != nil {
		return getError
	}
	_, name, splitError := cache.SplitMetaNamespaceKey(key)
	if splitError != nil {
		return splitError
	}
	// k8s にもう無いときは、観測値を消す(Deployment の名前が deploy の ID)。
	if !exists {
		return observer.store.DeleteDeployment(ctx, name)
	}
	// 観測値を作って、DB に反映する。
	deployment, isDeployment := object.(deploymentObject)
	if !isDeployment {
		return nil
	}
	snapshot, relevant := BuildDeploymentSnapshot(deployment, observer.now())
	if !relevant {
		return nil
	}
	_, upsertError := observer.store.UpsertDeployment(ctx, snapshot)
	return upsertError
}

// removeStale は、DB にあって、いまの k8s に無い Pod と Deployment の観測値を、削除する。
//
// 引き継いだ watcher が、止まっていた間に消えたものを、片付けるために、最初に1回行う。
//
// 引数:
//   - ctx: キャンセルや期限を伝える context。
//   - podIndexer: informer が持つ Pod の全件。
//   - deploymentIndexer: informer が持つ Deployment の全件。
//
// 戻り値:
//   - DB の読み書きに失敗したときのエラー。
func (observer *Observer) removeStale(ctx context.Context, podIndexer cache.Indexer, deploymentIndexer cache.Indexer) error {
	// DB にある Pod のうち、k8s に無いものを削除する。
	pods, listError := observer.store.ListPods(ctx)
	if listError != nil {
		return listError
	}
	for _, pod := range pods {
		_, exists, getError := podIndexer.GetByKey(pod.ProjectID + "/" + pod.PodName)
		if getError != nil {
			return getError
		}
		if !exists {
			if _, deleteError := observer.store.DeletePod(ctx, pod.ProjectID, pod.PodName, observer.now()); deleteError != nil {
				return deleteError
			}
			observer.appLogger.Info("止まっている間に消えた Pod を片付けた", "project_id", pod.ProjectID, "pod", pod.PodName)
		}
	}
	// DB に観測値がある Deployment のうち、k8s に無いものを削除する(Deployment の名前が deploy の ID)。
	deployIDs, listDeploymentsError := observer.store.ListObservedDeployIDs(ctx)
	if listDeploymentsError != nil {
		return listDeploymentsError
	}
	existingDeployIDs := map[string]bool{}
	for _, object := range deploymentIndexer.List() {
		if deployment, isDeployment := object.(deploymentObject); isDeployment {
			existingDeployIDs[deployment.Labels[k8s.DeployIDLabel]] = true
		}
	}
	for _, deployID := range deployIDs {
		if !existingDeployIDs[deployID] {
			if deleteError := observer.store.DeleteDeployment(ctx, deployID); deleteError != nil {
				return deleteError
			}
		}
	}
	return nil
}
