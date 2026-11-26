// Package exportlog は、アクセスログを、外に書き出すときの、1 行の形を決める(webhook と Google Drive で同じ)。
package exportlog

import (
	"bytes"
	"encoding/json"
	"time"
)

// Line は、書き出すアクセスログの 1 行。
type Line struct {
	// ID は、行の ID(ingress_access_logs.id)。受け取る側が、重複を除くのに使う。
	ID int64 `json:"id"`

	// IngressID は、ingress の ID。
	IngressID string `json:"ingressId"`

	// Host は、ingress のホスト名。
	Host string `json:"host"`

	// Time は、リクエストを受けた時刻(UTC、ミリ秒、RFC 3339)。
	Time string `json:"time"`

	// Method は、メソッド。
	Method string `json:"method"`

	// URL は、パスとクエリ。
	URL string `json:"url"`

	// Status は、ステータス(記録できないものは 0)。
	Status int `json:"status"`

	// DurationMs は、応答にかかった時間(ミリ秒)。
	DurationMs float64 `json:"durationMs"`

	// ClientIP は、クライアントの IP(保存しない設定では空)。
	ClientIP string `json:"clientIp"`

	// Bytes は、応答の大きさ(バイト)。
	Bytes int64 `json:"bytes"`
}

// FormatTime は、リクエストを受けた時刻を、行に入れる形(UTC、ミリ秒の RFC 3339)にする。
//
// 引数:
//   - moment: 時刻。
//
// 戻り値:
//   - 例: 2026-11-26T01:42:04.123Z。
func FormatTime(moment time.Time) string {
	// UTC にして、ミリ秒まで書く。
	return moment.UTC().Format("2006-01-02T15:04:05.000Z")
}

// EncodeNDJSON は、行を、NDJSON(1 行に JSON 1 つ、改行で区切る)にする。
//
// 引数:
//   - lines: 行。
//
// 戻り値:
//   - NDJSON のバイト列(各行の最後に改行がある。行が無ければ空)。
func EncodeNDJSON(lines []Line) []byte {
	// 1 行ずつ、JSON にして、改行を付ける。
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	// URL に含まれる & などを、& にしない(読みやすさのため)。
	encoder.SetEscapeHTML(false)
	for _, line := range lines {
		// 構造体なので、失敗しない。Encode は、最後に改行を付ける。
		_ = encoder.Encode(line)
	}
	return buffer.Bytes()
}
