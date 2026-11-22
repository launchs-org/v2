package model

import (
	"time"

	"gorm.io/datatypes"
)

// APITokenIDPrefix は、API トークンの ID(jti)の接頭辞。
const APITokenIDPrefix = "apitoken-"

// APIToken は、api_tokens テーブルの1行。API トークンの有効・無効の管理(本体は保存しない)。
type APIToken struct {
	// ID は、トークンの jti。
	ID string `gorm:"column:id;primaryKey"`

	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id"`

	// Name は、利用者が付けた名前。
	Name string `gorm:"column:name"`

	// Scope は、許す操作の写し(JSON の配列)。
	Scope datatypes.JSON `gorm:"column:scope"`

	// Deploys は、対象の deploy の ID の写し(JSON の配列)。
	Deploys datatypes.JSON `gorm:"column:deploys"`

	// Projects は、対象のプロジェクトの ID の写し(JSON の配列)。
	Projects datatypes.JSON `gorm:"column:projects"`

	// ExpiresAt は、期限。nil なら無期限。
	ExpiresAt *time.Time `gorm:"column:expires_at"`

	// RevokedAt は、失効した日時。nil なら有効。
	RevokedAt *time.Time `gorm:"column:revoked_at"`

	// LastUsedAt は、最後に使った日時。
	LastUsedAt *time.Time `gorm:"column:last_used_at"`

	// CreatedAt は、作成日時。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("api_tokens")。
func (APIToken) TableName() string {
	return "api_tokens"
}
