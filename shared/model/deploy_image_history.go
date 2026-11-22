package model

import "time"

// イメージの更新履歴の、反映の結果。
const (
	// ImageHistoryResultApplied は、反映に成功したことを表す。
	ImageHistoryResultApplied = "applied"

	// ImageHistoryResultFailed は、反映に失敗したことを表す(試したイメージは、pending に残る)。
	ImageHistoryResultFailed = "failed"
)

// DeployImageHistory は、deploy_image_history テーブルの1行。イメージを入れ替える反映の、1回分の記録。
type DeployImageHistory struct {
	// ID は、行の連番(主キー)。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`

	// DeployID は、対象の deploy の ID。
	DeployID string `gorm:"column:deploy_id"`

	// PreviousImageRef は、入れ替える前のイメージ。
	PreviousImageRef string `gorm:"column:previous_image_ref"`

	// ImageRef は、入れ替えようとしたイメージ。
	ImageRef string `gorm:"column:image_ref"`

	// Result は、反映の結果(ImageHistoryResult... のどれか)。
	Result string `gorm:"column:result"`

	// CreatedAt は、結果が確定した日時。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName は、このモデルのテーブル名を返す。
//
// 戻り値:
//   - テーブル名。
func (DeployImageHistory) TableName() string {
	// テーブル名を返す。
	return "deploy_image_history"
}
