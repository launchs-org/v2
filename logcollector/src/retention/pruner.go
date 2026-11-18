// Package retention は、保存期間と上限を超えたログを、定期的に削除する(リーダーの logcollector が動かす)。
package retention

import (
	"context"
	"log/slog"
	"time"

	"launchs/shared/repository"
)

// PruneInterval は、削除する間隔(設計: 1分ごと)。
const PruneInterval = time.Minute

// LogStore は、Pruner が使う、DB の操作。実体は repository.DeployLogRepository。
type LogStore interface {
	// PruneLogs は、cutoff より古いもの、Pod ごとの上限を超えたものを削除する。
	PruneLogs(requestContext context.Context, cutoff time.Time, maxBytesPerPod int64) (repository.LogPruneResult, error)
}

// Pruner は、保存期間と上限を超えたログを、定期的に削除する。
type Pruner struct {
	store          LogStore
	retention      time.Duration
	maxBytesPerPod int64
	interval       time.Duration
	appLogger      *slog.Logger
	// now は、現在の時刻を返す関数(テストで差し替える)。
	now func() time.Time
}

// NewPruner は、Pruner を作る。
//
// 引数:
//   - store: DB の操作。
//   - retention: ログを残す期間。
//   - maxBytesPerPod: Pod ごとの大きさの上限(バイト)。
//   - interval: 削除する間隔。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した Pruner。
func NewPruner(store LogStore, retention time.Duration, maxBytesPerPod int64, interval time.Duration, appLogger *slog.Logger) *Pruner {
	// 依存するものと、実際の時計を持たせて返す。
	return &Pruner{store: store, retention: retention, maxBytesPerPod: maxBytesPerPod, interval: interval, appLogger: appLogger, now: time.Now}
}

// Run は、始めに1回、そのあとは interval ごとに、削除する。ctx が終わると戻る。
//
// 引数:
//   - ctx: 止める合図(リーダーでなくなったときも、キャンセルされる)。
func (pruner *Pruner) Run(ctx context.Context) {
	// 始めに1回。
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

// pruneOnce は、1回だけ削除する。失敗しても止まらず、次の間隔でやり直す。
//
// 引数:
//   - ctx: 止める合図。
func (pruner *Pruner) pruneOnce(ctx context.Context) {
	// 保存期間より古いもの、上限を超えたものを削除する。
	result, pruneError := pruner.store.PruneLogs(ctx, pruner.now().Add(-pruner.retention), pruner.maxBytesPerPod)
	if pruneError != nil {
		if ctx.Err() == nil {
			pruner.appLogger.Warn("古いログを削除できませんでした。次の間隔でやり直します", "error", pruneError)
		}
		return
	}
	// 削除したものがあれば、記録する。
	if result.ExpiredLines > 0 || result.OverflowLines > 0 || result.DeletedStreams > 0 {
		pruner.appLogger.Info("古いログを削除した", "expired_lines", result.ExpiredLines, "overflow_lines", result.OverflowLines, "deleted_streams", result.DeletedStreams)
	}
}
