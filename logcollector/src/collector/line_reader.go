package collector

import (
	"bufio"
	"io"
)

// rawLine は、ログの API から読んだ1行を、時刻と本文に分けたもの。
type rawLine struct {
	// ns は、行の時刻(unix ナノ秒)。読めなければ 0。
	ns int64

	// text は、行の本文(まだ、保存用に整えていない)。
	text string

	// hasTimestamp は、時刻を読めたか。
	hasTimestamp bool
}

// lineSlack は、時刻の部分(RFC3339Nano は 30〜35 文字)と、区切りの空白のための余裕(バイト)。
const lineSlack = 64

// readLimitedLine は、1行を、上限まで読み、それより長い部分は読み捨てる(メモリを使い切らないため)。
//
// 上限は、本文の上限に、時刻の部分の余裕を足したもの。読み捨てた行は、整える段階で、切り詰めの印が付く
// (読んだ長さが、本文の上限を超えるため)。
//
// 引数:
//   - reader: 読み取り口。
//   - maxBytes: 本文の長さの上限(バイト)。
//
// 戻り値:
//   - 行(改行は除く)。最後の行に改行が無くても、返す。読み終えたら io.EOF(行は空)。他のエラーは、そのまま。
func readLimitedLine(reader *bufio.Reader, maxBytes int) (string, error) {
	// 読む長さの上限を決める。
	limit := maxBytes + lineSlack
	collected := make([]byte, 0, 256)
	for {
		// バッファの分だけ読む(長い行は、isPrefix が true で、続きがある)。
		part, isPrefix, readError := reader.ReadLine()
		if readError != nil {
			// 途中まで読んでいれば、その行を返す(エラーは、次の呼び出しで、また返る)。
			if len(collected) > 0 && readError == io.EOF {
				return string(collected), nil
			}
			return "", readError
		}
		// 上限までを集め、それより後ろは、読み捨てる。
		if remaining := limit - len(collected); remaining > 0 {
			if len(part) > remaining {
				part = part[:remaining]
			}
			collected = append(collected, part...)
		}
		// 行の終わりなら、返す。
		if !isPrefix {
			return string(collected), nil
		}
	}
}
