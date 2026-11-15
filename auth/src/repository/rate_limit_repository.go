package repository

import (
	"context"
	"time"

	"launchs/shared/apperror"

	"gorm.io/gorm"
)

// RateLimitRepository は、rate_limit_events テーブル(IP ごとのレート制限)を担当する。
type RateLimitRepository struct {
	database *gorm.DB
}

// NewRateLimitRepository は、RateLimitRepository を作る。
//
// 引数:
//   - database: auth データベースの接続。
//
// 戻り値:
//   - 作成した RateLimitRepository。
func NewRateLimitRepository(database *gorm.DB) *RateLimitRepository {
	return &RateLimitRepository{database: database}
}

// Hit は、IP の試行を1回記録し、時間枠の中の回数が上限を超えていないかを返す(スライディングウィンドウ)。
//
// 時刻は、データベースの時計で扱う。上限に達しているときは、許さず、記録もしない
// (大量に送られても、行が増えず、数える処理も重くならない。時間枠が進めば、また許される)。
// 同じ種類・IP の判定は、アドバイザリロックで順番に行うので、同時に来ても、上限を超えて許さない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - kind: 種類("login" か "signup")。
//   - ip: クライアントの IP。
//   - limit: 時間枠の中で許す回数。
//   - window: 時間枠。
//
// 戻り値:
//   - 許されるなら true。
//   - 許されないとき、あと何秒待てば、また許されるか(Retry-After に使う)。
//   - データベースのエラーは internal_error。
func (rateLimitRepository *RateLimitRepository) Hit(requestContext context.Context, kind string, ip string, limit int, window time.Duration) (bool, time.Duration, error) {
	// 記録と判定を、1つのトランザクションで行う。
	allowed := true
	retryAfter := time.Duration(0)
	transactionError := rateLimitRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 同じ種類・IP の判定を、順番にする(トランザクションの終わりで、ロックが外れる)。
		if lockError := transaction.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", kind+"|"+ip).Error; lockError != nil {
			return lockError
		}
		// 時間枠の中の回数を数える。
		var count int64
		if countError := transaction.Raw(
			"SELECT count(*) FROM rate_limit_events WHERE kind = ? AND ip = ? AND occurred_at > now() - make_interval(secs => ?)",
			kind, ip, window.Seconds()).Scan(&count).Error; countError != nil {
			return countError
		}
		// 上限に達していなければ、試行を記録して、許す。
		if count < int64(limit) {
			return transaction.Exec("INSERT INTO rate_limit_events (kind, ip) VALUES (?, ?)", kind, ip).Error
		}
		// 上限に達している(記録しない)。あと1回許されるのは、古い方から (count - limit + 1) 番目の試行が、時間枠から外れるとき。
		allowed = false
		var remainingSeconds float64
		if waitError := transaction.Raw(`
			SELECT EXTRACT(EPOCH FROM (occurred_at + make_interval(secs => ?) - now()))::float8
			FROM rate_limit_events WHERE kind = ? AND ip = ? AND occurred_at > now() - make_interval(secs => ?)
			ORDER BY occurred_at, id OFFSET ? LIMIT 1`,
			window.Seconds(), kind, ip, window.Seconds(), count-int64(limit)).Scan(&remainingSeconds).Error; waitError != nil {
			return waitError
		}
		retryAfter = time.Duration(remainingSeconds * float64(time.Second))
		return nil
	})
	if transactionError != nil {
		return false, 0, apperror.Wrap(apperror.KindInternal, "レート制限を確認できませんでした", transactionError)
	}
	return allowed, retryAfter, nil
}

// DeleteOlderThan は、古い試行の記録を消す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - olderThan: この時間より前の記録を消す(最も長い時間枠より、長くすること)。
//
// 戻り値:
//   - 消した数。データベースのエラーは internal_error。
func (rateLimitRepository *RateLimitRepository) DeleteOlderThan(requestContext context.Context, olderThan time.Duration) (int64, error) {
	// 古い記録を消す。
	result := rateLimitRepository.database.WithContext(requestContext).Exec(
		"DELETE FROM rate_limit_events WHERE occurred_at < now() - make_interval(secs => ?)", olderThan.Seconds())
	if result.Error != nil {
		return 0, apperror.Wrap(apperror.KindInternal, "古い試行の記録を消せませんでした", result.Error)
	}
	return result.RowsAffected, nil
}
