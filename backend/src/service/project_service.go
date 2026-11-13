// Package service は、バックエンドのビジネスルールを担当する。
//
// HTTP のことは知らず、DB の読み書きは repository に任せる。
package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"launchs/shared/apperror"
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
	MarkDeleting(requestContext context.Context, userID string, projectID string) error
	RevertDeleting(requestContext context.Context, userID string, projectID string) error
}

// ProjectDeletionStarter は、プロジェクトの実体を削除する処理(Workflow)を開始する。
//
// 実体は TemporalProjectDeletionStarter。テストでは fake に差し替える。
type ProjectDeletionStarter interface {
	// StartProjectDeletion は、削除の処理を開始する。完了は待たない。
	StartProjectDeletion(requestContext context.Context, projectID string) error
}

// ProjectService は、プロジェクトに関する操作のビジネスルールを担当する。
type ProjectService struct {
	projectStore           ProjectStore
	projectDeletionStarter ProjectDeletionStarter
}

// NewProjectService は、ProjectService を作る。
//
// 引数:
//   - projectStore: プロジェクトの保存先。
//   - projectDeletionStarter: プロジェクトの実体の削除を開始するもの。
//
// 戻り値:
//   - 作成した ProjectService。
func NewProjectService(projectStore ProjectStore, projectDeletionStarter ProjectDeletionStarter) *ProjectService {
	// 保存先と、削除の開始を持たせた ProjectService を返す。
	return &ProjectService{projectStore: projectStore, projectDeletionStarter: projectDeletionStarter}
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
	// ID は接頭辞と UUID v4 で作り、状態は active にして、保存するモデルを組み立てる。
	project := &model.Project{
		ID:     model.ProjectIDPrefix + uuid.NewString(),
		UserID: userID,
		Name:   name,
		Status: model.ProjectStatusActive,
	}
	// プロジェクトを保存する。名前の重複などは、保存先がエラーで返す。
	if createError := projectService.projectStore.Create(requestContext, project); createError != nil {
		// 保存に失敗したので、ID は返さず、エラーだけ返す。
		return "", createError
	}
	// 保存できたので、作ったプロジェクトの ID を返す。
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
	// ユーザーとプロジェクト ID の両方で探す。他のユーザーのものは、見つからない扱いになる。
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
	// 保存先に、ユーザーの1ページ分の取得を任せ、結果をそのまま返す。
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
	// 保存先に、名前の変更を任せ、結果(not_found や conflict)をそのまま返す。
	return projectService.projectStore.Rename(requestContext, userID, projectID, newName)
}

// Delete は、プロジェクトの削除を受け付ける。実体の削除は、非同期に行われる。
//
// 処理の流れ:
//  1. プロジェクトの状態を、削除中(deleting)にする(以後の変更は 409 になる)
//  2. 実体を削除する Workflow を開始する(完了は待たない)
//  3. 手順 2 に失敗したら、状態を active に戻す(削除中のまま残らないようにする)
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 削除するプロジェクトの ID。
//
// 戻り値:
//   - 無い、または他のユーザーのものなら not_found。すでに削除中なら conflict。
//   - Workflow を開始できなかったときの internal_error(状態は戻してある)。
func (projectService *ProjectService) Delete(requestContext context.Context, userID string, projectID string) error {
	// 1. 状態を削除中にする(無い・他人のもの・すでに削除中は、ここでエラーになる)。
	if markError := projectService.projectStore.MarkDeleting(requestContext, userID, projectID); markError != nil {
		return markError
	}

	// 2. 実体を削除する Workflow を開始する。
	startError := projectService.projectDeletionStarter.StartProjectDeletion(requestContext, projectID)
	if startError == nil {
		return nil
	}

	// 3. 開始できなかったので、状態を戻す。リクエストが中断されていても戻せるよう、context を切り離す。
	revertContext := context.WithoutCancel(requestContext)
	if revertError := projectService.projectStore.RevertDeleting(revertContext, userID, projectID); revertError != nil {
		// 戻すのにも失敗したときは、両方の原因を残す(プロジェクトは削除中のまま残る)。
		return apperror.Wrap(apperror.KindInternal, "プロジェクトの削除を開始できませんでした", errors.Join(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "プロジェクトの削除を開始できませんでした", startError)
}
