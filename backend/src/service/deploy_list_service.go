package service

import (
	"context"

	"launchs/shared/model"
	"launchs/shared/pagination"
)

// List は、プロジェクトの deploy を、作成日時の新しい順に、1ページ分返す。
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
//   - プロジェクトが無い、または他のユーザーのものなら not_found。
func (deployService *DeployService) List(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.Deploy, *pagination.Position, error) {
	// 保存先に任せる(持ち主の確認も、保存先が行う)。
	return deployService.deployStore.ListByProject(requestContext, userID, projectID, limit, after)
}
