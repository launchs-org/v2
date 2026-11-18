package repository

import (
	"context"
	"testing"
	"time"

	"launchs/shared/accessstats"
	"launchs/shared/database/testdatabase"
)

// newTestRecord は、試験用の集計を作る。
func newTestRecord(start time.Time, count int64) accessstats.Record {
	// 少しの内容を持つ集計。
	return accessstats.Record{
		IngressID: "ingress-a", Replica: "traefik-0", Start: start, IntervalSec: 60, Count: count,
		Status: map[string]int64{"2xx": count}, Methods: map[string]int64{"GET": count}, Bytes: 10 * count,
		Latency: make([]int64, 11), SumMs: 1, Paths: []accessstats.Counted{{Key: "/a", Count: count}}, Clients: []accessstats.Counted{{Key: "1.1.1.1", Count: count}}, UniqueClients: 1,
	}
}

// TestAccessStatsRepository は、保存(上書き)、1 時間へのまとめ、読み出し、削除を確認する。
func TestAccessStatsRepository(testingT *testing.T) {
	ctx := context.Background()
	repo := NewAccessStatsRepository(testdatabase.New(testingT))
	base := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)

	// 同じ行を 2 回保存しても、1 行のまま(上書き)。
	first := newTestRecord(base.Add(time.Minute), 3)
	if err := repo.UpsertMinute(ctx, first); err != nil {
		testingT.Fatal(err)
	}
	first.Count = 5
	if err := repo.UpsertMinute(ctx, first); err != nil {
		testingT.Fatal(err)
	}
	if err := repo.UpsertMinute(ctx, newTestRecord(base.Add(2*time.Minute), 7)); err != nil {
		testingT.Fatal(err)
	}
	minutes, queryError := repo.Query(ctx, false, "ingress-a", base, base.Add(time.Hour))
	if queryError != nil || len(minutes) != 2 || minutes[0].Count != 5 || minutes[0].Paths[0].Key != "/a" {
		testingT.Fatalf("上書きされて 2 行のはず: %v %+v", queryError, minutes)
	}
	// 1 時間にまとめる。
	if err := repo.RollupHours(ctx, base); err != nil {
		testingT.Fatal(err)
	}
	hours, hourError := repo.Query(ctx, true, "ingress-a", base, base.Add(time.Hour))
	if hourError != nil || len(hours) != 1 || hours[0].Count != 12 || hours[0].Paths[0].Count != 10 || hours[0].IntervalSec != 3600 {
		testingT.Fatalf("12 件の 1 行になるはず: %v %+v", hourError, hours)
	}
	// 別の ingress は、混ざらない。
	if other, _ := repo.Query(ctx, false, "ingress-b", base, base.Add(time.Hour)); len(other) != 0 {
		testingT.Errorf("別の ingress は空のはず: %+v", other)
	}
	// 古いものを消す。
	if err := repo.Prune(ctx, base.Add(90*time.Minute), base.Add(90*time.Minute)); err != nil {
		testingT.Fatal(err)
	}
	if left, _ := repo.Query(ctx, false, "ingress-a", base, base.Add(time.Hour)); len(left) != 0 {
		testingT.Errorf("消えているはず: %+v", left)
	}
}
