package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"gorm.io/gorm"
	"launchs/shared/apperror"
	"launchs/shared/database/testdatabase"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// newProjectEventTestEnvironment は、プロジェクトを2つ(user-a の project-00000001 と、user-b の project-00000002)持つ、使い捨てのデータベースと、リポジトリを作る。
//
// 引数:
//   - testingT: テストの状態。
//
// 戻り値:
//   - データベースと、ProjectEventRepository。
func newProjectEventTestEnvironment(testingT *testing.T) (*gorm.DB, *ProjectEventRepository) {
	// 使い捨てのデータベースに、プロジェクトを2件作る。
	database := testdatabase.New(testingT)
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))
	mustCreate(testingT, database, newTestProject(2, "user-b", time.Now()))
	return database, NewProjectEventRepository(database)
}

// newTestProjectEvent は、テスト用の Event を作る。
//
// 引数:
//   - projectID: プロジェクトの ID。
//   - uid: k8s の Event の UID。
//   - lastSeen: 最後に起きた時刻。
//
// 戻り値:
//   - Normal の Event(最初に起きた時刻は、最後の 1 分前)。
func newTestProjectEvent(projectID string, uid string, lastSeen time.Time) model.ProjectEvent {
	// 必須の項目を埋めた Event を返す。
	return model.ProjectEvent{ProjectID: projectID, EventUID: uid, Type: model.ProjectEventTypeNormal, Reason: "Pulling", Message: "Pulling image", ObjectKind: "Pod", ObjectName: "pod-1", Count: 1, FirstSeenAt: lastSeen.Add(-time.Minute), LastSeenAt: lastSeen}
}

// TestProjectEventRepositoryUpsertEvent は、同じ UID の Event が、1 行にまとまり、回数と時刻だけが進む(戻らない)ことを確認する。
func TestProjectEventRepositoryUpsertEvent(testingT *testing.T) {
	// 環境と、基準の時刻を用意する。
	database, eventRepository := newProjectEventTestEnvironment(testingT)
	base := time.Now().Truncate(time.Second)
	count := func() int64 {
		var total int64
		database.Table("project_events").Count(&total)
		return total
	}

	testingT.Run("同じ UID は 1 行にまとまり、回数と最後の時刻が、新しい値になる", func(subTest *testing.T) {
		first := newTestProjectEvent("project-00000001", "uid-1", base)
		second := first
		second.Count = 5
		second.LastSeenAt = base.Add(time.Minute)
		second.Message = "Back-off pulling image"
		if upsertError := eventRepository.UpsertEvent(context.Background(), first); upsertError != nil {
			subTest.Fatal(upsertError)
		}
		if upsertError := eventRepository.UpsertEvent(context.Background(), second); upsertError != nil {
			subTest.Fatal(upsertError)
		}
		var saved model.ProjectEvent
		database.First(&saved, "event_uid = ?", "uid-1")
		if count() != 1 || saved.Count != 5 || saved.Message != "Back-off pulling image" || !saved.LastSeenAt.Equal(base.Add(time.Minute)) {
			subTest.Errorf("1行で、新しい値になるはず: %d行 %+v", count(), saved)
		}
	})
	testingT.Run("古い値(回数が少なく、時刻が古い)で上書きしても、戻らない(順序が入れ替わっても壊れない)", func(subTest *testing.T) {
		stale := newTestProjectEvent("project-00000001", "uid-1", base.Add(-time.Hour))
		stale.Count = 2
		if upsertError := eventRepository.UpsertEvent(context.Background(), stale); upsertError != nil {
			subTest.Fatal(upsertError)
		}
		var saved model.ProjectEvent
		database.First(&saved, "event_uid = ?", "uid-1")
		if saved.Count != 5 || !saved.LastSeenAt.Equal(base.Add(time.Minute)) {
			subTest.Errorf("回数と最後の時刻は、戻らないはず: %+v", saved)
		}
	})
	testingT.Run("必須の項目が空なら、保存しない", func(subTest *testing.T) {
		empty := newTestProjectEvent("", "uid-2", base)
		if upsertError := eventRepository.UpsertEvent(context.Background(), empty); upsertError == nil || count() != 1 {
			subTest.Errorf("拒否されるはず: %v %d行", upsertError, count())
		}
	})
}

// TestProjectEventRepositoryListEvents は、新しい順、ページング(重複も欠落も無い)、絞り込み、持ち主の確認を確認する。
func TestProjectEventRepositoryListEvents(testingT *testing.T) {
	// project-00000001 に、時刻が同じものを含む 5 件(deploy-a が 2 件、Warning が 1 件)、別のプロジェクトに 1 件。
	_, eventRepository := newProjectEventTestEnvironment(testingT)
	base := time.Now().Truncate(time.Second)
	for index := 1; index <= 5; index++ {
		event := newTestProjectEvent("project-00000001", "uid-"+strconv.Itoa(index), base.Add(time.Duration(index/2)*time.Second))
		if index <= 2 {
			event.DeployID = "deploy-a"
		}
		if index == 3 {
			event.Type = model.ProjectEventTypeWarning
			event.Reason = "BackOff"
		}
		if upsertError := eventRepository.UpsertEvent(context.Background(), event); upsertError != nil {
			testingT.Fatal(upsertError)
		}
	}
	if upsertError := eventRepository.UpsertEvent(context.Background(), newTestProjectEvent("project-00000002", "uid-other", base)); upsertError != nil {
		testingT.Fatal(upsertError)
	}

	testingT.Run("新しい順に返し、他のプロジェクトのものは含まない", func(subTest *testing.T) {
		events, next, listError := eventRepository.ListEvents(context.Background(), "user-a", "project-00000001", model.ProjectEventFilter{}, 10, nil)
		if listError != nil || next != nil || len(events) != 5 {
			subTest.Fatalf("5件で、続きは無いはず: %d件 %v %v", len(events), next, listError)
		}
		for index := 1; index < len(events); index++ {
			if events[index].LastSeenAt.After(events[index-1].LastSeenAt) {
				subTest.Errorf("新しい順のはず: %v", events)
			}
		}
	})
	testingT.Run("ページをまたいでも、重複も欠落も無い(同じ時刻の行を含む)", func(subTest *testing.T) {
		seen := map[string]bool{}
		var cursor *pagination.Position
		for page := 0; page < 10; page++ {
			events, next, listError := eventRepository.ListEvents(context.Background(), "user-a", "project-00000001", model.ProjectEventFilter{}, 2, cursor)
			if listError != nil {
				subTest.Fatal(listError)
			}
			for _, event := range events {
				if seen[event.EventUID] {
					subTest.Fatalf("重複: %s", event.EventUID)
				}
				seen[event.EventUID] = true
			}
			if next == nil {
				break
			}
			cursor = next
		}
		if len(seen) != 5 {
			subTest.Errorf("5件全部が出るはず: %d件", len(seen))
		}
	})
	testingT.Run("種類、deploy、理由で絞り込める", func(subTest *testing.T) {
		warnings, _, _ := eventRepository.ListEvents(context.Background(), "user-a", "project-00000001", model.ProjectEventFilter{Type: model.ProjectEventTypeWarning}, 10, nil)
		byDeploy, _, _ := eventRepository.ListEvents(context.Background(), "user-a", "project-00000001", model.ProjectEventFilter{DeployID: "deploy-a"}, 10, nil)
		byReason, _, _ := eventRepository.ListEvents(context.Background(), "user-a", "project-00000001", model.ProjectEventFilter{Reason: "BackOff"}, 10, nil)
		if len(warnings) != 1 || len(byDeploy) != 2 || len(byReason) != 1 {
			subTest.Errorf("1件、2件、1件のはず: %d %d %d", len(warnings), len(byDeploy), len(byReason))
		}
	})
	testingT.Run("他のユーザーのプロジェクトと、無いプロジェクトは not_found", func(subTest *testing.T) {
		for _, projectID := range []string{"project-00000002", "project-nothing"} {
			_, _, listError := eventRepository.ListEvents(context.Background(), "user-a", projectID, model.ProjectEventFilter{}, 10, nil)
			if apperror.KindOf(listError) != apperror.KindNotFound {
				subTest.Errorf("not_found のはず(%s): %v", projectID, listError)
			}
		}
	})
	testingT.Run("カーソルの ID が数字でなければ invalid_request", func(subTest *testing.T) {
		_, _, listError := eventRepository.ListEvents(context.Background(), "user-a", "project-00000001", model.ProjectEventFilter{}, 10, &pagination.Position{CreatedAt: base, ID: "abc"})
		if apperror.KindOf(listError) != apperror.KindInvalidRequest {
			subTest.Errorf("invalid_request のはず: %v", listError)
		}
	})
}

// TestProjectEventRepositoryPrune は、保存期間を過ぎたものと、プロジェクトごとの件数の上限を超えたものだけが、古いものから消えることを確認する。
func TestProjectEventRepositoryPrune(testingT *testing.T) {
	// 環境と、基準の時刻を用意する。
	database, eventRepository := newProjectEventTestEnvironment(testingT)
	now := time.Now().Truncate(time.Second)
	cutoff := now.Add(-7 * 24 * time.Hour)
	count := func(projectID string) int64 {
		var total int64
		database.Table("project_events").Where("project_id = ?", projectID).Count(&total)
		return total
	}
	add := func(projectID string, uid string, at time.Time) {
		if upsertError := eventRepository.UpsertEvent(context.Background(), newTestProjectEvent(projectID, uid, at)); upsertError != nil {
			testingT.Fatal(upsertError)
		}
	}

	testingT.Run("保存期間を過ぎたものだけが消える(境界の前後)", func(subTest *testing.T) {
		add("project-00000001", "old-1", cutoff.Add(-time.Hour))
		add("project-00000001", "old-2", cutoff.Add(-time.Second))
		add("project-00000001", "new-1", cutoff.Add(time.Second))
		add("project-00000001", "new-2", now)
		deleted, pruneError := eventRepository.PruneProjectEvents(context.Background(), cutoff, 1000)
		if pruneError != nil || deleted != 2 || count("project-00000001") != 2 {
			subTest.Errorf("期限切れの2件だけ消えるはず: 削除 %d、残り %d、%v", deleted, count("project-00000001"), pruneError)
		}
	})
	testingT.Run("1 プロジェクトの件数の上限を超えたものが、古い方から消え、他のプロジェクトは残る", func(subTest *testing.T) {
		for index := 0; index < 5; index++ {
			add("project-00000002", "b-"+strconv.Itoa(index), now.Add(time.Duration(index)*time.Second))
		}
		deleted, pruneError := eventRepository.PruneProjectEvents(context.Background(), cutoff, 3)
		var oldest model.ProjectEvent
		database.Where("project_id = ?", "project-00000002").Order("last_seen_at ASC").First(&oldest)
		if pruneError != nil || count("project-00000002") != 3 || oldest.EventUID != "b-2" || count("project-00000001") != 2 || deleted != 2 {
			subTest.Errorf("project-2 は新しい 3 件が残り、project-1 は 2 件のままのはず: 削除 %d、残り %d / %d、最も古い %s、%v", deleted, count("project-00000002"), count("project-00000001"), oldest.EventUID, pruneError)
		}
	})
	testingT.Run("繰り返しても、何も消えない(冪等)", func(subTest *testing.T) {
		deleted, pruneError := eventRepository.PruneProjectEvents(context.Background(), cutoff, 3)
		if pruneError != nil || deleted != 0 {
			subTest.Errorf("何も消えないはず: %d %v", deleted, pruneError)
		}
	})
	testingT.Run("引数が不正なら、何も消さない", func(subTest *testing.T) {
		if _, pruneError := eventRepository.PruneProjectEvents(context.Background(), time.Time{}, 3); pruneError == nil {
			subTest.Error("時刻が空なら拒否されるはず")
		}
		if _, pruneError := eventRepository.PruneProjectEvents(context.Background(), cutoff, 0); pruneError == nil {
			subTest.Error("件数が 0 なら拒否されるはず")
		}
	})
}

// TestProjectEventRepositoryDeleteByUIDs は、指定した UID の Event だけが消えることを確認する。
func TestProjectEventRepositoryDeleteByUIDs(testingT *testing.T) {
	// 3 件入れて、2 件を消す。
	_, eventRepository := newProjectEventTestEnvironment(testingT)
	now := time.Now().Truncate(time.Second)
	for _, uid := range []string{"a", "b", "c"} {
		if upsertError := eventRepository.UpsertEvent(context.Background(), newTestProjectEvent("project-00000001", uid, now)); upsertError != nil {
			testingT.Fatal(upsertError)
		}
	}
	deleted, deleteError := eventRepository.DeleteEventsByUIDs(context.Background(), []string{"a", "b"})
	remaining, _ := eventRepository.ListEventUIDs(context.Background())
	if deleteError != nil || deleted != 2 || len(remaining) != 1 || remaining[0] != "c" {
		testingT.Errorf("c だけが残るはず: 削除 %d、残り %v、%v", deleted, remaining, deleteError)
	}
	// 空の一覧では、何も消えない。
	if none, _ := eventRepository.DeleteEventsByUIDs(context.Background(), nil); none != 0 {
		testingT.Errorf("何も消えないはず: %d", none)
	}
}
