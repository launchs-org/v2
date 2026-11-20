// Package metrics は、deploy の Pod の CPU・メモリ・再起動の回数を、一定の間隔で観測して、DB に保存する。
package metrics

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"launchs/shared/k8s"
	"launchs/shared/model"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// CollectInterval は、観測する間隔。
	CollectInterval = 30 * time.Second

	// Retention は、観測値を保存する期間(これより古いものは削除する)。画面で選べる最長の範囲(24 時間)に合わせる。
	Retention = 24 * time.Hour

	// pruneEvery は、何回の観測ごとに、古い観測値を削除するか(30 秒 × 120 = 1 時間ごと)。
	pruneEvery = 120
)

// Store は、Collector が必要とする、観測値の保存先(実体は repository.DeployMetricRepository)。
type Store interface {
	// InsertSamples は、観測値を保存する。
	InsertSamples(requestContext context.Context, samples []model.DeployMetricSample) error

	// PruneSamples は、cutoff より古い観測値を削除する。
	PruneSamples(requestContext context.Context, cutoff time.Time) (int64, error)
}

// Collector は、全ての deploy の Pod を観測して、deploy ごとに合計して保存する。
type Collector struct {
	clientset kubernetes.Interface
	store     Store
	appLogger *slog.Logger
	// now は、現在の時刻を返す。テストで差し替える。
	now func() time.Time
	// warnedUnavailable は、metrics-server が使えないことを、すでにログに出したか(毎回出さないため)。
	warnedUnavailable bool
}

// New は、Collector を作る。
//
// 引数:
//   - clientset: k8s への接続。
//   - store: 観測値の保存先。
//   - appLogger: ログの出力先。
func New(clientset kubernetes.Interface, store Store, appLogger *slog.Logger) *Collector {
	// 部品を持たせて返す。
	return &Collector{clientset: clientset, store: store, appLogger: appLogger, now: time.Now}
}

// Run は、止められるまで、一定の間隔で観測を繰り返す。
//
// 1 回の失敗は、ログに出して、次の回に進む(止めない)。
//
// 引数:
//   - runContext: 止めるための context。
func (collector *Collector) Run(runContext context.Context) {
	// 一定の間隔で呼ばれる時計を作る。
	ticker := time.NewTicker(CollectInterval)
	defer ticker.Stop()
	// 回数を数える(古い観測値の削除の間隔に使う)。
	round := 0
	for {
		// 1 回観測する。
		if collectError := collector.CollectOnce(runContext); collectError != nil && runContext.Err() == nil {
			collector.appLogger.Warn("メトリクスの観測に失敗しました", "error", collectError)
		}
		// 一定の回数ごとに、古い観測値を削除する。
		if round%pruneEvery == 0 {
			if _, pruneError := collector.store.PruneSamples(runContext, collector.now().Add(-Retention)); pruneError != nil && runContext.Err() == nil {
				collector.appLogger.Warn("古いメトリクスの削除に失敗しました", "error", pruneError)
			}
		}
		round++
		// 次の回まで待つ。止められたら終わる。
		select {
		case <-runContext.Done():
			return
		case <-ticker.C:
		}
	}
}

// podMetricsList は、metrics.k8s.io の PodMetricsList のうち、使う部分だけ。
type podMetricsList struct {
	Items []struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Containers []struct {
			Usage map[string]string `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

// CollectOnce は、全ての deploy の Pod を 1 回観測して、保存する。
//
// Pod の数・Ready・再起動は、Pod の一覧から。CPU とメモリは、metrics-server(metrics.k8s.io)から。
// metrics-server が無いときは、CPU とメモリを nil にして、他の値だけを保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - Pod の一覧の取得か、保存に失敗したときのエラー。
func (collector *Collector) CollectOnce(requestContext context.Context) error {
	// 観測の時刻を決める(同じ回は、全ての deploy で同じ時刻。秒に丸める)。
	sampledAt := collector.now().UTC().Truncate(time.Second)
	// deploy の Pod を、全ての namespace から読む。
	pods, listError := collector.clientset.CoreV1().Pods(metav1.NamespaceAll).List(requestContext, metav1.ListOptions{LabelSelector: k8s.DeployIDLabel})
	if listError != nil {
		return listError
	}
	// deploy ごとの集計を作る。
	samples := map[string]*model.DeployMetricSample{}
	for index := range pods.Items {
		pod := &pods.Items[index]
		deployID := pod.Labels[k8s.DeployIDLabel]
		// 終わった Pod(Succeeded、Failed)は、数えない。
		if deployID == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		sample := samples[deployID]
		if sample == nil {
			sample = &model.DeployMetricSample{DeployID: deployID, SampledAt: sampledAt}
			samples[deployID] = sample
		}
		// Pod の数、Ready の数、再起動の回数を足す。
		sample.Pods++
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				sample.ReadyPods++
			}
		}
		for _, status := range pod.Status.ContainerStatuses {
			sample.Restarts += int(status.RestartCount)
		}
	}
	// CPU とメモリを、metrics-server から足す。使えなければ、nil のまま。
	collector.addUsage(requestContext, samples)
	// 保存する(Pod が 1 つも無ければ、何もしない)。
	rows := make([]model.DeployMetricSample, 0, len(samples))
	for _, sample := range samples {
		rows = append(rows, *sample)
	}
	if len(rows) == 0 {
		return nil
	}
	return collector.store.InsertSamples(requestContext, rows)
}

// addUsage は、metrics-server の Pod の使用量を、deploy ごとに足す。
//
// 取れなかったとき(metrics-server が無い、など)は、最初の 1 回だけログに出して、何も足さない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - samples: deploy の ID ごとの集計(CPU とメモリを書き足す)。
func (collector *Collector) addUsage(requestContext context.Context, samples map[string]*model.DeployMetricSample) {
	// metrics.k8s.io の Pod の使用量を、deploy の Pod だけ読む。
	raw, requestError := collector.clientset.CoreV1().RESTClient().Get().AbsPath("/apis/metrics.k8s.io/v1beta1/pods").
		Param("labelSelector", k8s.DeployIDLabel).DoRaw(requestContext)
	if requestError != nil {
		// 使えないことを、最初の 1 回だけログに出す。
		if !collector.warnedUnavailable {
			collector.appLogger.Warn("metrics-server から使用量を取得できません(CPU とメモリは記録しません)", "error", requestError)
			collector.warnedUnavailable = true
		}
		return
	}
	// 取れたら、次に失敗したときも、また 1 回ログに出す。
	collector.warnedUnavailable = false
	// JSON を読む。
	var list podMetricsList
	if decodeError := json.Unmarshal(raw, &list); decodeError != nil {
		return
	}
	// Pod ごとに、コンテナの使用量を足す。
	for _, item := range list.Items {
		sample := samples[item.Metadata.Labels[k8s.DeployIDLabel]]
		if sample == nil {
			continue
		}
		for _, container := range item.Containers {
			// CPU は、ミリコアにして足す。
			if cpu, parseError := resource.ParseQuantity(container.Usage["cpu"]); parseError == nil {
				total := addInt64(sample.CPUMillicores, cpu.MilliValue())
				sample.CPUMillicores = &total
			}
			// メモリは、バイトにして足す。
			if memory, parseError := resource.ParseQuantity(container.Usage["memory"]); parseError == nil {
				total := addInt64(sample.MemoryBytes, memory.Value())
				sample.MemoryBytes = &total
			}
		}
	}
}

// addInt64 は、nil かもしれない合計に、値を足した結果を返す(nil は 0 として扱う)。
//
// 引数:
//   - total: いまの合計(nil なら 0)。
//   - value: 足す値。
//
// 戻り値:
//   - 足した結果。
func addInt64(total *int64, value int64) int64 {
	// nil なら、足す値だけ。
	if total == nil {
		return value
	}
	return *total + value
}
