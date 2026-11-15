package collector

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"
)

// TestReadLimitedLine は、行の読み取り(改行の除去、最後の改行なし、上限を超える行の読み捨て、空行、EOF)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestReadLimitedLine(testingT *testing.T) {
	// 短いバッファで、長い行が、複数回に分かれて届くようにする。
	input := "first\r\n\nsecond line\n" + strings.Repeat("y", 10000) + "\nlast without newline"
	reader := bufio.NewReaderSize(strings.NewReader(input), 16)
	// 読んだ行(上限は、本文 100 + 余裕)。
	expected := []string{"first", "", "second line"}
	for _, want := range expected {
		if line, readError := readLimitedLine(reader, 100); readError != nil || line != want {
			testingT.Fatalf("行が違う: %q %v(期待 %q)", line, readError, want)
		}
	}
	// 上限を超える行は、上限(+余裕)まで読んで、残りは読み捨てる。次の行は、ずれずに読める。
	if long, readError := readLimitedLine(reader, 100); readError != nil || len(long) != 100+lineSlack || strings.Trim(long, "y") != "" {
		testingT.Fatalf("上限まで読んで、読み捨てるはず: %d %v", len(long), readError)
	}
	if last, readError := readLimitedLine(reader, 100); readError != nil || last != "last without newline" {
		testingT.Fatalf("最後の改行なしの行: %q %v", last, readError)
	}
	if _, readError := readLimitedLine(reader, 100); readError != io.EOF {
		testingT.Fatalf("EOF のはず: %v", readError)
	}
}

// TestIngestLimiter は、窓の中の上限、窓の始めの1行は必ず通すこと、窓が変わったときに、捨てた数を知らせること、
// ストリームの終わりに、残りの数を取り出せることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestIngestLimiter(testingT *testing.T) {
	// 1秒に 25 バイト。
	limiter := newIngestLimiter(25)
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	// 同じ窓: 10、10 は通り、3つ目(30 > 25)は捨てる。
	for index, want := range []bool{true, true, false, false} {
		if allowed, dropped := limiter.allow(10, start); allowed != want || dropped != 0 {
			testingT.Fatalf("%d 行目: 通る=%v、捨てた数=%d(期待 通る=%v)", index+1, allowed, dropped, want)
		}
	}
	// 窓が変わると、前の窓で捨てた 2 行を知らせ、新しい窓の始めの行は通る。
	if allowed, dropped := limiter.allow(10, start.Add(time.Second)); !allowed || dropped != 2 {
		testingT.Fatalf("窓が変わったら、2行を知らせて通すはず: %v %d", allowed, dropped)
	}
	// 窓の始めの行は、上限より大きくても、通る(止まらない)。
	if allowed, _ := limiter.allow(1000, start.Add(2*time.Second)); !allowed {
		testingT.Fatal("窓の始めの1行は、上限より大きくても、通るはず")
	}
	// 残りの数を取り出せて、取り出すと 0 に戻る。
	limiter.allow(10, start.Add(2*time.Second))
	if remaining := limiter.takeDropped(); remaining != 1 || limiter.takeDropped() != 0 {
		testingT.Fatalf("残りを取り出せるはず: %d", remaining)
	}
}
