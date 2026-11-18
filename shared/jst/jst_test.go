package jst

import (
	"testing"
	"time"
)

// TestFormat は、どのタイムゾーンの日時も、+09:00 の RFC 3339 の文字列になることを確認する。
func TestFormat(testingT *testing.T) {
	// UTC、別のタイムゾーン、すでに JST の場合。
	testCases := []struct {
		name     string
		moment   time.Time
		expected string
	}{
		{name: "UTC の日時は、9時間進める", moment: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC), expected: "2026-09-16T09:00:00+09:00"},
		{name: "日付をまたぐ場合は、日付も進む", moment: time.Date(2026, 9, 16, 15, 30, 0, 0, time.UTC), expected: "2026-09-17T00:30:00+09:00"},
		{name: "別のタイムゾーンでも、同じ瞬間を JST で表す", moment: time.Date(2026, 9, 16, 5, 0, 0, 0, time.FixedZone("EST", -5*3600)), expected: "2026-09-16T19:00:00+09:00"},
		{name: "すでに JST ならそのまま", moment: time.Date(2026, 9, 16, 9, 0, 0, 0, location), expected: "2026-09-16T09:00:00+09:00"},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			if actual := Format(testCase.moment); actual != testCase.expected {
				subTest.Errorf("期待 %q、実際 %q", testCase.expected, actual)
			}
		})
	}
}

// TestFormatMillis は、JST のミリ秒つきの文字列になること(UTC の入力、ミリ秒より細かい部分の切り捨て、ミリ秒が 0 のときの桁)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestFormatMillis(testingT *testing.T) {
	// UTC の 12:00:01.123456789 は、JST の 21:00:01.123(切り捨て)。
	if formatted := FormatMillis(time.Date(2026, 11, 15, 12, 0, 1, 123456789, time.UTC)); formatted != "2026-11-15T21:00:01.123+09:00" {
		testingT.Errorf("JST のミリ秒つきのはず: %s", formatted)
	}
	// ミリ秒が 0 でも、3 桁そろう。
	if formatted := FormatMillis(time.Date(2026, 11, 15, 12, 0, 1, 0, time.UTC)); formatted != "2026-11-15T21:00:01.000+09:00" {
		testingT.Errorf("3桁そろうはず: %s", formatted)
	}
}
