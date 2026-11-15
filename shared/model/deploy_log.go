package model

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// DefaultLogRetention は、ログの行を残す期間の既定(7日)。
	DefaultLogRetention = 7 * 24 * time.Hour

	// DefaultMaxLogBytesPerPod は、Pod ごとの、保存するログの大きさの上限の既定(50MiB)。
	DefaultMaxLogBytesPerPod int64 = 50 * 1024 * 1024

	// DefaultMaxLogLineBytes は、1行の長さの上限の既定(16KiB)。
	DefaultMaxLogLineBytes = 16 * 1024

	// LogTruncatedSuffix は、長すぎて切り詰めた行の末尾に付ける印。
	LogTruncatedSuffix = "…(truncated)"
)

// DeployLogStream は、deploy_log_streams テーブルの1行。Pod 1つの、ログの収集の状態。
type DeployLogStream struct {
	// PodUID は、k8s の Pod の UID(API の podId は "pod-" + この値)。
	PodUID string `gorm:"column:pod_uid;primaryKey"`

	// DeployID は、属する deploy の ID。
	DeployID string `gorm:"column:deploy_id"`

	// ProjectID は、Pod の namespace(プロジェクトの ID)。
	ProjectID string `gorm:"column:project_id"`

	// PodName は、Pod の名前。
	PodName string `gorm:"column:pod_name"`

	// StartedAt は、Pod の作成時刻。
	StartedAt time.Time `gorm:"column:started_at"`

	// FinishedAt は、収集を終えた時刻。収集中は nil。
	FinishedAt *time.Time `gorm:"column:finished_at"`

	// ResumeNS は、読み取り位置のうち、最後に保存した行の時刻(unix ナノ秒)。
	ResumeNS int64 `gorm:"column:resume_ns"`

	// ResumeCount は、読み取り位置のうち、ResumeNS と同じ時刻の行を、何行まで保存したか。
	ResumeCount int `gorm:"column:resume_count"`

	// RestartCount は、最後に処理した、コンテナの再起動の回数。
	RestartCount int `gorm:"column:restart_count"`

	// Bytes は、保存済みの行の長さの合計(バイト)。
	Bytes int64 `gorm:"column:bytes"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("deploy_log_streams")。
func (DeployLogStream) TableName() string {
	return "deploy_log_streams"
}

// ResumePosition は、ログの読み取り位置。「最後に保存した行の時刻」と「その時刻の行を何行まで保存したか」。
type ResumePosition struct {
	// NS は、最後に保存した行の時刻(unix ナノ秒)。まだ何も保存していなければ 0。
	NS int64

	// Count は、NS と同じ時刻の行を、何行まで保存したか。
	Count int
}

// ResumePosition は、ストリームに保存されている読み取り位置を返す。
//
// 戻り値:
//   - 読み取り位置。
func (stream DeployLogStream) ResumePosition() ResumePosition {
	// 列の値を、そのまま位置にする。
	return ResumePosition{NS: stream.ResumeNS, Count: stream.ResumeCount}
}

// DeployLog は、deploy_logs テーブルの1行。ログの1行。
type DeployLog struct {
	// ID は、全体で増える番号(API の since / nextSince)。
	ID int64 `gorm:"column:id;primaryKey"`

	// PodUID は、ログを出した Pod の UID。
	PodUID string `gorm:"column:pod_uid"`

	// LoggedNS は、行の時刻(unix ナノ秒)。
	LoggedNS int64 `gorm:"column:logged_ns"`

	// Line は、行の本文。
	Line string `gorm:"column:line"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("deploy_logs")。
func (DeployLog) TableName() string {
	return "deploy_logs"
}

// ParseKubeLogLine は、k8s のログ API(timestamps=true)の1行を、時刻と本文に分ける。
//
// 形は「<RFC3339Nano の時刻> <本文>」。本文が空のときは、時刻だけの行になる。
//
// 引数:
//   - raw: ログの1行(改行は除く)。
//
// 戻り値:
//   - 時刻(unix ナノ秒)と、本文。時刻を読めなければ、第3の戻り値が false(本文は、行の全体)。
func ParseKubeLogLine(raw string) (int64, string, bool) {
	// 最初の空白で、時刻と本文に分ける(空白が無ければ、全体が時刻)。
	timestampText, text, _ := strings.Cut(raw, " ")
	parsed, parseError := time.Parse(time.RFC3339Nano, timestampText)
	if parseError != nil {
		return 0, raw, false
	}
	return parsed.UnixNano(), text, true
}

// SanitizeLogLine は、ログの本文を、保存できる形にする。
//
//   - NUL バイトを取り除く(PostgreSQL の text に入れられないため)。
//   - 不正な UTF-8 を U+FFFD に置き換える。
//   - maxBytes を超えるなら、切り詰めて、末尾に LogTruncatedSuffix を付ける(UTF-8 の文字の途中では切らない)。
//
// 引数:
//   - text: 本文。
//   - maxBytes: 1行の長さの上限(バイト)。
//
// 戻り値:
//   - 保存する本文。
func SanitizeLogLine(text string, maxBytes int) string {
	// NUL を除き、不正な UTF-8 を置き換える。
	text = strings.ToValidUTF8(strings.ReplaceAll(text, "\x00", ""), "�")
	// 上限以内なら、そのまま。
	if len(text) <= maxBytes {
		return text
	}
	// 上限まで切り詰める(文字の途中なら、手前まで戻る)。
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + LogTruncatedSuffix
}

// LogResumeFilter は、再接続のたびに届く、すでに保存した行を取り除くための、読み取り位置の管理。
//
// k8s の sinceTime は秒の精度で、同じ秒の行から返ることがある。そのため、
// 時刻が位置より前の行は捨て、位置と同じ時刻の行は、最初の Count 行を捨て、それより後を、新しい行とする。
// 新しい行を受け入れるたびに、位置を進める。
type LogResumeFilter struct {
	position ResumePosition
	seenAtNS int
}

// NewLogResumeFilter は、読み取り位置から、フィルターを作る。
//
// 引数:
//   - position: 保存されている読み取り位置。
//
// 戻り値:
//   - 作成したフィルター。
func NewLogResumeFilter(position ResumePosition) *LogResumeFilter {
	// 位置を持たせて返す(同じ時刻の行は、まだ1行も見ていない)。
	return &LogResumeFilter{position: position}
}

// Accept は、行(の時刻)が、新しい行か(保存すべきか)を返す。新しい行なら、位置を進める。
//
// 時刻が前の行より前に戻る(同じ時刻より前)ことは、ログでは、まず無い。もし来たら、捨てる(順序を壊さないため)。
//
// 引数:
//   - ns: 行の時刻(unix ナノ秒)。
//
// 戻り値:
//   - 新しい行なら true。すでに保存した行(または、位置より前の行)なら false。
func (filter *LogResumeFilter) Accept(ns int64) bool {
	// 位置より前の行は、保存済み。
	if ns < filter.position.NS {
		return false
	}
	// 位置と同じ時刻の行は、位置の行数までは、保存済み。
	if ns == filter.position.NS {
		filter.seenAtNS++
		if filter.seenAtNS <= filter.position.Count {
			return false
		}
		// 保存済みの数を超えた分は、新しい行(同じ時刻の行が増えた)。
		filter.position.Count = filter.seenAtNS
		return true
	}
	// 位置より後の時刻は、新しい行。位置を、この時刻の1行目に進める。
	filter.position = ResumePosition{NS: ns, Count: 1}
	filter.seenAtNS = 1
	return true
}

// Position は、いまの読み取り位置(最後に受け入れた行まで)を返す。
//
// 戻り値:
//   - 読み取り位置。
func (filter *LogResumeFilter) Position() ResumePosition {
	// 持っている位置を返す。
	return filter.position
}
