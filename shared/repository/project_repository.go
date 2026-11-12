// Package repository は、データベースの読み書きを担当する。
//
// バックエンドの API と、ワーカーの Activity の両方が同じテーブルを扱うため、shared に置く。
// ビジネスルールは書かず、SQL の実行と、データベースのエラーの変換だけを行う。
package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// ProjectRepository は、projects テーブルの読み書きを担当する。
type ProjectRepository struct {
	database *gorm.DB
}

// NewProjectRepository は、ProjectRepository を作る。
//
// 引数:
//   - database: 使うデータベースの接続。
//
// 戻り値:
//   - 作成した ProjectRepository。
func NewProjectRepository(database *gorm.DB) *ProjectRepository {
	return &ProjectRepository{database: database}
}

// Create は、プロジェクトを1件保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - project: 保存するプロジェクト。ID は、呼び出し側で作っておく。
//
// 戻り値:
//   - 同じユーザーの中で名前が重複したときの、conflict のエラー。
//   - それ以外のデータベースのエラーは、internal_error。
func (projectRepository *ProjectRepository) Create(requestContext context.Context, project *model.Project) error {
	createError := projectRepository.database.WithContext(requestContext).Create(project).Error
	if errors.Is(createError, gorm.ErrDuplicatedKey) {
		return apperror.Wrap(apperror.KindConflict, "同じ名前のプロジェクトがすでにあります", createError)
	}
	if createError != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトを保存できなかった", createError)
	}
	return nil
}

// FindByID は、ユーザーのプロジェクトを1件取得する。
//
// 他のユーザーのプロジェクトは、存在しないものとして扱う(共通ルール)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 取得するプロジェクトの ID。
//
// 戻り値:
//   - 見つかったプロジェクト。
//   - 無い、または他のユーザーのものだったときの、not_found のエラー。
func (projectRepository *ProjectRepository) FindByID(requestContext context.Context, userID string, projectID string) (model.Project, error) {
	var project model.Project
	findError := projectRepository.database.WithContext(requestContext).
		Where("id = ? AND user_id = ?", projectID, userID).
		Take(&project).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Project{}, apperror.Wrap(apperror.KindNotFound, "プロジェクトが見つかりません", findError)
	}
	if findError != nil {
		return model.Project{}, apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できなかった", findError)
	}
	return project, nil
}

// ListByUser は、ユーザーのプロジェクトを、作成日時の新しい順に1ページ分取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - limit: 1ページの最大件数。
//   - after: 前のページの最後の位置。最初のページなら nil。
//
// 戻り値:
//   - プロジェクトの一覧(最大 limit 件)。
//   - 次のページがあるときは、その開始の位置の元になる、最後の行の位置。最後のページなら nil。
//   - データベースのエラー。
func (projectRepository *ProjectRepository) ListByUser(requestContext context.Context, userID string, limit int, after *pagination.Position) ([]model.Project, *pagination.Position, error) {
	query := projectRepository.database.WithContext(requestContext).
		Where("user_id = ?", userID).
		Order("created_at DESC, id DESC").
		// 次のページがあるかを知るため、1件多く読む。
		Limit(limit + 1)
	if after != nil {
		query = query.Where("(created_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}

	var projects []model.Project
	if findError := query.Find(&projects).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "プロジェクトの一覧を取得できなかった", findError)
	}

	if len(projects) <= limit {
		return projects, nil, nil
	}
	projects = projects[:limit]
	lastProject := projects[limit-1]
	return projects, &pagination.Position{CreatedAt: lastProject.CreatedAt, ID: lastProject.ID}, nil
}

// Rename は、プロジェクトの名前を変える。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 名前を変えるプロジェクトの ID。
//   - newName: 新しい名前。
//
// 戻り値:
//   - 無い、または他のユーザーのものなら not_found。
//   - 削除中(deleting)なら conflict。
//   - 同じユーザーの中で名前が重複したら conflict。
func (projectRepository *ProjectRepository) Rename(requestContext context.Context, userID string, projectID string, newName string) error {
	updateResult := projectRepository.database.WithContext(requestContext).
		Model(&model.Project{}).
		Where("id = ? AND user_id = ? AND status = ?", projectID, userID, model.ProjectStatusActive).
		Updates(map[string]any{"name": newName, "updated_at": gorm.Expr("now()")})
	if errors.Is(updateResult.Error, gorm.ErrDuplicatedKey) {
		return apperror.Wrap(apperror.KindConflict, "同じ名前のプロジェクトがすでにあります", updateResult.Error)
	}
	if updateResult.Error != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトの名前を変更できなかった", updateResult.Error)
	}
	if updateResult.RowsAffected == 0 {
		return projectRepository.explainNoUpdate(requestContext, userID, projectID)
	}
	return nil
}

// MarkDeleting は、プロジェクトの状態を、削除中(deleting)にする。
//
// 実体の削除は、ここでは行わない(Workflow が非同期に行う)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 削除するプロジェクトの ID。
//
// 戻り値:
//   - 無い、または他のユーザーのものなら not_found。
//   - すでに削除中なら conflict。
func (projectRepository *ProjectRepository) MarkDeleting(requestContext context.Context, userID string, projectID string) error {
	updateResult := projectRepository.database.WithContext(requestContext).
		Model(&model.Project{}).
		Where("id = ? AND user_id = ? AND status = ?", projectID, userID, model.ProjectStatusActive).
		Updates(map[string]any{"status": model.ProjectStatusDeleting, "updated_at": gorm.Expr("now()")})
	if updateResult.Error != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトを削除中にできなかった", updateResult.Error)
	}
	if updateResult.RowsAffected == 0 {
		return projectRepository.explainNoUpdate(requestContext, userID, projectID)
	}
	return nil
}

// explainNoUpdate は、更新の対象が0件だったとき、その理由をエラーにする。
//
// 存在しない(他のユーザーのものを含む)なら not_found、存在するが active でないなら conflict。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 更新しようとしたプロジェクトの ID。
//
// 戻り値:
//   - 理由に対応したエラー。
func (projectRepository *ProjectRepository) explainNoUpdate(requestContext context.Context, userID string, projectID string) error {
	project, findError := projectRepository.FindByID(requestContext, userID, projectID)
	if findError != nil {
		return findError
	}
	return apperror.New(apperror.KindConflict, "プロジェクトは "+project.Status+" のため、変更できません")
}
