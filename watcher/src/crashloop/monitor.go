// Package crashloop は、クラッシュなどの異常な状態が続く deploy を見つけて、止める Workflow を開始する。
//
// 止める(レプリカを 0 にする)のは、ワーカーの Workflow。watcher は、見つけて、開始を頼むだけ。
package crashloop

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"launchs/shared/repository"
	"launchs/shared/workflow"
)

// CheckInterval は、止める候補を探す間隔。
const CheckInterval = time.Minute

// CandidateStore は、Monitor が必要とする、止める候補の読み取り。
//
// 実体は shared/repository の DeployObservationRepository。
type CandidateStore interface {
	// ListCrashLoopCandidates は、止めるべき deploy を、1つにつき1件返す。
	ListCrashLoopCandidates(requestContext context.Context, limits repository.CrashLoopLimits) ([]repository.CrashLoopCandidate, error)
}

// Starter は、deploy を止める処理(Workflow)を開始する。完了は待たない。
//
// 実体は TemporalStarter。テストでは fake に差し替える。
type Starter interface {
	// StartStop は、止める Workflow を開始する。
	StartStop(requestContext context.Context, input workflow.StopCrashLoopInput) error
}

// Monitor は、一定の間隔で、止めるべき deploy を探し、止める Workflow を開始する。
type Monitor struct {
	store     CandidateStore
	starter   Starter
	limits    repository.CrashLoopLimits
	interval  time.Duration
	appLogger *slog.Logger
}

// NewMonitor は、Monitor を作る。
//
// 引数:
//   - store: 止める候補の読み取り先。
//   - starter: 止める処理の開始役。
//   - limits: 止める判断の上限。
//   - interval: 候補を探す間隔。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した Monitor。
func NewMonitor(store CandidateStore, starter Starter, limits repository.CrashLoopLimits, interval time.Duration, appLogger *slog.Logger) *Monitor {
	// 設定を持たせて返す。
	return &Monitor{store: store, starter: starter, limits: limits, interval: interval, appLogger: appLogger}
}

// Run は、始めに1回、そのあとは一定の間隔で、止めるべき deploy を探し、止める Workflow を開始する。ctx がキャンセルされるまで戻らない。
//
// 失敗しても止まらない(次の間隔でやり直す)。同じ deploy を重ねて止めようとしても、Workflow が、deploy が active のときだけ
// 進めるので、2回目以降は何もしない。
//
// 引数:
//   - ctx: 止めるための context。
func (monitor *Monitor) Run(ctx context.Context) {
	// 始めに1回行い、そのあとは、間隔ごとに行う。
	monitor.checkOnce(ctx)
	ticker := time.NewTicker(monitor.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			monitor.checkOnce(ctx)
		}
	}
}

// checkOnce は、止める候補を1回探して、見つかった deploy ごとに、止める Workflow を開始する。
//
// 引数:
//   - ctx: キャンセルや期限を伝える context。
func (monitor *Monitor) checkOnce(ctx context.Context) {
	// ----- 止めるべき deploy を探す -----
	candidates, listError := monitor.store.ListCrashLoopCandidates(ctx, monitor.limits)
	if listError != nil {
		if ctx.Err() == nil {
			monitor.appLogger.Warn("止める候補の deploy を探せませんでした。次の間隔でやり直します", "error", listError)
		}
		return
	}
	// ----- 見つかった deploy ごとに、止める Workflow を開始する -----
	for _, candidate := range candidates {
		stopInput := newStopInput(candidate)
		if startError := monitor.starter.StartStop(ctx, stopInput); startError != nil {
			if ctx.Err() == nil {
				monitor.appLogger.Warn("deploy を止める処理を開始できませんでした。次の間隔でやり直します", "deploy_id", candidate.DeployID, "error", startError)
			}
			continue
		}
		monitor.appLogger.Warn("異常な状態が続く deploy を止める処理を開始した", "deploy_id", candidate.DeployID,
			"pod", candidate.PodName, "reason", stopInput.Reason, "message", stopInput.Message)
	}
}

// newStopInput は、止める候補から、止める Workflow の入力を作る。
//
// 引数:
//   - candidate: 止める候補の deploy と、根拠になった Pod。
//
// 戻り値:
//   - Workflow の入力。理由は、Pod が待っている理由(CrashLoopBackOff など)。無ければ Unhealthy。
func newStopInput(candidate repository.CrashLoopCandidate) workflow.StopCrashLoopInput {
	// 理由は、待っている理由があればそれ、無ければ Unhealthy にする。
	reason := candidate.WaitingReason
	if reason == "" {
		reason = "Unhealthy"
	}
	// 補足に、再起動の回数と、安定して動いていない状態が続いた時間を残す。
	message := fmt.Sprintf("再起動 %d 回、安定して動いていない状態が %d 分続いた", candidate.RestartCount, int(candidate.UnhealthyFor.Minutes()))
	return workflow.StopCrashLoopInput{DeployID: candidate.DeployID, PodUID: candidate.PodUID, PodName: candidate.PodName, Reason: reason, Message: message}
}
