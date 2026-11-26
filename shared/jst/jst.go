// Package jst は、API に出す日時を、日本標準時(+09:00)の文字列にする。
//
// 共通ルールで、日時は JST の RFC 3339 形式(2026-09-16T09:00:00+09:00)と決まっている。
// DB には timestamptz で持ち、Go では time.Time で扱い、API に出すときだけ、ここで変換する。
package jst

import "time"

// location は、日本標準時のタイムゾーン。
//
// 日本は夏時間が無いので、FixedZone にして、タイムゾーンのデータが無い環境でも動くようにする。
var location = time.FixedZone("JST", 9*60*60)

// Format は、日時を、JST の RFC 3339 形式の文字列にする。
//
// 引数:
//   - moment: 変換する日時(どのタイムゾーンでもよい)。
//
// 戻り値:
//   - 例: 2026-09-16T09:00:00+09:00。
func Format(moment time.Time) string {
	// JST に変えてから、RFC 3339 の形式に整形する。
	return moment.In(location).Format(time.RFC3339)
}

// FormatMillis は、時刻を、JST のミリ秒まで付けた RFC3339 の文字列にする(例: 2026-11-15T20:00:01.123+09:00)。ログの行の時刻に使う。
//
// 引数:
//   - moment: 時刻。
//
// 戻り値:
//   - JST のミリ秒つきの文字列。
func FormatMillis(moment time.Time) string {
	// JST に直して、ミリ秒まで書く(ミリ秒より細かい部分は、切り捨てる)。
	return moment.In(location).Format("2006-01-02T15:04:05.000Z07:00")
}

// DayStart は、時刻が属する日本時間の日の、始まり(0 時)を返す。
//
// 引数:
//   - moment: 時刻。
//
// 戻り値:
//   - その日の 0 時(JST)。
func DayStart(moment time.Time) time.Time {
	// JST に直して、年月日だけを残す。
	local := moment.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

// DayString は、時刻が属する日本時間の日を、2026-11-26 の形で返す(ファイル名に使う)。
//
// 引数:
//   - moment: 時刻。
//
// 戻り値:
//   - YYYY-MM-DD。
func DayString(moment time.Time) string {
	// JST に直して、日付だけを書く。
	return moment.In(location).Format("2006-01-02")
}
