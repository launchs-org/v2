// Package retention は、アクセスの集計を 1 時間ごとにまとめ、保存期間を過ぎたものを削除する(リーダーの accessstats が動かす)。
package retention

import (
	"context"
	"log/slog"
	"time"
)

// Interval は、まとめと削除をする間隔。
const Interval = time.Minute

// rollupWindow は、まとめ直す期間(今から、さかのぼる長さ。取りこぼしと遅れて来た行を、拾い直す)。
const rollupWindow = 2 * time.Hour

// Store は、まとめと削除に使う、DB の操作。実体は repository.AccessStatsRepository。
type Store interface {
	// RollupHours は、1 分ごとの集計を、1 時間ごとにまとめる(from 以降の分)。
	RollupHours(requestContext context.Context, from time.Time) error

	// Prune は、保存期間を過ぎた集計を削除する。
	Prune(requestContext context.Context, minutesCutoff time.Time, hoursCutoff time.Time) error
}

// Roller は、アクセスの集計を、定期的にまとめて、古いものを削除する。
type Roller struct {
	store           Store
	minuteRetention time.Duration
	hourRetention   time.Duration
	interval        time.Duration
	appLogger       *slog.Logger
	// now は、現在の時刻を返す関数(試験で差し替える)。
	now func() time.Time
}

// NewRoller は、Roller を作る。
//
// 引数:
//   - store: DB の操作。
//   - minuteRetention: 1 分ごとの集計を残す期間。
//   - hourRetention: 1 時間ごとの集計を残す期間。
//   - interval: 実行の間隔。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した Roller。
func NewRoller(store Store, minuteRetention time.Duration, hourRetention time.Duration, interval time.Duration, appLogger *slog.Logger) *Roller {
	// 値と、実際の時計を持たせて返す。
	return &Roller{store: store, minuteRetention: minuteRetention, hourRetention: hourRetention, interval: interval, appLogger: appLogger, now: time.Now}
}

// Run は、始めに 1 回、そのあとは interval ごとに、まとめと削除をする。ctx が終わると戻る。
//
// 引数:
//   - ctx: 止める合図。
func (roller *Roller) Run(ctx context.Context) {
	// 始めに 1 回。
	roller.runOnce(ctx)
	ticker := time.NewTicker(roller.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			roller.runOnce(ctx)
		}
	}
}

// runOnce は、1 回だけ、まとめて、削除する。失敗しても止まらず、次の間隔でやり直す。
func (roller *Roller) runOnce(ctx context.Context) {
	// 先にまとめて(古い行を消す前に、1 時間の行に残す)、そのあと消す。
	current := roller.now()
	if rollupError := roller.store.RollupHours(ctx, current.Add(-rollupWindow)); rollupError != nil {
		if ctx.Err() == nil {
			roller.appLogger.Warn("アクセスの集計を 1 時間ごとにまとめられませんでした", "error", rollupError)
		}
		return
	}
	if pruneError := roller.store.Prune(ctx, current.Add(-roller.minuteRetention), current.Add(-roller.hourRetention)); pruneError != nil && ctx.Err() == nil {
		roller.appLogger.Warn("古いアクセスの集計を削除できませんでした", "error", pruneError)
	}
}
