// Package model は、データベースのテーブルに対応するモデルを定義する。
//
// バックエンドとワーカーの両方が同じテーブルを読み書きするため、shared に置く。
package model

import "time"

const (
	// ProjectIDPrefix は、プロジェクトの ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。
	ProjectIDPrefix = "project-"

	// ProjectStatusActive は、通常の状態のプロジェクト。
	ProjectStatusActive = "active"

	// ProjectStatusDeleting は、削除を受け付けて、実体を削除している最中のプロジェクト。
	ProjectStatusDeleting = "deleting"
)

// Project は、projects テーブルの1行。build / deploy などは、全ていずれかのプロジェクトに属する。
type Project struct {
	// ID は、プロジェクトの ID("project-" + UUID v4)。
	ID string `gorm:"column:id;primaryKey"`

	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id"`

	// Name は、プロジェクトの名前。英数字とハイフンのみ、1〜64文字。
	Name string `gorm:"column:name"`

	// Status は、プロジェクトの状態(ProjectStatusActive または ProjectStatusDeleting)。
	Status string `gorm:"column:status"`

	// CreatedAt は、作成日時。
	CreatedAt time.Time `gorm:"column:created_at"`

	// UpdatedAt は、更新日時。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("projects")。
func (Project) TableName() string {
	return "projects"
}
