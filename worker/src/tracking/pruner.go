package tracking

import (
	"context"
	"log/slog"
	"time"
)

const (
	// TaskRetention は、タスクを残す期間。
	TaskRetention = 30 * 24 * time.Hour

	// MaxTasksPerProject は、プロジェクトごとに残すタスクの最大の件数。
	MaxTasksPerProject = 1000

	// PruneInterval は、古いタスクを削除する間隔。
	PruneInterval = time.Hour
)

// TaskPruneStore は、古いタスクの削除に使う、保存先。実体は repository.ProjectTaskRepository。
type TaskPruneStore interface {
	// Prune は、保存期間を過ぎたタスクと、件数の上限を超えたタスクと、プロジェクトが無いタスクを削除する。
	Prune(requestContext context.Context, cutoff time.Time, maxPerProject int) error
}

// RunPruner は、始めに 1 回、そのあとは PruneInterval ごとに、古いタスクを削除する。ctx が終わると戻る。
// worker は複数動くことがあるが、削除は何度行っても同じ結果になるので、リーダーは決めない。
//
// 引数:
//   - ctx: 止める合図。
//   - store: 保存先。
//   - appLogger: ログの出力先。
func RunPruner(ctx context.Context, store TaskPruneStore, appLogger *slog.Logger) {
	// 始めに 1 回。
	pruneOnce(ctx, store, appLogger, time.Now)
	ticker := time.NewTicker(PruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruneOnce(ctx, store, appLogger, time.Now)
		}
	}
}

// pruneOnce は、1 回だけ削除する。失敗しても止まらず、次の間隔でやり直す。
func pruneOnce(ctx context.Context, store TaskPruneStore, appLogger *slog.Logger, now func() time.Time) {
	// 保存期間と上限を過ぎたものを消す。
	if pruneError := store.Prune(ctx, now().Add(-TaskRetention), MaxTasksPerProject); pruneError != nil && ctx.Err() == nil {
		appLogger.Warn("古いタスクを削除できませんでした。次の間隔でやり直します", "error", pruneError)
	}
}
