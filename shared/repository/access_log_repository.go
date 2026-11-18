package repository

import (
	"context"
	"strconv"
	"strings"
	"time"

	"launchs/shared/accessstats"
	"launchs/shared/apperror"
	"launchs/shared/pagination"
)

// accessLogsTable は、アクセスログのテーブル。
const accessLogsTable = "ingress_access_logs"

// AccessLog は、アクセスログの 1 行(読み出し用)。
type AccessLog struct {
	// ID は、行の ID(新しい順の並びと、ページングに使う)。
	ID int64 `gorm:"column:id" json:"-"`

	// RequestedAt は、リクエストを受けた時刻。
	RequestedAt time.Time `gorm:"column:requested_at" json:"time"`

	// Method は、メソッド。
	Method string `gorm:"column:method" json:"method"`

	// URL は、パスとクエリ。
	URL string `gorm:"column:url" json:"url"`

	// Status は、ステータス(記録できないものは 0)。
	Status int `gorm:"column:status" json:"status"`

	// DurationMs は、応答にかかった時間(ミリ秒)。
	DurationMs float64 `gorm:"column:duration_ms" json:"durationMs"`

	// ClientIP は、クライアントの IP。
	ClientIP string `gorm:"column:client_ip" json:"clientIp"`

	// Bytes は、応答の大きさ(バイト)。
	Bytes int64 `gorm:"column:bytes" json:"bytes"`
}

// AccessLogFilter は、アクセスログの絞り込み(空の項目は、絞り込まない)。
type AccessLogFilter struct {
	// Status は、ステータス(「2xx」「3xx」「4xx」「5xx」の分類か、「404」のような数字)。
	Status string

	// Method は、メソッド(大文字小文字は区別しない)。
	Method string

	// ClientIP は、クライアントの IP(完全一致)。
	ClientIP string

	// From と To は、期間(From 以上、To より前)。ゼロ値なら、その側は絞り込まない。
	From time.Time
	To   time.Time

	// Query は、URL に含まれる文字列(大文字小文字は区別しない)。
	Query string
}

// InsertLogs は、1 回の出力に含まれる、1 リクエストごとの記録を保存する(同じ出力を再度読んでも、二重にならない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - record: 出力の集計(IngressID、Replica、Start と、Requests を使う)。
//
// 戻り値:
//   - データベースのエラー。
func (accessStatsRepository *AccessStatsRepository) InsertLogs(requestContext context.Context, record accessstats.Record) error {
	// 記録が無ければ、何もしない。
	if len(record.Requests) == 0 {
		return nil
	}
	// 複数行を、1 回の文で保存する(重複は、読み飛ばす)。
	placeholders := make([]string, 0, len(record.Requests))
	arguments := make([]any, 0, len(record.Requests)*11)
	for index, entry := range record.Requests {
		placeholders = append(placeholders, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		arguments = append(arguments, record.IngressID, record.Replica, record.Start, index, time.UnixMilli(entry.T).UTC(), entry.Method, entry.URL, entry.Status, entry.DurationMs, entry.ClientIP, entry.Bytes)
	}
	insertError := accessStatsRepository.database.WithContext(requestContext).Exec(
		`INSERT INTO `+accessLogsTable+` (ingress_id, replica, batch_start, seq, requested_at, method, url, status, duration_ms, client_ip, bytes) VALUES `+
			strings.Join(placeholders, ", ")+` ON CONFLICT (ingress_id, replica, batch_start, seq) DO NOTHING`, arguments...).Error
	if insertError != nil {
		return apperror.Wrap(apperror.KindInternal, "アクセスログを保存できませんでした", insertError)
	}
	return nil
}

// ListLogs は、ingress のアクセスログを、新しい順に返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: ingress の ID。
//   - filter: 絞り込み。
//   - limit: 1 ページの件数。
//   - after: 前のページの最後の位置。最初は nil(位置の ID は、行の ID の数字)。
//
// 戻り値:
//   - ログ(新しい順)と、次のページの位置(最後のページなら nil)。
func (accessStatsRepository *AccessStatsRepository) ListLogs(requestContext context.Context, ingressID string, filter AccessLogFilter, limit int, after *pagination.Position) ([]AccessLog, *pagination.Position, error) {
	// 絞り込みを組み立てる(次のページがあるかを知るため、1 件多く読む)。
	query := accessStatsRepository.database.WithContext(requestContext).Table(accessLogsTable).Where("ingress_id = ?", ingressID)
	if after != nil {
		afterID, parseError := strconv.ParseInt(after.ID, 10, 64)
		if parseError != nil {
			return nil, nil, apperror.New(apperror.KindInvalidRequest, "cursor が正しくありません")
		}
		query = query.Where("(requested_at, id) < (?, ?)", after.CreatedAt, afterID)
	}
	if !filter.From.IsZero() {
		query = query.Where("requested_at >= ?", filter.From)
	}
	if !filter.To.IsZero() {
		query = query.Where("requested_at < ?", filter.To)
	}
	if status := strings.ToLower(strings.TrimSpace(filter.Status)); status != "" {
		if len(status) == 3 && status[1:] == "xx" && status[0] >= '1' && status[0] <= '5' {
			base := int(status[0]-'0') * 100
			query = query.Where("status >= ? AND status < ?", base, base+100)
		} else if code, parseError := strconv.Atoi(status); parseError == nil {
			query = query.Where("status = ?", code)
		} else {
			return nil, nil, apperror.New(apperror.KindInvalidRequest, "status は 2xx のような分類か、404 のような数字にしてください")
		}
	}
	if method := strings.ToUpper(strings.TrimSpace(filter.Method)); method != "" {
		query = query.Where("method = ?", method)
	}
	if clientIP := strings.TrimSpace(filter.ClientIP); clientIP != "" {
		query = query.Where("client_ip = ?", clientIP)
	}
	if text := strings.TrimSpace(filter.Query); text != "" {
		// LIKE の特別な文字(% と _ と \)は、そのままの文字として探す。
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
		query = query.Where(`url ILIKE ? ESCAPE '\'`, "%"+escaped+"%")
	}
	var logs []AccessLog
	if findError := query.Order("requested_at DESC, id DESC").Limit(limit + 1).Find(&logs).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "アクセスログを取得できませんでした", findError)
	}
	// limit 件以下なら、次のページは無い。
	if len(logs) <= limit {
		return logs, nil, nil
	}
	logs = logs[:limit]
	last := logs[limit-1]
	return logs, &pagination.Position{CreatedAt: last.RequestedAt, ID: strconv.FormatInt(last.ID, 10)}, nil
}

// PruneLogs は、保存期間を過ぎたログと、ingress ごとの行数の上限を超えた古いログを削除する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - cutoff: これより前のログを消す。
//   - maxRowsPerIngress: ingress ごとに残す最大の行数(新しい順)。
//
// 戻り値:
//   - データベースのエラー。
func (accessStatsRepository *AccessStatsRepository) PruneLogs(requestContext context.Context, cutoff time.Time, maxRowsPerIngress int) error {
	// 保存期間を過ぎたものを消す。
	if deleteError := accessStatsRepository.database.WithContext(requestContext).Exec("DELETE FROM "+accessLogsTable+" WHERE requested_at < ?", cutoff).Error; deleteError != nil {
		return apperror.Wrap(apperror.KindInternal, "古いアクセスログを削除できませんでした", deleteError)
	}
	// ingress ごとに、上限を超えた古い行を消す。
	trimError := accessStatsRepository.database.WithContext(requestContext).Exec(
		`DELETE FROM `+accessLogsTable+` WHERE id IN (
		   SELECT id FROM (SELECT id, row_number() OVER (PARTITION BY ingress_id ORDER BY requested_at DESC, id DESC) AS position FROM `+accessLogsTable+`) ranked
		   WHERE position > ?)`, maxRowsPerIngress).Error
	if trimError != nil {
		return apperror.Wrap(apperror.KindInternal, "アクセスログの行数を整えられませんでした", trimError)
	}
	return nil
}
