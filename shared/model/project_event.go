package model

import "time"

// ProjectEvent は、プロジェクトの namespace で起きた、k8s の Event(docs/project-events.md)。
//
// 書き手は watcher だけ。backend は読むだけ。k8s の Event は、約 1 時間で消えるので、DB に溜めて、過去も見られるようにする。
type ProjectEvent struct {
	// ID は、通し番号(一覧の並びの、同じ時刻の順序と、カーソルに使う)。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`

	// ProjectID は、namespace(プロジェクトの ID)。
	ProjectID string `gorm:"column:project_id"`

	// EventUID は、k8s の Event の UID。同じ Event は、回数を増やして更新されるので、これで 1 行にまとめる。
	EventUID string `gorm:"column:event_uid"`

	// Type は、Normal か Warning。
	Type string `gorm:"column:type"`

	// Reason は、理由(Pulling、BackOff、FailedScheduling など)。
	Reason string `gorm:"column:reason"`

	// Message は、本文。
	Message string `gorm:"column:message"`

	// ObjectKind は、対象の種類(Pod、Deployment など)。
	ObjectKind string `gorm:"column:object_kind"`

	// ObjectName は、対象の名前。
	ObjectName string `gorm:"column:object_name"`

	// DeployID は、対象が属する deploy の ID。属さなければ空。
	DeployID string `gorm:"column:deploy_id"`

	// Count は、同じ Event が起きた回数。
	Count int `gorm:"column:count"`

	// FirstSeenAt は、最初に起きた時刻。
	FirstSeenAt time.Time `gorm:"column:first_seen_at"`

	// LastSeenAt は、最後に起きた時刻。
	LastSeenAt time.Time `gorm:"column:last_seen_at"`
}

// TableName は、テーブルの名前を返す。
//
// 戻り値:
//   - テーブルの名前。
func (ProjectEvent) TableName() string {
	return "project_events"
}

// ProjectEventTypeNormal と ProjectEventTypeWarning は、k8s の Event の種類。
const (
	ProjectEventTypeNormal  = "Normal"
	ProjectEventTypeWarning = "Warning"
)

// ProjectEventFilter は、Event の一覧の絞り込み。空の項目は、その条件を使わない。
type ProjectEventFilter struct {
	// Type は、Normal か Warning。
	Type string

	// DeployID は、deploy の ID。
	DeployID string

	// Reason は、理由(完全に一致するもの)。
	Reason string
}
