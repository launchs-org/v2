package repository

import (
	"context"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// ListByProject は、プロジェクトの deploy を、作成日時の新しい順に、1ページ分取得する。
//
// 削除が完了した deploy は、レコードごと無いので、含まれない。削除中(deleting)や、削除に失敗して止まっている(delete_failed)ものは、含まれる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - limit: 1ページの件数。
//   - after: 2ページ目以降の、前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - deploy の一覧と、次のページの位置(最後のページなら nil)。
//   - プロジェクトが無い(他人のものを含む)ときは not_found。データベースのエラーは internal_error。
func (deployRepository *DeployRepository) ListByProject(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.Deploy, *pagination.Position, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する(見つからない扱い)。
	if hasEmpty(userID, projectID) {
		return nil, nil, apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// プロジェクトが、持ち主のものかを確かめる(他人のものは、存在しないものとして扱う)。
	var projectCount int64
	if countError := deployRepository.database.WithContext(requestContext).Model(&model.Project{}).
		Where(&model.Project{ID: projectID, UserID: userID}).Count(&projectCount).Error; countError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", countError)
	}
	if projectCount == 0 {
		return nil, nil, apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// 新しい順に並べる検索を組み立てる(次のページがあるかを知るため、1件多く読む)。一覧に要らない、環境変数などの大きな列は、読まない。
	query := deployRepository.database.WithContext(requestContext).
		Select("id", "project_id", "user_id", "image_ref", "spec", "replicas", "status", "tags", "created_at", "updated_at").
		Where(&model.Deploy{ProjectID: projectID}).
		Order("created_at DESC, id DESC").
		Limit(limit + 1)
	// 2ページ目以降なら、前のページの最後より後ろだけに絞る。
	if after != nil {
		query = query.Where("(created_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}
	// 検索を実行する。
	var deploys []model.Deploy
	if findError := query.Find(&deploys).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "deploy の一覧を取得できませんでした", findError)
	}
	// limit 件以下なら、次のページは無い。
	if len(deploys) <= limit {
		return deploys, nil, nil
	}
	// 確認のために多く読んだ1件を外し、次のページの位置を作る。
	deploys = deploys[:limit]
	lastDeploy := deploys[limit-1]
	return deploys, &pagination.Position{CreatedAt: lastDeploy.CreatedAt, ID: lastDeploy.ID}, nil
}
