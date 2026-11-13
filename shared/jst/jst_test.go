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
