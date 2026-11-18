package repository

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/templateplan"
)

// TemplateApplyRepository は、template_applies テーブルの読み書きを担当する。
// 状態の規則は model.TemplateApply が持ち、ここは「ロックして読む → モデルを呼ぶ → 保存する」を行う。
type TemplateApplyRepository struct {
	database *gorm.DB
}

// NewTemplateApplyRepository は、TemplateApplyRepository を作る。
//
// 引数:
//   - database: 使うデータベース。
//
// 戻り値:
//   - 作成した TemplateApplyRepository。
func NewTemplateApplyRepository(database *gorm.DB) *TemplateApplyRepository {
	// データベースを持たせて返す。
	return &TemplateApplyRepository{database: database}
}

// Create は、テンプレートの実行を保存する(status は running)。
//
// プロジェクトが、持ち主のものであること、active であることを、プロジェクトの行をロックして確かめる。
// ConfigMap の数が、プロジェクトの上限(MaxConfigMapsPerProject)を超えないことも、同じロックの中で確かめる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - apply: 保存する実行。ID、ProjectID、UserID、TemplateID、Values、Plan、Steps は、呼び出し側で設定しておく。
//   - newConfigMaps: この実行が作る ConfigMap の数。
//
// 戻り値:
//   - プロジェクトが無い、または他のユーザーのものなら not_found。
//     active でない、または ConfigMap の数が上限を超えるなら conflict。
func (templateApplyRepository *TemplateApplyRepository) Create(requestContext context.Context, apply *model.TemplateApply, newConfigMaps int) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(apply.ID, apply.ProjectID, apply.UserID, apply.TemplateID) {
		return newEmptyConditionError()
	}
	// プロジェクトの確認と、保存を、1つのトランザクションで行う。
	return templateApplyRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 持ち主のプロジェクトを、他から状態を変えられないよう、ロックして読む(ConfigMap の作成も、同じ行をロックするので、数が競合しない)。
		var project model.Project
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Project{ID: apply.ProjectID, UserID: apply.UserID}).Take(&project).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "プロジェクトが見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", findError)
		}
		if project.Status != model.ProjectStatusActive {
			return apperror.New(apperror.KindConflict, "プロジェクトは "+project.Status+" のため、テンプレートから作れません")
		}
		// ConfigMap の数が、上限を超えないかを確かめる。
		if newConfigMaps > 0 {
			var existing int64
			if countError := transaction.Model(&model.ConfigMap{}).Where(&model.ConfigMap{ProjectID: apply.ProjectID}).Count(&existing).Error; countError != nil {
				return apperror.Wrap(apperror.KindInternal, "ConfigMap の数を数えられませんでした", countError)
			}
			if int(existing)+newConfigMaps > model.MaxConfigMapsPerProject {
				return apperror.New(apperror.KindConflict, "ConfigMap の数が上限("+itoaForMessage(model.MaxConfigMapsPerProject)+")を超えます")
			}
		}
		// 実行中として保存する。
		apply.Status = model.TemplateApplyStatusRunning
		if createError := transaction.Create(apply).Error; createError != nil {
			return apperror.Wrap(apperror.KindInternal, "テンプレートの実行を保存できませんでした", createError)
		}
		return nil
	})
}

// FindByID は、ユーザーのプロジェクトの、テンプレートの実行を 1 件取得する。他のユーザーのものは、存在しないものとして扱う。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - applyID: 実行の ID。
//
// 戻り値:
//   - 実行。無い(他人のものを含む)ときは not_found。
func (templateApplyRepository *TemplateApplyRepository) FindByID(requestContext context.Context, userID string, projectID string, applyID string) (model.TemplateApply, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, applyID) {
		return model.TemplateApply{}, apperror.New(apperror.KindNotFound, "テンプレートの実行が見つかりません")
	}
	// 持ち主とプロジェクトの条件つきで探す。
	var apply model.TemplateApply
	findError := templateApplyRepository.database.WithContext(requestContext).
		Where(&model.TemplateApply{ID: applyID, ProjectID: projectID, UserID: userID}).Take(&apply).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.TemplateApply{}, apperror.Wrap(apperror.KindNotFound, "テンプレートの実行が見つかりません", findError)
	}
	if findError != nil {
		return model.TemplateApply{}, apperror.Wrap(apperror.KindInternal, "テンプレートの実行を取得できませんでした", findError)
	}
	return apply, nil
}

// FindForWorker は、持ち主を問わずに、テンプレートの実行を ID で 1 件取得する(ワーカー用)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - applyID: 実行の ID。
//
// 戻り値:
//   - 実行。無いときは not_found。
func (templateApplyRepository *TemplateApplyRepository) FindForWorker(requestContext context.Context, applyID string) (model.TemplateApply, error) {
	// 条件の値が空だと、Gorm が条件を無視して先頭の 1 件を返すので、先に拒否する。
	if hasEmpty(applyID) {
		return model.TemplateApply{}, apperror.New(apperror.KindNotFound, "テンプレートの実行が見つかりません")
	}
	// ID で探す。
	var apply model.TemplateApply
	findError := templateApplyRepository.database.WithContext(requestContext).Where(&model.TemplateApply{ID: applyID}).Take(&apply).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.TemplateApply{}, apperror.Wrap(apperror.KindNotFound, "テンプレートの実行が見つかりません", findError)
	}
	if findError != nil {
		return model.TemplateApply{}, apperror.Wrap(apperror.KindInternal, "テンプレートの実行を取得できませんでした", findError)
	}
	return apply, nil
}

// UpdateStep は、ステップ 1 つの進み具合を、書き換える(ワーカー用)。行をロックして、他のステップの値を壊さないようにする。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - applyID: 実行の ID。
//   - index: ステップの位置(計画と同じ順)。
//   - state: 新しい進み具合。
//
// 戻り値:
//   - 実行が無いときは not_found。位置が範囲外のときは internal_error。
func (templateApplyRepository *TemplateApplyRepository) UpdateStep(requestContext context.Context, applyID string, index int, state templateplan.StepState) error {
	// 条件の値が空だと、Gorm が条件を無視するので、先に拒否する。
	if hasEmpty(applyID) {
		return newEmptyConditionError()
	}
	return templateApplyRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む。
		var apply model.TemplateApply
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).Where(&model.TemplateApply{ID: applyID}).Take(&apply).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "テンプレートの実行が見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "テンプレートの実行を取得できませんでした", findError)
		}
		// 進み具合を読み込んで、1 つだけ書き換える。
		var states []templateplan.StepState
		if unmarshalError := json.Unmarshal(apply.Steps, &states); unmarshalError != nil {
			return apperror.Wrap(apperror.KindInternal, "進み具合を読み込めませんでした", unmarshalError)
		}
		if index < 0 || index >= len(states) {
			return apperror.New(apperror.KindInternal, "ステップの位置が範囲外です")
		}
		states[index] = state
		encoded, marshalError := json.Marshal(states)
		if marshalError != nil {
			return apperror.Wrap(apperror.KindInternal, "進み具合を保存できませんでした", marshalError)
		}
		// 保存する。
		if saveError := transaction.Model(&model.TemplateApply{}).Where("id = ?", applyID).
			Updates(map[string]any{"steps": encoded, "updated_at": gorm.Expr("now()")}).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "進み具合を保存できませんでした", saveError)
		}
		return nil
	})
}

// Finish は、テンプレートの実行を終える(結果を確定する。ワーカー用)。running のときだけ変わる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - applyID: 実行の ID。
//   - succeeded: 全て作れたか。
//   - failureMessage: 失敗の理由。
//
// 戻り値:
//   - 変えたら true(すでに終わっていれば false)。データベースのエラー。
func (templateApplyRepository *TemplateApplyRepository) Finish(requestContext context.Context, applyID string, succeeded bool, failureMessage string) (bool, error) {
	// モデルに、結果の確定を任せる。
	return transitionRow[model.TemplateApply](templateApplyRepository.database, requestContext, applyID, func(apply *model.TemplateApply) bool {
		return apply.Finish(succeeded, failureMessage)
	})
}
