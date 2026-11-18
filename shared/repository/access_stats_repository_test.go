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

// TestAccessLogRepository は、アクセスログの保存(二重にならない)、新しい順、絞り込み、ページング、削除を確認する。
func TestAccessLogRepository(testingT *testing.T) {
	ctx := context.Background()
	repo := NewAccessStatsRepository(testdatabase.New(testingT))
	start := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	record := accessstats.Record{IngressID: "ingress-a", Replica: "traefik-0", Start: start}
	for index, entry := range []accessstats.RequestEntry{
		{T: start.UnixMilli() + 1, Method: "GET", URL: "/a?token=secret&x=1", Status: 200, DurationMs: 1, ClientIP: "1.1.1.1", Bytes: 5},
		{T: start.UnixMilli() + 2, Method: "POST", URL: "/b_c%d", Status: 404, DurationMs: 2, ClientIP: "2.2.2.2", Bytes: 6},
		{T: start.UnixMilli() + 3, Method: "GET", URL: "/c", Status: 503, DurationMs: 3, ClientIP: "1.1.1.1", Bytes: 7},
	} {
		_ = index
		record.Requests = append(record.Requests, entry)
	}
	// 同じ出力を 2 回保存しても、3 行のまま。
	for range 2 {
		if err := repo.InsertLogs(ctx, record); err != nil {
			testingT.Fatal(err)
		}
	}
	all, next, err := repo.ListLogs(ctx, "ingress-a", AccessLogFilter{}, 10, nil)
	if err != nil || len(all) != 3 || next != nil || all[0].URL != "/c" || all[2].URL != "/a?token=secret&x=1" {
		testingT.Fatalf("新しい順に 3 行のはず: %v %+v", err, all)
	}
	if other, _, _ := repo.ListLogs(ctx, "ingress-b", AccessLogFilter{}, 10, nil); len(other) != 0 {
		testingT.Error("別の ingress は混ざらないはず")
	}
	// ページング。
	first, cursor, _ := repo.ListLogs(ctx, "ingress-a", AccessLogFilter{}, 2, nil)
	rest, last, _ := repo.ListLogs(ctx, "ingress-a", AccessLogFilter{}, 2, cursor)
	if len(first) != 2 || cursor == nil || len(rest) != 1 || last != nil || rest[0].URL != "/a?token=secret&x=1" {
		testingT.Errorf("2 ページに分かれるはず: %+v %+v", first, rest)
	}
	// 絞り込み。
	for name, testCase := range map[string]struct {
		filter AccessLogFilter
		want   int
	}{
		"5xx":           {AccessLogFilter{Status: "5xx"}, 1},
		"404":           {AccessLogFilter{Status: "404"}, 1},
		"メソッド":          {AccessLogFilter{Method: "post"}, 1},
		"IP":            {AccessLogFilter{ClientIP: "1.1.1.1"}, 2},
		"クエリ文字列":        {AccessLogFilter{Query: "TOKEN=secret"}, 1},
		"_ はそのまま":       {AccessLogFilter{Query: "b_c"}, 1},
		"% はそのまま":       {AccessLogFilter{Query: "%d"}, 1},
		"_ は任意の1文字ではない": {AccessLogFilter{Query: "b_"}, 1},
		"期間(From 以降)":   {AccessLogFilter{From: time.UnixMilli(start.UnixMilli() + 2)}, 2},
		"期間(To より前)":    {AccessLogFilter{To: time.UnixMilli(start.UnixMilli() + 2)}, 1},
		"組み合わせ":         {AccessLogFilter{ClientIP: "1.1.1.1", Status: "2xx"}, 1},
	} {
		got, _, filterError := repo.ListLogs(ctx, "ingress-a", testCase.filter, 10, nil)
		if filterError != nil || len(got) != testCase.want {
			testingT.Errorf("%s: %d 件のはず: %v %+v", name, testCase.want, filterError, got)
		}
	}
	if _, _, badError := repo.ListLogs(ctx, "ingress-a", AccessLogFilter{Status: "abc"}, 10, nil); badError == nil {
		testingT.Error("不正な status は拒否するはず")
	}
	// 行数の上限で、古いものから消える。
	if pruneError := repo.PruneLogs(ctx, start.Add(-time.Hour), 2); pruneError != nil {
		testingT.Fatal(pruneError)
	}
	left, _, _ := repo.ListLogs(ctx, "ingress-a", AccessLogFilter{}, 10, nil)
	if len(left) != 2 || left[1].URL != "/b_c%d" {
		testingT.Errorf("新しい 2 行が残るはず: %+v", left)
	}
	// 保存期間で、全て消える。
	_ = repo.PruneLogs(ctx, time.Now().Add(time.Hour), 100)
	if left, _, _ = repo.ListLogs(ctx, "ingress-a", AccessLogFilter{}, 10, nil); len(left) != 0 {
		testingT.Errorf("全て消えるはず: %+v", left)
	}
}
