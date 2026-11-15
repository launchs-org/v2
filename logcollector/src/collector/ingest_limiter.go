package collector

import (
	"fmt"
	"time"
)

// ingestLimiter は、Pod ごとの書き込みの速さ(バイト/秒)を制限する。超えた行は、捨てる。
//
// 1秒ごとの窓で数える。窓の始めには、必ず1行は通す(1行が上限より長くても、止まらない)。
// 捨てた数は、次の窓の始めに、まとめて知らせる(1秒に1行の、印の行にする)。
type ingestLimiter struct {
	bytesPerSecond int
	windowStart    time.Time
	used           int
	dropped        int
}

// newIngestLimiter は、ingestLimiter を作る。
//
// 引数:
//   - bytesPerSecond: 1秒に通す大きさ(バイト)。
//
// 戻り値:
//   - 作成した ingestLimiter。
func newIngestLimiter(bytesPerSecond int) *ingestLimiter {
	// 上限を持たせて返す(窓は、最初の呼び出しで始まる)。
	return &ingestLimiter{bytesPerSecond: bytesPerSecond}
}

// allow は、行を通してよいかを返す。窓が変わったときは、前の窓で捨てた数も返す。
//
// 引数:
//   - size: 行の大きさ(バイト)。
//   - now: 現在の時刻。
//
// 戻り値:
//   - 通してよければ true。
//   - 前の窓で捨てた行の数(窓が変わったときだけ。0 なら、知らせることは無い)。
func (limiter *ingestLimiter) allow(size int, now time.Time) (bool, int) {
	// 窓が変わったら、前の窓で捨てた数を取り出して、数え直す。
	droppedBefore := 0
	if limiter.windowStart.IsZero() || now.Sub(limiter.windowStart) >= time.Second {
		droppedBefore = limiter.dropped
		limiter.dropped, limiter.used, limiter.windowStart = 0, 0, now
	}
	// 窓に、すでに行があって、上限を超えるなら、捨てる。
	if limiter.used > 0 && limiter.used+size > limiter.bytesPerSecond {
		limiter.dropped++
		return false, droppedBefore
	}
	limiter.used += size
	return true, droppedBefore
}

// takeDropped は、まだ知らせていない、捨てた行の数を取り出す(数えは、0 に戻る)。ストリームの終わりに使う。
//
// 戻り値:
//   - 捨てた行の数。
func (limiter *ingestLimiter) takeDropped() int {
	// 取り出して、0 に戻す。
	dropped := limiter.dropped
	limiter.dropped = 0
	return dropped
}

// droppedMarker は、捨てた行の数を知らせる、印の行の本文を作る。
//
// 引数:
//   - dropped: 捨てた行の数。
//
// 戻り値:
//   - 印の行の本文。
func droppedMarker(dropped int) string {
	// 数を入れた文章にする。
	return fmt.Sprintf("[logcollector] 書き込みの速さの上限を超えたため、%d 行を捨てました", dropped)
}
