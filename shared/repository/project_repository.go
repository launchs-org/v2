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
	// 受け取ったデータベースの接続を持たせた、リポジトリを返す。
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
	// context を渡して、プロジェクトを INSERT する。
	createError := projectRepository.database.WithContext(requestContext).Create(project).Error
	// 一意制約の違反は、名前の重複なので、利用者に伝えられる conflict にする。
	if errors.Is(createError, gorm.ErrDuplicatedKey) {
		return apperror.Wrap(apperror.KindConflict, "同じ名前のプロジェクトがすでにあります", createError)
	}
	// それ以外のエラーは、利用者には詳細を見せない internal にする。
	if createError != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトを保存できませんでした", createError)
	}
	// 保存できたので、エラー無しで返す。
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
	// 取得した行を入れる変数。
	var project model.Project
	// ID と持ち主の両方で絞って1件取る。持ち主も条件に入れて、他人のものを見せない。
	findError := projectRepository.database.WithContext(requestContext).
		Where(&model.Project{ID: projectID, UserID: userID}).
		Take(&project).Error
	// 行が無いときは、他人のものも含めて、not_found にする。
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Project{}, apperror.Wrap(apperror.KindNotFound, "プロジェクトが見つかりません", findError)
	}
	// それ以外のエラーは、internal にする。
	if findError != nil {
		return model.Project{}, apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", findError)
	}
	// 見つかったプロジェクトを返す。
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
	// 自分のプロジェクトを、新しい順に並べる検索を組み立てる。
	query := projectRepository.database.WithContext(requestContext).
		Where(&model.Project{UserID: userID}).
		Order("created_at DESC, id DESC").
		// 次のページがあるかを知るため、1件多く読む。
		Limit(limit + 1)
	// 2ページ目以降なら、前のページの最後より後ろ(古い側)だけに絞る。
	if after != nil {
		// 2列の組の比較は、構造体では書けないので、ここだけ SQL の文字列で書く。
		query = query.Where("(created_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}

	// 取得した行を入れる変数。
	var projects []model.Project
	// 検索を実行する。失敗したときは、internal にして返す。
	if findError := query.Find(&projects).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "プロジェクトの一覧を取得できませんでした", findError)
	}

	// limit 件以下なら、次のページは無いので、位置は nil で返す。
	if len(projects) <= limit {
		return projects, nil, nil
	}
	// 確認のために多く読んだ1件を、返す一覧から外す。
	projects = projects[:limit]
	// 次のページの開始位置の元になる、このページの最後の行を取る。
	lastProject := projects[limit-1]
	// 一覧と、次のページの位置を返す。
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
	// active のものだけを対象に、名前を UPDATE する。削除中のものは変えない。
	updateResult := projectRepository.database.WithContext(requestContext).
		Model(&model.Project{}).
		Where(&model.Project{ID: projectID, UserID: userID, Status: model.ProjectStatusActive}).
		// updated_at は、Gorm が更新日時に自動で入れる。
		Updates(&model.Project{Name: newName})
	// 一意制約の違反は、名前の重複なので、conflict にする。
	if errors.Is(updateResult.Error, gorm.ErrDuplicatedKey) {
		return apperror.Wrap(apperror.KindConflict, "同じ名前のプロジェクトがすでにあります", updateResult.Error)
	}
	// それ以外のエラーは、internal にする。
	if updateResult.Error != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトの名前を変更できませんでした", updateResult.Error)
	}
	// 更新された行が無いときは、無いのか、削除中なのかを調べて、理由のエラーを返す。
	if updateResult.RowsAffected == 0 {
		return projectRepository.explainNoUpdate(requestContext, userID, projectID)
	}
	// 名前を変えられたので、エラー無しで返す。
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
	// active のものだけを対象に、status を deleting に UPDATE する。二重の削除を防ぐ。
	updateResult := projectRepository.database.WithContext(requestContext).
		Model(&model.Project{}).
		Where(&model.Project{ID: projectID, UserID: userID, Status: model.ProjectStatusActive}).
		Updates(&model.Project{Status: model.ProjectStatusDeleting})
	// データベースのエラーは、internal にする。
	if updateResult.Error != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトを削除中にできませんでした", updateResult.Error)
	}
	// 更新された行が無いときは、無いのか、すでに削除中なのかを調べて、理由のエラーを返す。
	if updateResult.RowsAffected == 0 {
		return projectRepository.explainNoUpdate(requestContext, userID, projectID)
	}
	// 削除中にできたので、エラー無しで返す。
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
	// 持ち主のプロジェクトとして取得し、存在するかを調べる。
	project, findError := projectRepository.FindByID(requestContext, userID, projectID)
	// 存在しない(または他人の)ときは、not_found などのエラーをそのまま返す。
	if findError != nil {
		return findError
	}
	// 存在するのに更新されなかったのは、status のためなので、その status を伝える conflict を返す。
	return apperror.New(apperror.KindConflict, "プロジェクトは "+project.Status+" のため、変更できません")
}

// RevertDeleting は、削除中(deleting)にしたプロジェクトを、通常(active)に戻す。
//
// 削除の Workflow を開始できなかったときに、プロジェクトが削除中のまま残らないよう、
// 状態を戻すために使う。すでに active のとき、または無いときは、何もしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 状態を戻すプロジェクトの ID。
//
// 戻り値:
//   - データベースのエラー。
func (projectRepository *ProjectRepository) RevertDeleting(requestContext context.Context, userID string, projectID string) error {
	// 持ち主の、削除中のプロジェクトだけを対象にして、active に戻す(更新日時は Gorm が更新する)。
	updateResult := projectRepository.database.WithContext(requestContext).
		Model(&model.Project{}).
		Where(&model.Project{ID: projectID, UserID: userID, Status: model.ProjectStatusDeleting}).
		Updates(&model.Project{Status: model.ProjectStatusActive})
	// データベースのエラーは、内部エラーにして返す。
	if updateResult.Error != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトを通常の状態に戻せませんでした", updateResult.Error)
	}
	return nil
}

// DeleteRecord は、プロジェクトの行を物理削除する。削除の Workflow が、最後に使う。
//
// 繰り返し実行されても壊れないよう、行がすでに無くてもエラーにしない。
// 削除中(deleting)でないプロジェクトは、消さない(誤って消さないための安全装置)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - projectID: 削除するプロジェクトの ID。
//
// 戻り値:
//   - データベースのエラー。
func (projectRepository *ProjectRepository) DeleteRecord(requestContext context.Context, projectID string) error {
	// 削除中のプロジェクトだけを、物理削除する(無ければ 0 件で、エラーにはならない)。
	deleteError := projectRepository.database.WithContext(requestContext).
		Where(&model.Project{ID: projectID, Status: model.ProjectStatusDeleting}).
		Delete(&model.Project{}).Error
	// データベースのエラーは、内部エラーにして返す。
	if deleteError != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトを削除できませんでした", deleteError)
	}
	return nil
}
