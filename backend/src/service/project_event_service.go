package service

import (
	"context"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// ProjectEventStore は、ProjectEventService が必要とする、プロジェクトの k8s の Event の読み取り。
//
// 実体は shared/repository の ProjectEventRepository。テストでは fake に差し替える。
type ProjectEventStore interface {
	// ListEvents は、プロジェクトの Event を、最後に起きた時刻の新しい順に、1ページ分返す(持ち主の確認も行う)。
	ListEvents(requestContext context.Context, userID string, projectID string, filter model.ProjectEventFilter, limit int, after *pagination.Position) ([]model.ProjectEvent, *pagination.Position, error)
}

// ProjectEventService は、プロジェクトの k8s の Event を読むための、ビジネスルールを担当する。
type ProjectEventService struct {
	eventStore ProjectEventStore
}

// NewProjectEventService は、ProjectEventService を作る。
//
// 引数:
//   - eventStore: Event の読み取り先。
//
// 戻り値:
//   - 作成した ProjectEventService。
func NewProjectEventService(eventStore ProjectEventStore) *ProjectEventService {
	// 保存先を持たせて返す。
	return &ProjectEventService{eventStore: eventStore}
}

// List は、プロジェクトの Event を、最後に起きた時刻の新しい順に、1ページ分返す。
//
// 絞り込みの種類(type)は、Normal か Warning だけを受け付ける(空なら、絞らない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - filter: 絞り込み(種類、deploy、理由)。
//   - limit: 1ページの件数。
//   - after: 2ページ目以降の、前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - Event の一覧と、次のページの位置(最後のページなら nil)。
//   - 種類が不正なら invalid_request。プロジェクトが無い、または他のユーザーのものなら not_found。
func (eventService *ProjectEventService) List(requestContext context.Context, userID string, projectID string, filter model.ProjectEventFilter, limit int, after *pagination.Position) ([]model.ProjectEvent, *pagination.Position, error) {
	// 種類は、決まった 2 つだけを許す(空は、絞らない)。
	if filter.Type != "" && filter.Type != model.ProjectEventTypeNormal && filter.Type != model.ProjectEventTypeWarning {
		return nil, nil, apperror.New(apperror.KindInvalidRequest, "type は Normal か Warning で指定してください")
	}
	// 保存先に任せる(持ち主の確認も、保存先が行う)。
	return eventService.eventStore.ListEvents(requestContext, userID, projectID, filter, limit, after)
}
