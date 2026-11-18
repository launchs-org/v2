package repository

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"launchs/shared/accessstats"
	"launchs/shared/apperror"
)

const (
	// accessMinutesTable は、1 分ごとの集計のテーブル。
	accessMinutesTable = "ingress_access_minutes"

	// accessHoursTable は、1 時間ごとの集計のテーブル。
	accessHoursTable = "ingress_access_hours"
)

// accessStatsRow は、集計のテーブルの 1 行(minutes と hours は、同じ形)。
type accessStatsRow struct {
	IngressID     string         `gorm:"column:ingress_id"`
	Replica       string         `gorm:"column:replica"`
	BucketStart   time.Time      `gorm:"column:bucket_start"`
	IntervalSec   int            `gorm:"column:interval_sec"`
	Count         int64          `gorm:"column:count"`
	Status        datatypes.JSON `gorm:"column:status"`
	Methods       datatypes.JSON `gorm:"column:methods"`
	Bytes         int64          `gorm:"column:bytes"`
	Latency       datatypes.JSON `gorm:"column:latency"`
	SumMs         float64        `gorm:"column:sum_ms"`
	Paths         datatypes.JSON `gorm:"column:paths"`
	Clients       datatypes.JSON `gorm:"column:clients"`
	UniqueClients int            `gorm:"column:unique_clients"`
}

// AccessStatsRepository は、ingress のアクセスの集計の保存と読み出しを行う。
type AccessStatsRepository struct {
	database *gorm.DB
}

// NewAccessStatsRepository は、AccessStatsRepository を作る。
//
// 引数:
//   - database: 使うデータベース。
//
// 戻り値:
//   - 作成した AccessStatsRepository。
func NewAccessStatsRepository(database *gorm.DB) *AccessStatsRepository {
	// データベースを持たせて返す。
	return &AccessStatsRepository{database: database}
}

// rowFromRecord は、集計を、テーブルの行にする。
func rowFromRecord(record accessstats.Record) (accessStatsRow, error) {
	// 各項目を JSON にする。
	row := accessStatsRow{IngressID: record.IngressID, Replica: record.Replica, BucketStart: record.Start, IntervalSec: record.IntervalSec,
		Count: record.Count, Bytes: record.Bytes, SumMs: record.SumMs, UniqueClients: record.UniqueClients}
	for target, value := range map[*datatypes.JSON]any{&row.Status: record.Status, &row.Methods: record.Methods, &row.Latency: record.Latency, &row.Paths: nonNilCounted(record.Paths), &row.Clients: nonNilCounted(record.Clients)} {
		encoded, marshalError := json.Marshal(value)
		if marshalError != nil {
			return accessStatsRow{}, marshalError
		}
		*target = encoded
	}
	return row, nil
}

// nonNilCounted は、空の一覧を、null ではなく空の配列にする。
func nonNilCounted(items []accessstats.Counted) []accessstats.Counted {
	// nil なら、空の配列に替える。
	if items == nil {
		return []accessstats.Counted{}
	}
	return items
}

// recordFromRow は、テーブルの行を、集計にする。
func recordFromRow(row accessStatsRow) accessstats.Record {
	// JSON の項目を読む(壊れていれば、空のまま)。
	record := accessstats.Record{IngressID: row.IngressID, Replica: row.Replica, Start: row.BucketStart.UTC(), IntervalSec: row.IntervalSec,
		Count: row.Count, Bytes: row.Bytes, SumMs: row.SumMs, UniqueClients: row.UniqueClients}
	_ = json.Unmarshal(row.Status, &record.Status)
	_ = json.Unmarshal(row.Methods, &record.Methods)
	_ = json.Unmarshal(row.Latency, &record.Latency)
	_ = json.Unmarshal(row.Paths, &record.Paths)
	_ = json.Unmarshal(row.Clients, &record.Clients)
	return record
}

// upsert は、集計を、指定のテーブルに保存する(同じ (ingress_id, replica, bucket_start) の行は、上書きする。冪等)。
func (accessStatsRepository *AccessStatsRepository) upsert(requestContext context.Context, table string, record accessstats.Record) error {
	// 行にして、書く。
	row, rowError := rowFromRecord(record)
	if rowError != nil {
		return apperror.Wrap(apperror.KindInternal, "アクセスの集計を保存する形にできませんでした", rowError)
	}
	saveError := accessStatsRepository.database.WithContext(requestContext).Exec(
		`INSERT INTO `+table+` (ingress_id, replica, bucket_start, interval_sec, count, status, methods, bytes, latency, sum_ms, paths, clients, unique_clients)
		 VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?::jsonb, ?, ?::jsonb, ?::jsonb, ?)
		 ON CONFLICT (ingress_id, replica, bucket_start) DO UPDATE SET interval_sec = EXCLUDED.interval_sec, count = EXCLUDED.count, status = EXCLUDED.status,
		   methods = EXCLUDED.methods, bytes = EXCLUDED.bytes, latency = EXCLUDED.latency, sum_ms = EXCLUDED.sum_ms, paths = EXCLUDED.paths,
		   clients = EXCLUDED.clients, unique_clients = EXCLUDED.unique_clients`,
		row.IngressID, row.Replica, row.BucketStart, row.IntervalSec, row.Count, string(row.Status), string(row.Methods), row.Bytes,
		string(row.Latency), row.SumMs, string(row.Paths), string(row.Clients), row.UniqueClients).Error
	if saveError != nil {
		return apperror.Wrap(apperror.KindInternal, "アクセスの集計を保存できませんでした", saveError)
	}
	return nil
}

// UpsertMinute は、1 分ごとの集計を保存する(同じ行は上書きする。再接続で同じ行が来ても、二重にならない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - record: 保存する集計。
//
// 戻り値:
//   - データベースのエラー。
func (accessStatsRepository *AccessStatsRepository) UpsertMinute(requestContext context.Context, record accessstats.Record) error {
	// minutes に書く。
	return accessStatsRepository.upsert(requestContext, accessMinutesTable, record)
}

// Query は、ingress の集計を、期間 [from, to) で読む。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - hours: true なら 1 時間ごとの表、false なら 1 分ごとの表から読む。
//   - ingressID: ingress の ID。
//   - from: 期間の始まり(含む)。
//   - to: 期間の終わり(含まない)。
//
// 戻り値:
//   - 集計(始まりの時刻の昇順)。
func (accessStatsRepository *AccessStatsRepository) Query(requestContext context.Context, hours bool, ingressID string, from time.Time, to time.Time) ([]accessstats.Record, error) {
	// 表を選んで読む。
	table := accessMinutesTable
	if hours {
		table = accessHoursTable
	}
	var rows []accessStatsRow
	findError := accessStatsRepository.database.WithContext(requestContext).Table(table).
		Where("ingress_id = ? AND bucket_start >= ? AND bucket_start < ?", ingressID, from, to).Order("bucket_start ASC").Find(&rows).Error
	if findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "アクセスの集計を取得できませんでした", findError)
	}
	records := make([]accessstats.Record, 0, len(rows))
	for _, row := range rows {
		records = append(records, recordFromRow(row))
	}
	return records, nil
}

// RollupHours は、1 分ごとの集計を、1 時間ごとにまとめて、hours に保存する(期間 [from, 今) の分。同じ行は上書きする。冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - from: まとめ直す期間の始まり(1 時間の区切りに丸めて使う)。
//
// 戻り値:
//   - データベースのエラー。
func (accessStatsRepository *AccessStatsRepository) RollupHours(requestContext context.Context, from time.Time) error {
	// 期間の分の行を読む。
	var rows []accessStatsRow
	findError := accessStatsRepository.database.WithContext(requestContext).Table(accessMinutesTable).
		Where("bucket_start >= ?", from.UTC().Truncate(time.Hour)).Order("bucket_start ASC").Find(&rows).Error
	if findError != nil {
		return apperror.Wrap(apperror.KindInternal, "アクセスの集計を取得できませんでした", findError)
	}
	// (ingress, 時間) ごとに分ける。
	type groupKey struct {
		ingressID string
		hour      time.Time
	}
	groups := map[groupKey][]accessstats.Record{}
	for _, row := range rows {
		record := recordFromRow(row)
		key := groupKey{ingressID: record.IngressID, hour: record.Start.Truncate(time.Hour)}
		groups[key] = append(groups[key], record)
	}
	// 足し合わせて、保存する。
	for key, records := range groups {
		merged := accessstats.Merge(records, accessstats.HourTopLimit)
		merged.IngressID = key.ingressID
		merged.Replica = ""
		merged.Start = key.hour
		merged.IntervalSec = 3600
		if saveError := accessStatsRepository.upsert(requestContext, accessHoursTable, merged); saveError != nil {
			return saveError
		}
	}
	return nil
}

// Prune は、保存期間を過ぎた集計を削除する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - minutesCutoff: これより前の minutes を消す。
//   - hoursCutoff: これより前の hours を消す。
//
// 戻り値:
//   - データベースのエラー。
func (accessStatsRepository *AccessStatsRepository) Prune(requestContext context.Context, minutesCutoff time.Time, hoursCutoff time.Time) error {
	// 2 つの表から、古い行を消す。
	for table, cutoff := range map[string]time.Time{accessMinutesTable: minutesCutoff, accessHoursTable: hoursCutoff} {
		if deleteError := accessStatsRepository.database.WithContext(requestContext).Exec("DELETE FROM "+table+" WHERE bucket_start < ?", cutoff).Error; deleteError != nil {
			return apperror.Wrap(apperror.KindInternal, "古いアクセスの集計を削除できませんでした", deleteError)
		}
	}
	return nil
}
