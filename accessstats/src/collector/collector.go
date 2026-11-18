// Package collector は、Traefik の Pod のログから、アクセスの集計の行(LAUNCHS_STATS)を読み取り、DB に保存する(リーダーの accessstats が動かす)。
package collector

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"launchs/shared/accessstats"
)

const (
	// maxLineBytes は、1 行の長さの上限。これを超える行は、読み飛ばす。
	maxLineBytes = 1 << 20

	// firstSinceSeconds は、最初につなぐとき、さかのぼって読む秒数(再起動の間に出た行を拾う。同じ行は、上書きされるだけ)。
	firstSinceSeconds = 300

	// reconnectSinceSeconds は、つなぎ直すとき、さかのぼって読む秒数。
	reconnectSinceSeconds = 120

	// reconnectDelay は、つなぎ直すまでの待ち。
	reconnectDelay = 3 * time.Second
)

// ErrPodGone は、Pod が(k8s に)無いときのエラー。追うのを終える合図になる。
var ErrPodGone = errors.New("Pod がありません")

// Source は、Traefik の Pod の一覧と、ログを取得するもの。実体は k8s の API。テストでは、偽物に差し替える。
type Source interface {
	// ListPods は、追う Pod の名前の一覧を返す。
	ListPods(ctx context.Context) ([]string, error)

	// OpenLog は、Pod のログを、続きを待つ形で開く。sinceSeconds 秒前から。Pod が無ければ ErrPodGone。
	OpenLog(ctx context.Context, podName string, sinceSeconds int64) (io.ReadCloser, error)
}

// Store は、集計の保存先。実体は repository.AccessStatsRepository。
type Store interface {
	// UpsertMinute は、1 分ごとの集計を保存する(同じ行は上書きする)。
	UpsertMinute(requestContext context.Context, record accessstats.Record) error
}

// KubernetesSource は、k8s の API を使う Source。
type KubernetesSource struct {
	clientset kubernetes.Interface
	namespace string
	selector  string
}

// NewKubernetesSource は、KubernetesSource を作る。
//
// 引数:
//   - clientset: k8s のクライアント。
//   - namespace: Traefik の Pod がいる namespace。
//   - selector: Traefik の Pod を選ぶラベル。
//
// 戻り値:
//   - 作成した KubernetesSource。
func NewKubernetesSource(clientset kubernetes.Interface, namespace string, selector string) *KubernetesSource {
	// 値を持たせて返す。
	return &KubernetesSource{clientset: clientset, namespace: namespace, selector: selector}
}

// ListPods は、Running の Traefik の Pod の名前を返す。
//
// 引数:
//   - ctx: キャンセルを伝える context。
//
// 戻り値:
//   - Pod の名前の一覧。k8s への依頼に失敗したときはエラー。
func (source *KubernetesSource) ListPods(ctx context.Context) ([]string, error) {
	// ラベルで選んで、一覧を取る。
	pods, listError := source.clientset.CoreV1().Pods(source.namespace).List(ctx, metav1.ListOptions{LabelSelector: source.selector})
	if listError != nil {
		return nil, listError
	}
	// 動いているものだけを返す。
	names := []string{}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.DeletionTimestamp == nil {
			names = append(names, pod.Name)
		}
	}
	return names, nil
}

// OpenLog は、Pod のログを、続きを待つ形で開く。
//
// 引数:
//   - ctx: キャンセルを伝える context。キャンセルすると、つながりが切れる。
//   - podName: Pod の名前。
//   - sinceSeconds: さかのぼる秒数。
//
// 戻り値:
//   - ログの読み取り口。Pod が無ければ ErrPodGone。
func (source *KubernetesSource) OpenLog(ctx context.Context, podName string, sinceSeconds int64) (io.ReadCloser, error) {
	// ストリームを開く。
	stream, streamError := source.clientset.CoreV1().Pods(source.namespace).GetLogs(podName, &corev1.PodLogOptions{Follow: true, SinceSeconds: &sinceSeconds}).Stream(ctx)
	if apierrors.IsNotFound(streamError) {
		return nil, ErrPodGone
	}
	return stream, streamError
}

// Settings は、Manager の設定。
type Settings struct {
	// ReconcileInterval は、追う Pod を決め直す間隔。
	ReconcileInterval time.Duration

	// KeepClients は、クライアントの IP を保存するか。
	KeepClients bool
}

// Manager は、Traefik の Pod ごとに、ログを追う。
type Manager struct {
	source    Source
	store     Store
	settings  Settings
	appLogger *slog.Logger
}

// NewManager は、Manager を作る。
//
// 引数:
//   - source: Pod とログを取得するもの。
//   - store: 集計の保存先。
//   - settings: 設定。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した Manager。
func NewManager(source Source, store Store, settings Settings, appLogger *slog.Logger) *Manager {
	// 値を持たせて返す。
	return &Manager{source: source, store: store, settings: settings, appLogger: appLogger}
}

// Run は、決まった間隔で Pod の一覧を見て、新しい Pod を追い始め、無くなった Pod の追跡を止める。ctx が終わると、全て止めて戻る。
//
// 引数:
//   - ctx: 止める合図(リーダーでなくなったときも、キャンセルされる)。
func (manager *Manager) Run(ctx context.Context) {
	// 追っている Pod と、その止め方。
	cancels := map[string]context.CancelFunc{}
	var waitGroup sync.WaitGroup
	defer func() {
		// 全ての追跡を止めて、終わるのを待つ。
		for _, cancel := range cancels {
			cancel()
		}
		waitGroup.Wait()
	}()
	ticker := time.NewTicker(manager.settings.ReconcileInterval)
	defer ticker.Stop()
	for {
		// 一覧と、追っているものを合わせる。
		manager.reconcile(ctx, cancels, &waitGroup)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// reconcile は、Pod の一覧に、追跡を合わせる。一覧を取れなかったときは、何も変えない。
func (manager *Manager) reconcile(ctx context.Context, cancels map[string]context.CancelFunc, waitGroup *sync.WaitGroup) {
	// 今の Pod の一覧を取る。
	names, listError := manager.source.ListPods(ctx)
	if listError != nil {
		if ctx.Err() == nil {
			manager.appLogger.Warn("Traefik の Pod の一覧を取れませんでした", "error", listError)
		}
		return
	}
	present := map[string]bool{}
	for _, name := range names {
		present[name] = true
		// 追っていなければ、追い始める。
		if _, following := cancels[name]; !following {
			followContext, cancel := context.WithCancel(ctx)
			cancels[name] = cancel
			waitGroup.Add(1)
			go func(podName string) {
				defer waitGroup.Done()
				manager.follow(followContext, podName)
			}(name)
			manager.appLogger.Info("Traefik の Pod のアクセスの集計を追い始める", "pod", name)
		}
	}
	// 無くなった Pod の追跡を止める。
	for name, cancel := range cancels {
		if !present[name] {
			cancel()
			delete(cancels, name)
		}
	}
}

// follow は、Pod のログを追い続ける。切れたら、つなぎ直す。Pod が無くなる、または ctx が終わると戻る。
func (manager *Manager) follow(ctx context.Context, podName string) {
	// 最初は長めにさかのぼる。
	since := int64(firstSinceSeconds)
	for ctx.Err() == nil {
		stream, openError := manager.source.OpenLog(ctx, podName, since)
		if errors.Is(openError, ErrPodGone) {
			return
		}
		if openError != nil {
			if ctx.Err() == nil {
				manager.appLogger.Warn("Traefik のログを開けませんでした。つなぎ直します", "pod", podName, "error", openError)
			}
		} else {
			manager.ingest(ctx, podName, stream)
			_ = stream.Close()
		}
		// つなぎ直すまで、待つ。
		since = reconnectSinceSeconds
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

// ingest は、ストリームを 1 行ずつ読み、集計の行を保存する。ストリームが終わる、または ctx が終わると戻る。
func (manager *Manager) ingest(ctx context.Context, podName string, stream io.Reader) {
	// 長い行も読めるよう、バッファを大きくする。
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		// 集計の行だけを読み取る。
		record, parsed := accessstats.ParseLine(scanner.Text(), manager.settings.KeepClients)
		if !parsed {
			continue
		}
		record.Replica = podName
		// 保存する。失敗しても、続ける(次の間隔の行は、保存できるかもしれない)。
		if saveError := manager.store.UpsertMinute(ctx, record); saveError != nil && ctx.Err() == nil {
			manager.appLogger.Warn("アクセスの集計を保存できませんでした", "pod", podName, "ingress_id", record.IngressID, "error", saveError)
		}
	}
}
