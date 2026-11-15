package service

import (
	"errors"
	"time"
)

// RateLimitedError は、レート制限に達したときのエラー。何秒待てばよいかを持つ(429 の Retry-After に使う)。
type RateLimitedError struct {
	// RetryAfter は、待つ時間。
	RetryAfter time.Duration
}

// Error は、エラーの文字列を返す。
//
// 戻り値:
//   - エラーの説明。
func (rateLimitedError *RateLimitedError) Error() string {
	// 待つ時間を含める。
	return "リクエストが多すぎます。しばらくしてから、もう一度試してください: " + rateLimitedError.RetryAfter.String()
}

// IsRateLimited は、エラーが、レート制限のものかを調べ、そうなら待つ時間を返す。
//
// 引数:
//   - checkedError: 調べるエラー。
//
// 戻り値:
//   - 待つ時間と、レート制限のエラーだったか。
func IsRateLimited(checkedError error) (time.Duration, bool) {
	// RateLimitedError に変換できるか。
	var rateLimitedError *RateLimitedError
	if errors.As(checkedError, &rateLimitedError) {
		return rateLimitedError.RetryAfter, true
	}
	return 0, false
}
