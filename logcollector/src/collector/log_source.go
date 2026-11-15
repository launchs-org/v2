// Package collector は、Pod のログを購読して、DB に保存する(リーダーの logcollector が動かす)。
package collector

import (
	"context"
	"errors"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	// ErrPodGone は、Pod が(k8s に)無いときのエラー。収集を終える合図になる。
	ErrPodGone = errors.New("Pod がありません")

	// ErrLogUnavailable は、まだログを読めないときのエラー(コンテナが起動前、前のコンテナが無い、など)。あとで、やり直す。
	ErrLogUnavailable = errors.New("まだログを読めません")
)

// LogRequest は、Pod のログの取得の頼み。
type LogRequest struct {
	// Namespace は、Pod の namespace(プロジェクトの ID)。
	Namespace string

	// PodName は、Pod の名前。
	PodName string

	// Container は、コンテナの名前。
	Container string

	// Follow は、続きを待ち続けるか(true なら、コンテナが終わるまで、つながったまま)。
	Follow bool

	// Previous は、1つ前に終了したコンテナのログを読むか。
	Previous bool

	// SinceTime は、この時刻(秒の精度)以降のログだけを返す。ゼロ値なら、最初から。
	SinceTime time.Time
}

// LogSource は、Pod のログを取得するもの。実体は k8s の API。テストでは、偽物に差し替える。
type LogSource interface {
	// Open は、ログの取得を始める。各行は「<RFC3339Nano の時刻> <本文>」の形で、改行で区切られる。
	// Pod が無ければ ErrPodGone、まだ読めなければ ErrLogUnavailable を返す。
	Open(ctx context.Context, request LogRequest) (io.ReadCloser, error)
}

// KubernetesLogSource は、k8s の Pod のログ API(pods/log)を使う LogSource。
type KubernetesLogSource struct {
	clientset kubernetes.Interface
}

// NewKubernetesLogSource は、KubernetesLogSource を作る。
//
// 引数:
//   - clientset: k8s のクライアント。
//
// 戻り値:
//   - 作成した KubernetesLogSource。
func NewKubernetesLogSource(clientset kubernetes.Interface) *KubernetesLogSource {
	// クライアントを持たせて返す。
	return &KubernetesLogSource{clientset: clientset}
}

// Open は、k8s の pods/log を、時刻つき(timestamps=true)で開く。
//
// 引数:
//   - ctx: キャンセルを伝える context。キャンセルすると、つながりが切れる。
//   - request: 取得の頼み。
//
// 戻り値:
//   - ログの読み取り口。Pod が無ければ ErrPodGone、コンテナが起動前・前のコンテナが無いなど(400)なら ErrLogUnavailable。
func (source *KubernetesLogSource) Open(ctx context.Context, request LogRequest) (io.ReadCloser, error) {
	// 取得の指定を作る。時刻は、秒の精度で、指定する。
	options := &corev1.PodLogOptions{Container: request.Container, Follow: request.Follow, Previous: request.Previous, Timestamps: true}
	if !request.SinceTime.IsZero() {
		options.SinceTime = &metav1.Time{Time: request.SinceTime}
	}
	// ストリームを開く。
	stream, streamError := source.clientset.CoreV1().Pods(request.Namespace).GetLogs(request.PodName, options).Stream(ctx)
	if apierrors.IsNotFound(streamError) {
		return nil, ErrPodGone
	}
	if apierrors.IsBadRequest(streamError) {
		return nil, ErrLogUnavailable
	}
	if streamError != nil {
		return nil, streamError
	}
	return stream, nil
}
