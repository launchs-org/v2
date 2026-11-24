// Package accessstats は、ingress のアクセスの集計(Traefik のプラグイン launchs-stats の出力)の型と、読み取り・足し合わせ・集計を持つ。
// 設計は dev-docs/ingress-plugins.md。
package accessstats

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const (
	// LinePrefix は、プラグインが出す行の頭の印。
	LinePrefix = "LAUNCHS_STATS "

	// MinuteTopLimit は、1 分ごとの行に残す、パスとクライアントの数。
	MinuteTopLimit = 20

	// HourTopLimit は、1 時間ごとの行に残す、パスとクライアントの数。
	HourTopLimit = 100
)

// LatencyBoundsMs は、応答時間の区切り(ミリ秒)。度数の配列は、この数 + 1 個(最後は、それ以上)。プラグインと同じ値。
var LatencyBoundsMs = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}

// StatusClasses は、ステータスの分類の名前(並び順)。
var StatusClasses = []string{"1xx", "2xx", "3xx", "4xx", "5xx"}

// Counted は、キーと、その数(上位の集計の 1 行)。
type Counted struct {
	// Key は、パスやクライアントの IP。
	Key string `json:"key"`

	// Count は、数。
	Count int64 `json:"count"`
}

const (
	// MaxRequestEntries は、1 行に入れてよい、1 リクエストごとの記録の数の上限(プラグインと同じ)。
	MaxRequestEntries = 200

	// MaxURLLength は、保存する URL の長さの上限(バイト)。
	MaxURLLength = 1000
)

// RequestEntry は、1 リクエストの記録(アクセスログの 1 行)。
type RequestEntry struct {
	// T は、リクエストを受けた時刻(Unix のミリ秒)。
	T int64 `json:"t"`

	// Method は、メソッド。
	Method string `json:"m"`

	// URL は、パスとクエリ(そのまま)。
	URL string `json:"u"`

	// Status は、ステータス(WebSocket など、記録できないものは 0)。
	Status int `json:"s"`

	// DurationMs は、応答にかかった時間(ミリ秒)。
	DurationMs float64 `json:"d"`

	// ClientIP は、クライアントの IP(分からなければ空)。
	ClientIP string `json:"ip"`

	// Bytes は、応答の大きさ(バイト)。
	Bytes int64 `json:"b"`
}

// Record は、1 つの ingress の、ある期間の集計。
type Record struct {
	// IngressID は、ingress の ID。
	IngressID string `json:"ingressId"`

	// Replica は、出力した Traefik の Pod の名前(1 時間ごとにまとめた行では空)。
	Replica string `json:"-"`

	// Start は、集計の始まりの時刻。
	Start time.Time `json:"-"`

	// IntervalSec は、集計の長さ(秒)。
	IntervalSec int `json:"intervalSec"`

	// Count は、リクエストの数。
	Count int64 `json:"count"`

	// Status は、ステータスの分類(1xx〜5xx)ごとの数。
	Status map[string]int64 `json:"status"`

	// Methods は、メソッドごとの数。
	Methods map[string]int64 `json:"methods"`

	// Bytes は、応答の大きさの合計。
	Bytes int64 `json:"bytes"`

	// Latency は、応答時間の度数(LatencyBoundsMs + 最後の 1 つ)。
	Latency []int64 `json:"latency"`

	// SumMs は、応答時間の合計(ミリ秒)。
	SumMs float64 `json:"sumMs"`

	// Paths は、よく使われたパス(多い順)。
	Paths []Counted `json:"paths"`

	// Clients は、よく来たクライアント(多い順)。
	Clients []Counted `json:"clients"`

	// UniqueClients は、重複を除いたクライアントの数(合算すると、重複して数えうる)。
	UniqueClients int `json:"uniqueClients"`

	// Requests は、この期間の、1 リクエストごとの記録(上限まで。集計の行には保存しない)。
	Requests []RequestEntry `json:"requests"`

	// RequestsDropped は、上限を超えて、記録しなかったリクエストの数。
	RequestsDropped int64 `json:"requestsDropped"`
}

// wireLine は、プラグインが出す JSON の形(start が文字列)。
type wireLine struct {
	Record
	Start string `json:"start"`
}

// ParseLine は、ログの 1 行から、集計を読み取る。印の無い行・壊れた行は、読み取れない。
//
// 引数:
//   - text: ログの 1 行(前に Traefik の接頭辞などがあっても、印から後ろを読む)。
//   - keepClients: false なら、クライアントの IP を捨てる。
//
// 戻り値:
//   - 読み取った集計と、読み取れたか。
func ParseLine(text string, keepClients bool) (Record, bool) {
	// 印を探す。
	index := strings.Index(text, LinePrefix)
	if index < 0 {
		return Record{}, false
	}
	// JSON として読む。
	var line wireLine
	if decodeError := json.Unmarshal([]byte(text[index+len(LinePrefix):]), &line); decodeError != nil {
		return Record{}, false
	}
	// 始まりの時刻を読む。必須の値が無ければ、捨てる。
	start, timeError := time.Parse(time.RFC3339, line.Start)
	if timeError != nil || line.IngressID == "" || line.Count <= 0 {
		return Record{}, false
	}
	record := line.Record
	record.Start = start.UTC()
	// 度数の数が合わなければ、捨てる(区切りが違う版からの出力)。
	if len(record.Latency) != len(LatencyBoundsMs)+1 {
		return Record{}, false
	}
	// 空の入れ物を埋める。
	if record.Status == nil {
		record.Status = map[string]int64{}
	}
	if record.Methods == nil {
		record.Methods = map[string]int64{}
	}
	// 1 リクエストごとの記録を、上限にそろえる(URL を切り、時刻の無いものは捨てる)。
	if len(record.Requests) > MaxRequestEntries {
		record.Requests = record.Requests[:MaxRequestEntries]
	}
	entries := make([]RequestEntry, 0, len(record.Requests))
	for _, entry := range record.Requests {
		if entry.T <= 0 || entry.Method == "" {
			continue
		}
		if len(entry.URL) > MaxURLLength {
			entry.URL = entry.URL[:MaxURLLength]
		}
		entries = append(entries, entry)
	}
	record.Requests = entries
	// クライアントを保存しない設定なら、捨てる(記録の IP も)。
	if !keepClients {
		record.Clients = nil
		record.UniqueClients = 0
		for index := range record.Requests {
			record.Requests[index].ClientIP = ""
		}
	}
	// 上位の数を、保存する数にそろえる。
	record.Paths = topOf(record.Paths, MinuteTopLimit)
	record.Clients = topOf(record.Clients, MinuteTopLimit)
	return record, true
}

// topOf は、多い順に並べて、上位 limit 個にする(元は変えない)。
func topOf(items []Counted, limit int) []Counted {
	// 空は、空の配列(JSON で null にしない)。
	result := make([]Counted, len(items))
	copy(result, items)
	sort.Slice(result, func(left, right int) bool {
		if result[left].Count != result[right].Count {
			return result[left].Count > result[right].Count
		}
		return result[left].Key < result[right].Key
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

// Merge は、複数の集計を足し合わせる。上位の一覧は、キーごとに足して、多い順に top 個にする。
// 重複を除いたクライアントの数は、足し合わせた値の最大(下限の目安)にする。
//
// 引数:
//   - records: 足し合わせる集計。
//   - top: 上位として残す数。
//
// 戻り値:
//   - 足し合わせた集計(IngressID と Start は、呼び出し側が決める)。
func Merge(records []Record, top int) Record {
	// 空の集計から始める。
	merged := Record{Status: map[string]int64{}, Methods: map[string]int64{}, Latency: make([]int64, len(LatencyBoundsMs)+1)}
	paths := map[string]int64{}
	clients := map[string]int64{}
	for _, record := range records {
		merged.Count += record.Count
		merged.Bytes += record.Bytes
		merged.SumMs += record.SumMs
		for name, count := range record.Status {
			merged.Status[name] += count
		}
		for name, count := range record.Methods {
			merged.Methods[name] += count
		}
		for index := 0; index < len(record.Latency) && index < len(merged.Latency); index++ {
			merged.Latency[index] += record.Latency[index]
		}
		for _, item := range record.Paths {
			paths[item.Key] += item.Count
		}
		for _, item := range record.Clients {
			clients[item.Key] += item.Count
		}
		if record.UniqueClients > merged.UniqueClients {
			merged.UniqueClients = record.UniqueClients
		}
	}
	merged.Paths = topOf(mapToCounted(paths), top)
	merged.Clients = topOf(mapToCounted(clients), top)
	return merged
}

// mapToCounted は、キーごとの数の表を、一覧にする。
func mapToCounted(values map[string]int64) []Counted {
	// 1 つずつ、一覧に移す。
	result := make([]Counted, 0, len(values))
	for key, count := range values {
		result = append(result, Counted{Key: key, Count: count})
	}
	return result
}

// Percentile は、応答時間の度数から、q 分位(0〜1)の値を、ミリ秒で見積もる(その区切りの上限を返す。最後の区切りは、最後の上限)。
//
// 引数:
//   - latency: 応答時間の度数。
//   - quantile: 0 より大きく 1 以下。
//
// 戻り値:
//   - 見積もり(ミリ秒)。度数が無ければ 0。
func Percentile(latency []int64, quantile float64) float64 {
	// 全体の数を数える。
	var total int64
	for _, count := range latency {
		total += count
	}
	if total == 0 {
		return 0
	}
	// 累積が、目標の順位に届く区切りを探す。
	target := int64(float64(total)*quantile + 0.999999)
	var running int64
	for index, count := range latency {
		running += count
		if running >= target {
			if index >= len(LatencyBoundsMs) {
				return LatencyBoundsMs[len(LatencyBoundsMs)-1]
			}
			return LatencyBoundsMs[index]
		}
	}
	return LatencyBoundsMs[len(LatencyBoundsMs)-1]
}
