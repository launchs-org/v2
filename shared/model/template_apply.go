package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	// TemplateApplyIDPrefix は、テンプレートの実行の ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。
	TemplateApplyIDPrefix = "apply-"

	// TemplateApplyStatusRunning は、部品を作っている最中の実行。
	TemplateApplyStatusRunning = "running"

	// TemplateApplyStatusSucceeded は、全ての部品を作り終えた実行。
	TemplateApplyStatusSucceeded = "succeeded"

	// TemplateApplyStatusFailed は、どれかの部品の作成に失敗して止まった実行(作れたものは、残っている)。
	TemplateApplyStatusFailed = "failed"
)

// TemplateApply は、template_applies テーブルの1行。テンプレートから部品を作る、1 回の実行。
type TemplateApply struct {
	// ID は、実行の ID("apply-" + UUID v4)。
	ID string `gorm:"column:id;primaryKey"`

	// ProjectID は、属するプロジェクトの ID。
	ProjectID string `gorm:"column:project_id"`

	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id"`

	// TemplateID は、使ったテンプレートの id。
	TemplateID string `gorm:"column:template_id"`

	// Status は、TemplateApplyStatus... のどれか。
	Status string `gorm:"column:status"`

	// Values は、全ての変数の値(JSON のオブジェクト。変数名 → 文字列)。
	Values datatypes.JSON `gorm:"column:values"`

	// Plan は、計画(templateplan.Plan の JSON)。
	Plan datatypes.JSON `gorm:"column:plan"`

	// Steps は、ステップごとの進み具合(templateplan.StepState の JSON の配列)。
	Steps datatypes.JSON `gorm:"column:steps"`

	// Error は、失敗の理由。失敗していなければ nil。
	Error *string `gorm:"column:error"`

	// CreatedAt は、作成日時。
	CreatedAt time.Time `gorm:"column:created_at"`

	// UpdatedAt は、更新日時。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("template_applies")。
func (TemplateApply) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "template_applies"
}

// IsFinished は、実行が終わっているか(成功か失敗か)を返す。
//
// 戻り値:
//   - 終わっていれば true。
func (apply TemplateApply) IsFinished() bool {
	// running 以外は、終わっている。
	return apply.Status != TemplateApplyStatusRunning
}

// Finish は、実行を終える(結果を確定する)。running のときだけ変わる。
//
// 引数:
//   - succeeded: 全て作れたか。
//   - failureMessage: 失敗の理由(失敗のときだけ使う)。
//
// 戻り値:
//   - 変えたら true。すでに終わっていれば、何も変えず false。
func (apply *TemplateApply) Finish(succeeded bool, failureMessage string) bool {
	// 終わっていれば、変えない。
	if apply.IsFinished() {
		return false
	}
	// 成功か失敗かで、状態と理由を決める。
	if succeeded {
		apply.Status = TemplateApplyStatusSucceeded
		apply.Error = nil
		return true
	}
	apply.Status = TemplateApplyStatusFailed
	apply.Error = &failureMessage
	return true
}
