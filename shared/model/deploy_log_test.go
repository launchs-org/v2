package model

import (
	"strings"
	"testing"
	"time"
)

// TestParseKubeLogLine は、k8s のログの1行を、時刻と本文に分けられること(本文が空、本文に空白がある、時刻が読めない)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestParseKubeLogLine(testingT *testing.T) {
	// 時刻(ナノ秒)つきの行は、時刻と本文に分かれる。本文の空白は、そのまま残る。
	expectedNS := time.Date(2026, 11, 15, 12, 0, 1, 123456789, time.UTC).UnixNano()
	if ns, text, ok := ParseKubeLogLine("2026-11-15T12:00:01.123456789Z server started on :8080  ok"); !ok || ns != expectedNS || text != "server started on :8080  ok" {
		testingT.Fatalf("時刻と本文に分かれるはず: %d %q %v", ns, text, ok)
	}
	// 本文が空の行は、時刻だけ。
	if ns, text, ok := ParseKubeLogLine("2026-11-15T12:00:01.5Z"); !ok || ns != time.Date(2026, 11, 15, 12, 0, 1, 500000000, time.UTC).UnixNano() || text != "" {
		testingT.Fatalf("時刻だけの行のはず: %d %q %v", ns, text, ok)
	}
	// 時刻を読めない行は、全体を本文として返し、ok は false。
	for _, raw := range []string{"no timestamp here", "", "2026-13-45T00:00:00Z bad month"} {
		if _, text, ok := ParseKubeLogLine(raw); ok || text != raw {
			testingT.Errorf("%q は、読めない行のはず: %q %v", raw, text, ok)
		}
	}
}

// TestSanitizeLogLine は、NUL の除去、不正な UTF-8 の置き換え、長すぎる行の切り詰め(文字の途中では切らない)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestSanitizeLogLine(testingT *testing.T) {
	// NUL を取り除く。不正な UTF-8 は U+FFFD になる。
	if cleaned := SanitizeLogLine("a\x00b\xffc", 100); cleaned != "ab�c" {
		testingT.Errorf("NUL の除去と置き換えのはず: %q", cleaned)
	}
	// 上限ちょうどは、そのまま。1バイト超えると、切り詰めて印が付く。
	if exact := SanitizeLogLine("12345", 5); exact != "12345" {
		testingT.Errorf("上限ちょうどはそのまま: %q", exact)
	}
	if cut := SanitizeLogLine("123456", 5); cut != "12345"+LogTruncatedSuffix {
		testingT.Errorf("切り詰めて印が付くはず: %q", cut)
	}
	// 日本語(1文字3バイト)の途中では切らない。
	if cut := SanitizeLogLine("あいう", 4); cut != "あ"+LogTruncatedSuffix {
		testingT.Errorf("文字の途中では切らないはず: %q", cut)
	}
	// 空行は、そのまま。
	if empty := SanitizeLogLine("", 10); empty != "" {
		testingT.Errorf("空行はそのまま: %q", empty)
	}
	// 長い行でも、結果は、上限と印の長さを超えない。
	if long := SanitizeLogLine(strings.Repeat("x", 100000), DefaultMaxLogLineBytes); len(long) != DefaultMaxLogLineBytes+len(LogTruncatedSuffix) {
		testingT.Errorf("上限+印の長さのはず: %d", len(long))
	}
}

// TestLogResumeFilter は、読み取り位置からの再接続で、保存済みの行を捨て(同じ時刻の行は、保存済みの数まで)、
// 新しい行だけを受け入れ、位置を進めることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestLogResumeFilter(testingT *testing.T) {
	// 初めて(位置なし)は、全ての行を受け入れ、同じ時刻の行も、数えながら受け入れる。
	fresh := NewLogResumeFilter(ResumePosition{})
	for _, ns := range []int64{100, 100, 101} {
		if !fresh.Accept(ns) {
			testingT.Fatalf("初めては、全て受け入れるはず: %d", ns)
		}
	}
	if fresh.Position() != (ResumePosition{NS: 101, Count: 1}) {
		testingT.Fatalf("位置が進むはず: %+v", fresh.Position())
	}
	// 位置 {100, 2}(時刻 100 の行を2行まで保存済み)から再接続。届く行: 99, 100, 100, 100, 101。
	resumed := NewLogResumeFilter(ResumePosition{NS: 100, Count: 2})
	accepted := []bool{}
	for _, ns := range []int64{99, 100, 100, 100, 101} {
		accepted = append(accepted, resumed.Accept(ns))
	}
	// 99 と、100 の最初の2行は捨て、100 の3行目(新しい)と、101 を受け入れる。
	expected := []bool{false, false, false, true, true}
	for index := range expected {
		if accepted[index] != expected[index] {
			testingT.Fatalf("受け入れの結果が違う: %v(期待 %v)", accepted, expected)
		}
	}
	if resumed.Position() != (ResumePosition{NS: 101, Count: 1}) {
		testingT.Fatalf("位置が進むはず: %+v", resumed.Position())
	}
	// 位置より前に時刻が戻った行は、捨てる(順序を壊さない)。
	if resumed.Accept(50) {
		testingT.Error("時刻が戻った行は捨てるはず")
	}
	// 位置 {100, 1} から、同じ時刻の行が増えて届く(100, 100)と、2行目だけが新しい。
	grown := NewLogResumeFilter(ResumePosition{NS: 100, Count: 1})
	if grown.Accept(100) || !grown.Accept(100) || grown.Position() != (ResumePosition{NS: 100, Count: 2}) {
		testingT.Fatalf("同じ時刻の行が増えたときは、増えた分だけ新しいはず: %+v", grown.Position())
	}
}
