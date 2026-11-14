package observer

import (
	"context"
	"launchs/shared/k8s"

	"k8s.io/client-go/tools/cache"
)

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
	// ----- いまの k8s の状態を調べる -----
	object, exists, getError := indexer.GetByKey(key)
	if getError != nil {
		return getError
	}
	namespace, name, splitError := cache.SplitMetaNamespaceKey(key)
	if splitError != nil {
		return splitError
	}

	// ----- k8s にもう無いとき: DB の行を消して、deleted のイベントを残す -----
	if !exists {
		deleted, deleteError := observer.store.DeletePod(ctx, namespace, name, observer.now())
		if deleteError == nil && deleted {
			observer.appLogger.Info("Pod が消えた", "project_id", namespace, "pod", name)
		}
		return deleteError
	}
	// ----- k8s にあるとき: 状態を作って、DB を k8s の値で上書きする(deploy のものでなければ、何もしない) -----
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
	// ----- 状態の変化を、運用で追えるよう、ログにも出す -----
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
	// ----- Pod: DB にあって、k8s に無いものを削除する -----
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
	// ----- Deployment の観測値: DB にあって、k8s に無いものを削除する(Deployment の名前が deploy の ID) -----
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
