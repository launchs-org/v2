package retention

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

// fakeStore は、呼ばれた引数を覚える。
type fakeStore struct {
	rollupFrom                 time.Time
	minutesCutoff, hoursCutoff time.Time
	calls                      []string
	logCutoff                  time.Time
	logMaxRows                 int
}

// RollupHours は、引数を覚える。
func (store *fakeStore) RollupHours(_ context.Context, from time.Time) error {
	store.rollupFrom = from
	store.calls = append(store.calls, "rollup")
	return nil
}

// Prune は、引数を覚える。
func (store *fakeStore) Prune(_ context.Context, minutesCutoff time.Time, hoursCutoff time.Time) error {
	store.minutesCutoff, store.hoursCutoff = minutesCutoff, hoursCutoff
	store.calls = append(store.calls, "prune")
	return nil
}

// PruneLogs は、引数を覚える。
func (store *fakeStore) PruneLogs(_ context.Context, cutoff time.Time, maxRows int) error {
	store.logCutoff, store.logMaxRows = cutoff, maxRows
	store.calls = append(store.calls, "prunelogs")
	return nil
}

// TestRollerRunOnce は、まとめてから削除する順番と、期間の計算を確認する。
func TestRollerRunOnce(testingT *testing.T) {
	store := &fakeStore{}
	roller := NewRoller(store, 24*time.Hour, 720*time.Hour, 12*time.Hour, 5000, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	fixed := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	roller.now = func() time.Time { return fixed }
	roller.runOnce(context.Background())
	if len(store.calls) != 3 || store.calls[0] != "rollup" || store.calls[1] != "prune" || store.calls[2] != "prunelogs" {
		testingT.Fatalf("まとめてから削除するはず: %v", store.calls)
	}
	if !store.rollupFrom.Equal(fixed.Add(-2*time.Hour)) || !store.minutesCutoff.Equal(fixed.Add(-24*time.Hour)) || !store.hoursCutoff.Equal(fixed.Add(-720*time.Hour)) || !store.logCutoff.Equal(fixed.Add(-12*time.Hour)) || store.logMaxRows != 5000 {
		testingT.Errorf("期間が違う: %v %v %v", store.rollupFrom, store.minutesCutoff, store.hoursCutoff)
	}
}
