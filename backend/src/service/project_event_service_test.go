package service

import (
	"context"
	"testing"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// fakeProjectEventStore は、呼ばれた引数を記録し、決めた結果を返す、テスト用の保存先。
type fakeProjectEventStore struct {
	// 呼ばれた回数と、最後に渡された引数。
	calls   int
	userID  string
	project string
	filter  model.ProjectEventFilter
	limit   int
	after   *pagination.Position
	// 返す結果。
	events []model.ProjectEvent
	next   *pagination.Position
	err    error
}

// ListEvents は、引数を記録して、決めた結果を返す。
//
// 引数:
//   - requestContext: 使わない。
//   - userID, projectID, filter, limit, after: 記録する。
//
// 戻り値:
//   - 決めておいた Event、次の位置、エラー。
func (store *fakeProjectEventStore) ListEvents(requestContext context.Context, userID string, projectID string, filter model.ProjectEventFilter, limit int, after *pagination.Position) ([]model.ProjectEvent, *pagination.Position, error) {
	// 引数を記録する。
	store.calls++
	store.userID, store.project, store.filter, store.limit, store.after = userID, projectID, filter, limit, after
	return store.events, store.next, store.err
}

// TestProjectEventServiceList は、種類の検査と、引数・結果をそのまま保存先に渡すことを確認する。
func TestProjectEventServiceList(testingT *testing.T) {
	testingT.Run("引数をそのまま保存先に渡し、結果をそのまま返す", func(subTest *testing.T) {
		// 次の位置と、1件の Event を返す fake を用意する。
		position := &pagination.Position{CreatedAt: time.Now(), ID: "7"}
		store := &fakeProjectEventStore{events: []model.ProjectEvent{{ID: 7, EventUID: "uid"}}, next: position}
		eventService := NewProjectEventService(store)
		after := &pagination.Position{CreatedAt: time.Now(), ID: "9"}
		filter := model.ProjectEventFilter{Type: model.ProjectEventTypeWarning, DeployID: "deploy-a", Reason: "BackOff"}
		events, next, listError := eventService.List(context.Background(), "user-a", "project-a", filter, 30, after)
		if listError != nil || len(events) != 1 || next != position {
			subTest.Fatalf("結果がそのまま返るはず: %v %v %v", events, next, listError)
		}
		if store.userID != "user-a" || store.project != "project-a" || store.filter != filter || store.limit != 30 || store.after != after {
			subTest.Errorf("引数が、そのまま渡るはず: %+v", store)
		}
	})
	testingT.Run("種類は、Normal、Warning、空だけを受け付ける", func(subTest *testing.T) {
		for _, accepted := range []string{"", "Normal", "Warning"} {
			store := &fakeProjectEventStore{}
			if _, _, listError := NewProjectEventService(store).List(context.Background(), "u", "p", model.ProjectEventFilter{Type: accepted}, 10, nil); listError != nil || store.calls != 1 {
				subTest.Errorf("%q は通るはず: %v", accepted, listError)
			}
		}
		for _, rejected := range []string{"warning", "Error", "NORMAL", " Normal"} {
			store := &fakeProjectEventStore{}
			_, _, listError := NewProjectEventService(store).List(context.Background(), "u", "p", model.ProjectEventFilter{Type: rejected}, 10, nil)
			if apperror.KindOf(listError) != apperror.KindInvalidRequest || store.calls != 0 {
				subTest.Errorf("%q は invalid_request で、保存先を呼ばないはず: %v 呼び出し %d", rejected, listError, store.calls)
			}
		}
	})
	testingT.Run("保存先のエラーは、そのまま返る(not_found など)", func(subTest *testing.T) {
		store := &fakeProjectEventStore{err: apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")}
		_, _, listError := NewProjectEventService(store).List(context.Background(), "u", "p", model.ProjectEventFilter{}, 10, nil)
		if apperror.KindOf(listError) != apperror.KindNotFound {
			subTest.Errorf("not_found のはず: %v", listError)
		}
	})
}
