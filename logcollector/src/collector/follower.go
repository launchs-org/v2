package collector

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"launchs/shared/k8s"
	"launchs/shared/model"
	"launchs/shared/repository"
)

// pod の phase のうち、コンテナがもう動かない(ログがこれ以上増えない)もの。
const (
	podPhaseSucceeded = "Succeeded"
	podPhaseFailed    = "Failed"
)

// FollowerStore は、Follower が使う、DB の操作。実体は repository.DeployLogRepository。
type FollowerStore interface {
	// FindLivePod は、Pod を deploy_pods から探す。
	FindLivePod(requestContext context.Context, podUID string) (model.DeployPod, bool, error)

	// AppendLines は、行を保存し、読み取り位置と大きさを、同じトランザクションで更新する。
	AppendLines(requestContext context.Context, podUID string, lines []model.DeployLog, position model.ResumePosition) error

	// SetRestartCount は、処理した、コンテナの再起動の回数を保存する。
	SetRestartCount(requestContext context.Context, podUID string, restartCount int) error

	// FinishStream は、ストリームを終了にする。
	FinishStream(requestContext context.Context, podUID string) error
}

// FollowerSettings は、Follower の設定。
type FollowerSettings struct {
	// MaxLineBytes は、1行の長さの上限(バイト)。
	MaxLineBytes int

	// IngestBytesPerSecond は、書き込みの速さの上限(バイト/秒)。
	IngestBytesPerSecond int

	// BatchLines は、まとめて書く行数の上限(これに達したら、すぐ書く)。
	BatchLines int

	// FlushInterval は、まとめて書く間隔(行が少なくても、この間隔で書く)。
	FlushInterval time.Duration

	// MinBackoff と MaxBackoff は、つなぎ直す前に待つ時間の、最小と最大(失敗が続くと、倍々で延びる)。
	MinBackoff time.Duration
	MaxBackoff time.Duration

	// StableAfter は、この時間以上つながっていたら、うまくいっていたとみなして、待つ時間を最小に戻す。
	StableAfter time.Duration

	// Now は、現在の時刻を返す関数(テストで差し替える。nil なら time.Now)。
	Now func() time.Time
}

// DefaultFollowerSettings は、既定の設定に、指定の値を入れて返す。
//
// 引数:
//   - maxLineBytes: 1行の長さの上限。
//   - ingestBytesPerSecond: 書き込みの速さの上限。
//   - flushInterval: まとめて書く間隔。
//
// 戻り値:
//   - Follower の設定。
func DefaultFollowerSettings(maxLineBytes int, ingestBytesPerSecond int, flushInterval time.Duration) FollowerSettings {
	// 待ち時間などは、設計(docs/logs.md §5)の値にする。
	return FollowerSettings{
		MaxLineBytes: maxLineBytes, IngestBytesPerSecond: ingestBytesPerSecond, BatchLines: 500, FlushInterval: flushInterval,
		MinBackoff: time.Second, MaxBackoff: 30 * time.Second, StableAfter: 10 * time.Second, Now: time.Now,
	}
}

// Follower は、Pod 1つのログを購読して、DB に保存する。
type Follower struct {
	store        FollowerStore
	source       LogSource
	settings     FollowerSettings
	appLogger    *slog.Logger
	podUID       string
	position     model.ResumePosition
	restartCount int
}

// NewFollower は、Follower を作る。
//
// 引数:
//   - store: DB の操作。
//   - source: ログの取得元。
//   - settings: 設定。
//   - appLogger: ログの出力先。
//   - stream: 追う Pod のストリーム(読み取り位置と、処理した再起動の回数を持つ)。
//
// 戻り値:
//   - 作成した Follower。
func NewFollower(store FollowerStore, source LogSource, settings FollowerSettings, appLogger *slog.Logger, stream model.DeployLogStream) *Follower {
	// 時計が無ければ、実際の時計を使う。
	if settings.Now == nil {
		settings.Now = time.Now
	}
	return &Follower{
		store: store, source: source, settings: settings, appLogger: appLogger.With("pod_uid", stream.PodUID, "deploy_id", stream.DeployID),
		podUID: stream.PodUID, position: stream.ResumePosition(), restartCount: stream.RestartCount,
	}
}

// Run は、Pod のログを、ctx が終わるまで(または、Pod が無くなる・コンテナが終わるまで)、保存し続ける。
//
// 流れ(docs/logs.md §5):
//  1. Pod が deploy_pods に無ければ、ストリームを終了にして、終わる。
//  2. コンテナの再起動の回数が増えていれば、前のコンテナのログの残りを、1回読む。
//  3. いまのコンテナのログを、follow で読んで、保存する。
//  4. つながりが切れたら、間隔を空けて、1 からやり直す。コンテナが終わった Pod(Succeeded / Failed)なら、終了にして、終わる。
//
// 引数:
//   - ctx: 止める合図(キャンセル)。
func (follower *Follower) Run(ctx context.Context) {
	// 待つ時間は、最小から始める。
	backoff := follower.settings.MinBackoff
	for ctx.Err() == nil {
		// Pod が、まだあるかを確かめる(無ければ、終了)。
		pod, found, findError := follower.store.FindLivePod(ctx, follower.podUID)
		if findError != nil {
			follower.warn(ctx, "Pod を確認できませんでした", findError)
			backoff = follower.wait(ctx, backoff, false)
			continue
		}
		if !found {
			follower.finish("Pod が無くなりました")
			return
		}
		// コンテナが再起動していたら、前のコンテナのログの残りを読む。
		if pod.RestartCount > follower.restartCount {
			previousError := follower.consume(ctx, pod, true)
			if previousError != nil && !errors.Is(previousError, ErrLogUnavailable) {
				if follower.handleStop(ctx, previousError) {
					return
				}
				backoff = follower.wait(ctx, backoff, false)
				continue
			}
			// 読めた、または、前のコンテナが無い。どちらも、処理済みにする。
			if setError := follower.store.SetRestartCount(ctx, follower.podUID, pod.RestartCount); setError != nil {
				follower.warn(ctx, "再起動の回数を保存できませんでした", setError)
				backoff = follower.wait(ctx, backoff, false)
				continue
			}
			follower.restartCount = pod.RestartCount
		}
		// いまのコンテナのログを、続きを待ちながら読む。
		startedAt := follower.settings.Now()
		consumeError := follower.consume(ctx, pod, false)
		if ctx.Err() != nil {
			return
		}
		if consumeError != nil && follower.handleStop(ctx, consumeError) {
			return
		}
		// 読み終えた(つながりが切れた)。コンテナが終わった Pod は、これ以上増えないので、終了にする。
		if consumeError == nil && (pod.Phase == podPhaseSucceeded || pod.Phase == podPhaseFailed) {
			follower.finish("コンテナが終了しました")
			return
		}
		if consumeError != nil && !errors.Is(consumeError, ErrLogUnavailable) {
			follower.warn(ctx, "ログを読めませんでした。つなぎ直します", consumeError)
		}
		// 長くつながっていたなら、待ち時間を最小に戻して、つなぎ直す。
		stable := follower.settings.Now().Sub(startedAt) >= follower.settings.StableAfter
		backoff = follower.wait(ctx, backoff, stable)
	}
}

// handleStop は、エラーが、収集を終える種類(Pod が無い、ストリームが無い)なら、後始末をして、true を返す。
//
// 引数:
//   - ctx: 止める合図。
//   - handledError: 調べるエラー。
//
// 戻り値:
//   - 収集を終えるなら true。
func (follower *Follower) handleStop(ctx context.Context, handledError error) bool {
	// ストリームが無い(deploy が削除された)ときは、何も残さず、終わる。
	if errors.Is(handledError, repository.ErrLogStreamGone) {
		follower.appLogger.Info("ログのストリームが無くなったため、収集を終える")
		return true
	}
	// Pod が無いときは、終了にして、終わる。
	if errors.Is(handledError, ErrPodGone) {
		follower.finish("Pod が k8s に無くなりました")
		return true
	}
	return ctx.Err() != nil
}

// finish は、ストリームを終了にする(止める合図の後でも、書けるよう、context を切り離す)。
//
// 引数:
//   - reason: 終わる理由(ログ用)。
func (follower *Follower) finish(reason string) {
	// 時間を限って、終了にする。
	finishContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if finishError := follower.store.FinishStream(finishContext, follower.podUID); finishError != nil {
		follower.appLogger.Warn("ログのストリームを終了にできませんでした", "error", finishError)
		return
	}
	follower.appLogger.Info("ログの収集を終えた", "reason", reason)
}

// warn は、止める合図の最中でなければ、警告を残す。
//
// 引数:
//   - ctx: 止める合図。
//   - message: メッセージ。
//   - cause: 原因のエラー。
func (follower *Follower) warn(ctx context.Context, message string, cause error) {
	// 止める合図の最中のエラーは、当然なので、残さない。
	if ctx.Err() == nil {
		follower.appLogger.Warn(message, "error", cause)
	}
}

// wait は、つなぎ直す前に、待つ。待った後の、次の待ち時間を返す。
//
// 引数:
//   - ctx: 止める合図(合図があれば、すぐ戻る)。
//   - backoff: 今回待つ時間。
//   - reset: true なら、待ち時間を最小に戻す(うまくいっていたとき)。
//
// 戻り値:
//   - 次回の待ち時間(倍にして、最大まで)。
func (follower *Follower) wait(ctx context.Context, backoff time.Duration, reset bool) time.Duration {
	// うまくいっていたなら、最小に戻す。
	if reset {
		backoff = follower.settings.MinBackoff
	}
	// 待つ(止める合図があれば、途中で戻る)。
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	// 次回は倍にする(最大まで)。
	next := backoff * 2
	if next > follower.settings.MaxBackoff {
		next = follower.settings.MaxBackoff
	}
	return next
}

// consume は、Pod のログを1回つないで、読み切るまで(つながりが切れるまで)保存する。
//
// 保存する行は、まとめて(BatchLines 行、または FlushInterval ごと)書く。書くときは、読み取り位置も、同じトランザクションで進める。
// すでに保存した行(再接続で、また届く行)は、読み取り位置で取り除く。
//
// 引数:
//   - ctx: 止める合図。合図があれば、書いていない分を書いてから、戻る。
//   - pod: 追う Pod(namespace と名前を使う)。
//   - previous: true なら、1つ前に終了したコンテナのログを、続きを待たずに読む。
//
// 戻り値:
//   - 読み切ったら nil。取得の失敗は、そのエラー(Pod が無ければ ErrPodGone)。保存の失敗は、そのエラー(ストリームが無ければ repository.ErrLogStreamGone)。
func (follower *Follower) consume(ctx context.Context, pod model.DeployPod, previous bool) error {
	// 取得を始める(ここから先は、この関数が終わったら、必ず閉じる)。
	consumeContext, cancel := context.WithCancel(ctx)
	defer cancel()
	request := LogRequest{Namespace: pod.ProjectID, PodName: pod.PodName, Container: k8s.AppContainerName, Follow: !previous, Previous: previous}
	if follower.position.NS > 0 {
		request.SinceTime = time.Unix(0, follower.position.NS).UTC().Truncate(time.Second)
	}
	reader, openError := follower.source.Open(consumeContext, request)
	if openError != nil {
		return openError
	}
	// 止める合図で、読み取りを中断できるよう、閉じる係を置く。
	go func() {
		<-consumeContext.Done()
		_ = reader.Close()
	}()
	// 読む係は別の goroutine に任せ、行を、チャネルで受け取る(時間での書き込みを、読み取りの待ちに邪魔されないため)。
	lineChannel := make(chan rawLine)
	readResult := make(chan error, 1)
	go func() {
		defer close(lineChannel)
		buffered := bufio.NewReaderSize(reader, 64*1024)
		for {
			text, readError := readLimitedLine(buffered, follower.settings.MaxLineBytes)
			if readError != nil {
				if errors.Is(readError, io.EOF) {
					readError = nil
				}
				readResult <- readError
				return
			}
			nanoseconds, body, hasTimestamp := model.ParseKubeLogLine(text)
			select {
			case lineChannel <- rawLine{ns: nanoseconds, text: body, hasTimestamp: hasTimestamp}:
			case <-consumeContext.Done():
				readResult <- consumeContext.Err()
				return
			}
		}
	}()
	// 受け取った行を、整えて、まとめて書く。
	pipeline := newLinePipeline(follower, ctx)
	ticker := time.NewTicker(follower.settings.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case line, open := <-lineChannel:
			// 読み終えた(つながりが切れた): 残りを書いて、読む係の結果を返す。
			if !open {
				if flushError := pipeline.flush(true); flushError != nil {
					return flushError
				}
				return <-readResult
			}
			if flushError := pipeline.add(line); flushError != nil {
				return flushError
			}
		case <-ticker.C:
			if flushError := pipeline.flush(false); flushError != nil {
				return flushError
			}
		case <-ctx.Done():
			// 止める合図: 書いていない分を、切り離した context で書いてから、戻る。
			_ = pipeline.flush(true)
			return ctx.Err()
		}
	}
}
