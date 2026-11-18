package accessstats

import (
	"testing"
	"time"
)

// sampleLine は、プラグインの出力の例(試験用の 1 行)。
const sampleLine = `2026-11-18T09:50:00Z LAUNCHS_STATS {"bytes":10,"clients":[{"key":"1.1.1.1","count":3},{"key":"2.2.2.2","count":1}],"count":4,"ingressId":"ingress-a","intervalSec":60,"latency":[4,0,0,0,0,0,0,0,0,0,0],"methods":{"GET":3,"POST":1},"paths":[{"key":"/b","count":1},{"key":"/a","count":3}],"start":"2026-11-18T09:50:00Z","status":{"1xx":0,"2xx":4,"3xx":0,"4xx":0,"5xx":0},"sumMs":8.5,"uniqueClients":2}`

// TestParseLine は、行の読み取り(接頭辞つき、壊れた行、クライアントを捨てる設定)を確認する。
func TestParseLine(testingT *testing.T) {
	record, parsed := ParseLine(sampleLine, true)
	if !parsed || record.IngressID != "ingress-a" || record.Count != 4 || record.Start != time.Date(2026, 11, 18, 9, 50, 0, 0, time.UTC) {
		testingT.Fatalf("読み取れるはず: %v %+v", parsed, record)
	}
	if record.Paths[0].Key != "/a" || record.Clients[0].Key != "1.1.1.1" || record.UniqueClients != 2 {
		testingT.Errorf("多い順に並ぶはず: %+v", record)
	}
	noClients, _ := ParseLine(sampleLine, false)
	if len(noClients.Clients) != 0 || noClients.UniqueClients != 0 {
		testingT.Errorf("クライアントは捨てるはず: %+v", noClients)
	}
	for name, text := range map[string]string{
		"印が無い":        `{"count":1}`,
		"JSON が壊れている": `LAUNCHS_STATS {oops`,
		"件数が 0":       `LAUNCHS_STATS {"ingressId":"i","count":0,"start":"2026-11-18T09:50:00Z","latency":[0,0,0,0,0,0,0,0,0,0,0]}`,
		"度数の数が違う":     `LAUNCHS_STATS {"ingressId":"i","count":1,"start":"2026-11-18T09:50:00Z","latency":[1]}`,
		"時刻が壊れている":    `LAUNCHS_STATS {"ingressId":"i","count":1,"start":"x","latency":[0,0,0,0,0,0,0,0,0,0,0]}`,
	} {
		if _, ok := ParseLine(text, true); ok {
			testingT.Errorf("%s: 読み取れないはず", name)
		}
	}
}

// TestMergeAndPercentile は、足し合わせと、分位の見積もりを確認する。
func TestMergeAndPercentile(testingT *testing.T) {
	first, _ := ParseLine(sampleLine, true)
	second := first
	second.Latency = []int64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 4}
	merged := Merge([]Record{first, second}, 1)
	if merged.Count != 8 || merged.Status["2xx"] != 8 || merged.Methods["GET"] != 6 || merged.Bytes != 20 {
		testingT.Errorf("足し合わせが違う: %+v", merged)
	}
	if len(merged.Paths) != 1 || merged.Paths[0].Key != "/a" || merged.Paths[0].Count != 6 {
		testingT.Errorf("上位は 1 つで、足し合わせた数のはず: %+v", merged.Paths)
	}
	if p50 := Percentile(merged.Latency, 0.5); p50 != 5 {
		testingT.Errorf("p50 は 5ms のはず: %v", p50)
	}
	if p99 := Percentile(merged.Latency, 0.99); p99 != 5000 {
		testingT.Errorf("p99 は最後の上限 5000ms のはず: %v", p99)
	}
	if Percentile(make([]int64, 11), 0.5) != 0 {
		testingT.Error("度数が無ければ 0 のはず")
	}
}

// TestBuildReport は、区間への振り分け、空の区間の穴埋め、期間の名前の検索を確認する。
func TestBuildReport(testingT *testing.T) {
	spec, found := FindRange("1h")
	if !found {
		testingT.Fatal("1h があるはず")
	}
	if _, other := FindRange("2h"); other {
		testingT.Error("2h は無いはず")
	}
	now := time.Date(2026, 11, 18, 10, 30, 30, 0, time.UTC)
	record, _ := ParseLine(sampleLine, true)
	record.Start = time.Date(2026, 11, 18, 10, 10, 20, 0, time.UTC)
	report := BuildReport(spec, now, []Record{record})
	if report.Range != "1h" || report.BucketSeconds != 60 || report.Totals.Count != 4 || report.Totals.Status["2xx"] != 4 || report.Totals.Status["5xx"] != 0 {
		testingT.Fatalf("合計が違う: %+v", report.Totals)
	}
	// 1 時間(60 区間)+ 今の区間。
	if len(report.Series) != 61 || !report.Series[0].Start.Equal(time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC)) {
		testingT.Fatalf("区間の数と始まりが違う: %d %v", len(report.Series), report.Series[0].Start)
	}
	filled := 0
	for _, point := range report.Series {
		if point.Count > 0 {
			filled++
			if !point.Start.Equal(time.Date(2026, 11, 18, 10, 10, 0, 0, time.UTC)) || point.AvgMs != 2.125 {
				testingT.Errorf("区間が違う: %+v", point)
			}
		}
	}
	if filled != 1 || report.Paths[0].Key != "/a" || report.Clients[0].Key != "1.1.1.1" {
		testingT.Errorf("1 区間だけが埋まり、上位が入るはず: %d %+v", filled, report)
	}
}
