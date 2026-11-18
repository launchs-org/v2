package retention

import (
	"context"
	"log/slog"
	"time"
)

const (
	// ProjectEventRetention は、プロジェクトの k8s の Event を保存する期間。これより古いものは削除する(ログの保持期間と同じ)。
	ProjectEventRetention = 7 * 24 * time.Hour

	// MaxProjectEventsPerProject は、1つのプロジェクトに残す、Event の最大の件数。超えた分は、古いものから削除する。
	MaxProjectEventsPerProject = 5000
)

// ProjectEventStore は、ProjectEventPruner が必要とする、Event の保存先の操作。
//
// 実体は shared/repository の ProjectEventRepository。
type ProjectEventStore interface {
	// PruneProjectEvents は、cutoff より前に最後に起きた Event と、1つのプロジェクトにつき maxPerProject 件を超えた Event を削除し、その件数を返す。
	PruneProjectEvents(requestContext context.Context, cutoff time.Time, maxPerProject int) (int64, error)
}

// ProjectEventPruner は、プロジェクトの k8s の Event を、一定の間隔で、上限を超えた分だけ削除する。
type ProjectEventPruner struct {
	store         ProjectEventStore
	retention     time.Duration
	maxPerProject int
	interval      time.Duration
	appLogger     *slog.Logger
	// now は、現在の時刻を返す。テストで差し替える。
	now func() time.Time
}

// NewProjectEventPruner は、ProjectEventPruner を作る。
//
// 引数:
//   - store: Event の保存先。
//   - retention: Event を保存する期間。
//   - maxPerProject: 1つのプロジェクトに残す、Event の最大の件数。
//   - interval: 削除の処理を行う間隔。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した ProjectEventPruner。
func NewProjectEventPruner(store ProjectEventStore, retention time.Duration, maxPerProject int, interval time.Duration, appLogger *slog.Logger) *ProjectEventPruner {
	// 設定を持たせて返す。
	return &ProjectEventPruner{store: store, retention: retention, maxPerProject: maxPerProject, interval: interval, appLogger: appLogger, now: time.Now}
}

// Run は、始めに1回、そのあとは一定の間隔で、上限を超えた Event を削除する。ctx がキャンセルされるまで戻らない。
//
// 削除に失敗しても止まらない(次の間隔でやり直す)。
//
// 引数:
//   - ctx: 止めるための context。
func (pruner *ProjectEventPruner) Run(ctx context.Context) {
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
func (pruner *ProjectEventPruner) pruneOnce(ctx context.Context) {
	// 保存期間より前のものと、件数の上限を超えたものを、削除する。
	deletedCount, pruneError := pruner.store.PruneProjectEvents(ctx, pruner.now().Add(-pruner.retention), pruner.maxPerProject)
	if pruneError != nil {
		// 止める合図による中断は、失敗として扱わない。
		if ctx.Err() == nil {
			pruner.appLogger.Warn("古いプロジェクトの Event を削除できませんでした。次の間隔でやり直します", "error", pruneError)
		}
		return
	}
	// 削除したときだけ、ログに出す。
	if deletedCount > 0 {
		pruner.appLogger.Info("上限を超えたプロジェクトの Event を削除した", "deleted", deletedCount)
	}
}
