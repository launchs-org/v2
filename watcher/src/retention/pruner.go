// Package retention は、watcher が書く記録のうち、増え続けるものを、上限を超えたら削除する。
package retention

import (
	"context"
	"log/slog"
	"time"
)

const (
	// PodEventRetention は、Pod の状態変化のイベントを保存する期間。これより古いものは削除する。
	PodEventRetention = 30 * 24 * time.Hour

	// MaxPodEventsPerDeploy は、1つの deploy に残す、Pod のイベントの最大の件数。超えた分は、古いものから削除する。
	// クラッシュを繰り返す Pod は、再起動のたびに数件ずつ増えるので、期間だけでは足りない。
	MaxPodEventsPerDeploy = 1000

	// PruneInterval は、削除の処理を行う間隔。
	PruneInterval = time.Hour
)

// EventStore は、Pruner が必要とする、イベントの保存先の操作。
//
// 実体は shared/repository の DeployObservationRepository。
type EventStore interface {
	// PruneEvents は、cutoff より前のイベントと、1つの deploy につき maxPerDeploy 件を超えたイベントを削除し、その件数を返す。
	PruneEvents(requestContext context.Context, cutoff time.Time, maxPerDeploy int) (int64, error)
}

// Pruner は、Pod のイベントを、一定の間隔で、上限を超えた分だけ削除する。
type Pruner struct {
	store        EventStore
	retention    time.Duration
	maxPerDeploy int
	interval     time.Duration
	appLogger    *slog.Logger
	// now は、現在の時刻を返す。テストで差し替える。
	now func() time.Time
}

// NewPruner は、Pruner を作る。
//
// 引数:
//   - store: イベントの保存先。
//   - retention: イベントを保存する期間。
//   - maxPerDeploy: 1つの deploy に残す、イベントの最大の件数。
//   - interval: 削除の処理を行う間隔。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した Pruner。
func NewPruner(store EventStore, retention time.Duration, maxPerDeploy int, interval time.Duration, appLogger *slog.Logger) *Pruner {
	// 設定を持たせて返す。
	return &Pruner{store: store, retention: retention, maxPerDeploy: maxPerDeploy, interval: interval, appLogger: appLogger, now: time.Now}
}

// Run は、始めに1回、そのあとは一定の間隔で、上限を超えたイベントを削除する。ctx がキャンセルされるまで戻らない。
//
// 削除に失敗しても止まらない(次の間隔でやり直す)。
//
// 引数:
//   - ctx: 止めるための context。
func (pruner *Pruner) Run(ctx context.Context) {
	// 始めに1回行い、そのあとは、間隔ごとに行う。
	pruner.pruneOnce(ctx)
	ticker := time.NewTicker(pruner.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruner.pruneOnce(ctx)
		}
	}
}

// pruneOnce は、削除の処理を1回行い、結果をログに出す。
//
// 引数:
//   - ctx: キャンセルや期限を伝える context。
func (pruner *Pruner) pruneOnce(ctx context.Context) {
	// 保存期間より前に発生したものと、件数の上限を超えたものを、削除する。
	deletedCount, pruneError := pruner.store.PruneEvents(ctx, pruner.now().Add(-pruner.retention), pruner.maxPerDeploy)
	if pruneError != nil {
		// 止める合図による中断は、失敗として扱わない。
		if ctx.Err() == nil {
			pruner.appLogger.Warn("古い Pod のイベントを削除できませんでした。次の間隔でやり直します", "error", pruneError)
		}
		return
	}
	// 削除したときだけ、ログに出す。
	if deletedCount > 0 {
		pruner.appLogger.Info("上限を超えた Pod のイベントを削除した", "deleted", deletedCount)
	}
}
