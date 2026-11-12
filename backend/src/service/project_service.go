// Package service は、バックエンドのビジネスルールを担当する。
//
// HTTP のことは知らず、DB の読み書きは repository に任せる。
package service

import (
	"context"

	"github.com/google/uuid"

	"launchs/shared/model"
	"launchs/shared/pagination"
)

// ProjectStore は、ProjectService が必要とする、プロジェクトの保存先の操作。
//
// 実体は shared/repository の ProjectRepository。テストでは fake に差し替える。
type ProjectStore interface {
	Create(requestContext context.Context, project *model.Project) error
	FindByID(requestContext context.Context, userID string, projectID string) (model.Project, error)
	ListByUser(requestContext context.Context, userID string, limit int, after *pagination.Position) ([]model.Project, *pagination.Position, error)
	Rename(requestContext context.Context, userID string, projectID string, newName string) error
}

// ProjectService は、プロジェクトに関する操作のビジネスルールを担当する。
type ProjectService struct {
	projectStore ProjectStore
}

// NewProjectService は、ProjectService を作る。
//
// 引数:
//   - projectStore: プロジェクトの保存先。
//
// 戻り値:
//   - 作成した ProjectService。
func NewProjectService(projectStore ProjectStore) *ProjectService {
	return &ProjectService{projectStore: projectStore}
}

// Create は、ユーザーのプロジェクトを新しく作る。
//
// 処理の流れ: ID("project-" + UUID v4)を作り、active 状態で保存する。
// 名前の形式の検証は、呼び出し側(controller)で済んでいる前提。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - name: プロジェクトの名前。
//
// 戻り値:
//   - 作成したプロジェクトの ID。
//   - 同じユーザーの中で名前が重複したときの conflict など、保存のエラー。
func (projectService *ProjectService) Create(requestContext context.Context, userID string, name string) (string, error) {
	project := &model.Project{
		ID:     model.ProjectIDPrefix + uuid.NewString(),
		UserID: userID,
		Name:   name,
		Status: model.ProjectStatusActive,
	}
	if createError := projectService.projectStore.Create(requestContext, project); createError != nil {
		return "", createError
	}
	return project.ID, nil
}

// Get は、ユーザーのプロジェクトを1件取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 取得するプロジェクトの ID。
//
// 戻り値:
//   - プロジェクト。
//   - 無い、または他のユーザーのものなら not_found。
func (projectService *ProjectService) Get(requestContext context.Context, userID string, projectID string) (model.Project, error) {
	return projectService.projectStore.FindByID(requestContext, userID, projectID)
}

// List は、ユーザーのプロジェクトを、作成日時の新しい順に1ページ分取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - limit: 1ページの最大件数。
//   - after: 前のページの最後の位置。最初のページなら nil。
//
// 戻り値:
//   - プロジェクトの一覧。
//   - 次のページがあるときの、最後の行の位置。最後のページなら nil。
//   - 取得のエラー。
func (projectService *ProjectService) List(requestContext context.Context, userID string, limit int, after *pagination.Position) ([]model.Project, *pagination.Position, error) {
	return projectService.projectStore.ListByUser(requestContext, userID, limit, after)
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
//   - 削除中、または名前の重複なら conflict。
func (projectService *ProjectService) Rename(requestContext context.Context, userID string, projectID string, newName string) error {
	return projectService.projectStore.Rename(requestContext, userID, projectID, newName)
}
