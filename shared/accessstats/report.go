package accessstats

import (
	"sort"
	"time"
)

// RangeSpec は、API で指定できる期間の定義。
type RangeSpec struct {
	// Name は、期間の名前(1h、24h、7d、30d)。
	Name string

	// Length は、期間の長さ。
	Length time.Duration

	// Bucket は、グラフの 1 つの区間の長さ。
	Bucket time.Duration

	// Hours は、1 時間ごとの表から読むか(false なら 1 分ごとの表)。
	Hours bool
}

// Ranges は、指定できる期間の一覧。
var Ranges = []RangeSpec{
	{Name: "1h", Length: time.Hour, Bucket: time.Minute, Hours: false},
	{Name: "24h", Length: 24 * time.Hour, Bucket: 15 * time.Minute, Hours: false},
	{Name: "7d", Length: 7 * 24 * time.Hour, Bucket: time.Hour, Hours: true},
	{Name: "30d", Length: 30 * 24 * time.Hour, Bucket: 24 * time.Hour, Hours: true},
}

// FindRange は、名前から期間の定義を探す。
//
// 引数:
//   - name: 期間の名前。
//
// 戻り値:
//   - 定義と、見つかったか。
func FindRange(name string) (RangeSpec, bool) {
	// 1 つずつ比べる。
	for _, spec := range Ranges {
		if spec.Name == name {
			return spec, true
		}
	}
	return RangeSpec{}, false
}

// Summary は、ある期間の合計の要約。
type Summary struct {
	// Count は、リクエストの数。
	Count int64 `json:"count"`

	// Status は、ステータスの分類(1xx〜5xx)ごとの数。常に 5 つとも入る。
	Status map[string]int64 `json:"status"`

	// Methods は、メソッドごとの数。
	Methods map[string]int64 `json:"methods"`

	// Bytes は、応答の大きさの合計。
	Bytes int64 `json:"bytes"`

	// AvgMs は、応答時間の平均(ミリ秒)。
	AvgMs float64 `json:"avgMs"`

	// P50Ms、P95Ms、P99Ms は、応答時間の分位の見積もり(ミリ秒。区切りの上限)。
	P50Ms float64 `json:"p50Ms"`
	P95Ms float64 `json:"p95Ms"`
	P99Ms float64 `json:"p99Ms"`
}

// Point は、グラフの 1 つの区間。
type Point struct {
	// Start は、区間の始まり。
	Start time.Time `json:"start"`

	// Summary は、この区間の要約。
	Summary
}

// Report は、API が返す、ある期間のアクセスの集計。
type Report struct {
	// Range は、期間の名前。
	Range string `json:"range"`

	// From と To は、期間の始まりと終わり。
	From time.Time `json:"from"`
	To   time.Time `json:"to"`

	// BucketSeconds は、グラフの 1 区間の長さ(秒)。
	BucketSeconds int `json:"bucketSeconds"`

	// Totals は、期間全体の要約。
	Totals Summary `json:"totals"`

	// Series は、区間ごとの要約(件数が 0 の区間も含む。時刻の昇順)。
	Series []Point `json:"series"`

	// Paths は、よく使われたパス(上位 20)。
	Paths []Counted `json:"paths"`

	// Clients は、よく来たクライアント(上位 20。保存しない設定では空)。
	Clients []Counted `json:"clients"`

	// UniqueClients は、期間の中の 1 区間での、重複を除いたクライアント数の最大(下限の目安)。
	UniqueClients int `json:"uniqueClients"`
}

// summarize は、足し合わせた集計から、要約を作る。
func summarize(merged Record) Summary {
	// ステータスは、5 つとも入れる。
	status := map[string]int64{}
	for _, class := range StatusClasses {
		status[class] = merged.Status[class]
	}
	summary := Summary{Count: merged.Count, Status: status, Methods: merged.Methods, Bytes: merged.Bytes,
		P50Ms: Percentile(merged.Latency, 0.50), P95Ms: Percentile(merged.Latency, 0.95), P99Ms: Percentile(merged.Latency, 0.99)}
	if merged.Count > 0 {
		summary.AvgMs = merged.SumMs / float64(merged.Count)
	}
	return summary
}

// BuildReport は、集計の行から、期間のレポートを作る。
//
// 引数:
//   - spec: 期間の定義。
//   - now: 現在の時刻(期間の終わり)。
//   - records: 期間の中の行(順不同でよい)。
//
// 戻り値:
//   - レポート。
func BuildReport(spec RangeSpec, now time.Time, records []Record) Report {
	// 期間の始まりを、区間の区切りに合わせる。
	end := now.UTC()
	from := end.Add(-spec.Length).Truncate(spec.Bucket)
	// 区間ごとに行を分ける。
	byBucket := map[time.Time][]Record{}
	for _, record := range records {
		bucket := record.Start.UTC().Truncate(spec.Bucket)
		byBucket[bucket] = append(byBucket[bucket], record)
	}
	// 区間を、空の区間も含めて、並べる。
	var series []Point
	for start := from; !start.After(end); start = start.Add(spec.Bucket) {
		series = append(series, Point{Start: start, Summary: summarize(Merge(byBucket[start], 0))})
	}
	// 期間全体の要約と、上位の一覧を作る。
	all := Merge(records, 20)
	sort.SliceStable(series, func(left, right int) bool { return series[left].Start.Before(series[right].Start) })
	return Report{Range: spec.Name, From: from, To: end, BucketSeconds: int(spec.Bucket / time.Second), Totals: summarize(all), Series: series,
		Paths: all.Paths, Clients: all.Clients, UniqueClients: all.UniqueClients}
}
