// Package model は、データベースのテーブルに対応するモデルを定義する。
//
// バックエンドとワーカーの両方が同じテーブルを読み書きするため、shared に置く。
package model

import (
	"time"

	"launchs/shared/apperror"
)

const (
	// ProjectIDPrefix は、プロジェクトの ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。
	ProjectIDPrefix = "project-"

	// ProjectStatusActive は、通常の状態のプロジェクト。
	ProjectStatusActive = "active"

	// ProjectStatusDeleting は、削除を受け付けて、実体を削除している最中のプロジェクト。
	ProjectStatusDeleting = "deleting"

	// ProjectStatusDeleteFailed は、削除に失敗して止まっているプロジェクト。
	// リトライの期限までに実体を削除できなかったときの状態で、もう一度の削除を受け付ける。
	ProjectStatusDeleteFailed = "delete_failed"
)

// Project は、projects テーブルの1行。build / deploy などは、全ていずれかのプロジェクトに属する。
type Project struct {
	// ID は、プロジェクトの ID("project-" + UUID v4)。
	ID string `gorm:"column:id;primaryKey"`

	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id"`

	// Name は、プロジェクトの名前。英数字とハイフンのみ、1〜64文字。
	Name string `gorm:"column:name"`

	// Status は、プロジェクトの状態(ProjectStatusActive、ProjectStatusDeleting、ProjectStatusDeleteFailed のどれか)。
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
	// このモデルが対応するテーブルの名前を返す。
	return "projects"
}

// CanBeDeleted は、プロジェクトの削除を受け付けられるかを返す。
//
// 戻り値:
//   - active、または削除に失敗して止まっている(delete_failed)ときは true。
func (project Project) CanBeDeleted() bool {
	// 稼働中と、削除失敗のときだけ受け付ける。
	return project.Status == ProjectStatusActive || project.Status == ProjectStatusDeleteFailed
}

// BeginDelete は、削除を受け付ける(状態を deleting にする)。
//
// 戻り値:
//   - 受け付ける前の状態(削除を始められなかったとき、元に戻すために使う)。
//   - 受け付けられない状態なら conflict。
func (project *Project) BeginDelete() (string, error) {
	// 受け付けられない状態なら、conflict。
	if !project.CanBeDeleted() {
		return "", apperror.New(apperror.KindConflict, "プロジェクトは "+project.Status+" のため、変更できません")
	}
	// 前の状態を覚えて、削除中にする。
	previousStatus := project.Status
	project.Status = ProjectStatusDeleting
	return previousStatus, nil
}

// RevertDeleting は、削除中のプロジェクトを、削除を受け付ける前の状態に戻す。
//
// 引数:
//   - previousStatus: 戻す先の状態(BeginDelete が返したもの)。
//
// 戻り値:
//   - 戻したら true。削除中でないときは、何も変えず false。
func (project *Project) RevertDeleting(previousStatus string) bool {
	// 削除中でなければ、戻すものが無い。
	if project.Status != ProjectStatusDeleting {
		return false
	}
	// 前の状態に戻す。
	project.Status = previousStatus
	return true
}

// MarkDeleteFailed は、削除中のプロジェクトを、削除失敗(delete_failed)にする。
//
// 戻り値:
//   - 変えたら true。削除中でないときは、何も変えず false。
func (project *Project) MarkDeleteFailed() bool {
	// 削除中でなければ、変えるものが無い。
	if project.Status != ProjectStatusDeleting {
		return false
	}
	// 削除失敗の状態にする。
	project.Status = ProjectStatusDeleteFailed
	return true
}
