package repository

import (
	"context"
	"strconv"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProjectEventRepository は、プロジェクトの namespace の k8s の Event(project_events)の保存先。
//
// 書くのは watcher だけ(UpsertEvent、DeleteEventByUID、ListEventUIDs、PruneProjectEvents)。
// backend は、読むだけ(ListEvents)。
type ProjectEventRepository struct {
	database *gorm.DB
}

// NewProjectEventRepository は、ProjectEventRepository を作る。
//
// 引数:
//   - database: Gorm のデータベース接続。
//
// 戻り値:
//   - 作成した ProjectEventRepository。
func NewProjectEventRepository(database *gorm.DB) *ProjectEventRepository {
	// 接続を持たせて返す。
	return &ProjectEventRepository{database: database}
}

// UpsertEvent は、k8s の Event を保存する。同じ UID の Event があれば、回数と時刻、本文を上書きする(k8s は、同じ Event を、回数を増やして更新する)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - event: 保存する Event。ProjectID、EventUID、Type、時刻が要る。
//
// 戻り値:
//   - 必須の項目が空のとき、またはデータベースのエラー。
func (eventRepository *ProjectEventRepository) UpsertEvent(requestContext context.Context, event model.ProjectEvent) error {
	// 空の条件だと、意図しない行が対象になるので、必須の項目が空なら、先に拒否する。
	if hasEmpty(event.ProjectID, event.EventUID, event.Type) || event.FirstSeenAt.IsZero() || event.LastSeenAt.IsZero() {
		return newEmptyConditionError()
	}
	// 同じ UID なら、回数・時刻・本文を、新しい値で上書きする(最初の時刻は、古い方を残す)。
	saveError := eventRepository.database.WithContext(requestContext).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "event_uid"}},
		DoUpdates: clause.Assignments(map[string]any{
			"type":          gorm.Expr("EXCLUDED.type"),
			"reason":        gorm.Expr("EXCLUDED.reason"),
			"message":       gorm.Expr("EXCLUDED.message"),
			"object_kind":   gorm.Expr("EXCLUDED.object_kind"),
			"object_name":   gorm.Expr("EXCLUDED.object_name"),
			"deploy_id":     gorm.Expr("EXCLUDED.deploy_id"),
			"count":         gorm.Expr("GREATEST(project_events.count, EXCLUDED.count)"),
			"first_seen_at": gorm.Expr("LEAST(project_events.first_seen_at, EXCLUDED.first_seen_at)"),
			"last_seen_at":  gorm.Expr("GREATEST(project_events.last_seen_at, EXCLUDED.last_seen_at)"),
		}),
	}).Create(&event).Error
	if saveError != nil {
		return apperror.Wrap(apperror.KindInternal, "イベントを保存できませんでした", saveError)
	}
	return nil
}

// ListEvents は、プロジェクトの k8s の Event を、最後に起きた時刻の新しい順に、1ページ分取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - filter: 絞り込み(種類、deploy、理由。空なら、その条件は使わない)。
//   - limit: 1ページの件数。
//   - after: 2ページ目以降の、前のページの最後の位置。最初は nil。位置の時刻は最後に起きた時刻、ID は通し番号(10進の文字列)。
//
// 戻り値:
//   - Event の一覧と、次のページの位置(最後のページなら nil)。
//   - プロジェクトが無い(他人のものを含む)ときは not_found。位置の ID が数字でないときは invalid_request。データベースのエラーは internal_error。
func (eventRepository *ProjectEventRepository) ListEvents(requestContext context.Context, userID string, projectID string, filter model.ProjectEventFilter, limit int, after *pagination.Position) ([]model.ProjectEvent, *pagination.Position, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する(見つからない扱い)。
	if hasEmpty(userID, projectID) {
		return nil, nil, apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// プロジェクトが、持ち主のものかを確かめる(他人のものは、存在しないものとして扱う)。
	var projectCount int64
	if countError := eventRepository.database.WithContext(requestContext).Model(&model.Project{}).
		Where(&model.Project{ID: projectID, UserID: userID}).Count(&projectCount).Error; countError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", countError)
	}
	if projectCount == 0 {
		return nil, nil, apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// 新しい順に並べる検索を組み立てる(次のページがあるかを知るため、1件多く読む)。
	query := eventRepository.database.WithContext(requestContext).
		Where(&model.ProjectEvent{ProjectID: projectID}).
		Order("last_seen_at DESC, id DESC").
		Limit(limit + 1)
	// 絞り込みの条件を足す(空の条件は、使わない)。
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.DeployID != "" {
		query = query.Where("deploy_id = ?", filter.DeployID)
	}
	if filter.Reason != "" {
		query = query.Where("reason = ?", filter.Reason)
	}
	// 2ページ目以降なら、前のページの最後より後ろだけに絞る。
	if after != nil {
		afterID, parseError := strconv.ParseInt(after.ID, 10, 64)
		if parseError != nil {
			return nil, nil, apperror.New(apperror.KindInvalidRequest, "cursor が正しくありません")
		}
		query = query.Where("(last_seen_at, id) < (?, ?)", after.CreatedAt, afterID)
	}
	// 検索を実行する。
	var events []model.ProjectEvent
	if findError := query.Find(&events).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "イベントの一覧を取得できませんでした", findError)
	}
	// limit 件以下なら、次のページは無い。
	if len(events) <= limit {
		return events, nil, nil
	}
	// 確認のために多く読んだ1件を外し、次のページの位置を作る。
	events = events[:limit]
	last := events[limit-1]
	return events, &pagination.Position{CreatedAt: last.LastSeenAt, ID: strconv.FormatInt(last.ID, 10)}, nil
}

// ListEventUIDs は、保存してある Event の UID を、全て返す(watcher が、起動のとき、k8s に無いものを消すために使う)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - UID の一覧。データベースのエラーは internal_error。
func (eventRepository *ProjectEventRepository) ListEventUIDs(requestContext context.Context) ([]string, error) {
	// UID の列だけを読む。
	var uids []string
	if findError := eventRepository.database.WithContext(requestContext).Model(&model.ProjectEvent{}).Pluck("event_uid", &uids).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "イベントの一覧を取得できませんでした", findError)
	}
	return uids, nil
}

// PruneProjectEvents は、保存の上限を超えた Event を削除する。
//
// 削除するもの(どちらも、古いものから):
//   - 最後に起きた時刻が cutoff より前のもの(保存期間を過ぎたもの)
//   - 1つのプロジェクトにつき、新しい順に maxPerProject 件を超えたもの
//
// 繰り返し実行しても壊れない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - cutoff: この時刻より前に最後に起きた Event を削除する。
//   - maxPerProject: 1つのプロジェクトに残す、Event の最大の件数(1以上)。
//
// 戻り値:
//   - 削除した件数。
//   - 引数が不正なとき(時刻が空、件数が1未満)、またはデータベースのエラー。
func (eventRepository *ProjectEventRepository) PruneProjectEvents(requestContext context.Context, cutoff time.Time, maxPerProject int) (int64, error) {
	// 空の時刻だと全てが対象外(または対象)になり、件数が0以下だと全て消えるので、先に拒否する。
	if cutoff.IsZero() || maxPerProject < 1 {
		return 0, newEmptyConditionError()
	}
	// 削除した件数を、トランザクションの外へ持ち出す入れ物。
	var deletedCount int64
	// 2つの削除を、1つのトランザクションで行う。
	transactionError := eventRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// ----- 保存期間を過ぎたものを削除する -----
		expired := transaction.Where("last_seen_at < ?", cutoff).Delete(&model.ProjectEvent{})
		if expired.Error != nil {
			return expired.Error
		}
		// ----- 1つのプロジェクトにつき、新しい順に maxPerProject 件を超えたものを削除する -----
		overflow := transaction.Exec(`
			DELETE FROM project_events
			WHERE id IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (PARTITION BY project_id ORDER BY last_seen_at DESC, id DESC) AS position
					FROM project_events
				) ranked
				WHERE position > ?
			)`, maxPerProject)
		if overflow.Error != nil {
			return overflow.Error
		}
		deletedCount = expired.RowsAffected + overflow.RowsAffected
		return nil
	})
	if transactionError != nil {
		return 0, apperror.Wrap(apperror.KindInternal, "古いイベントを削除できませんでした", transactionError)
	}
	return deletedCount, nil
}

// DeleteEventsByUIDs は、指定した UID の Event を削除する(watcher が、k8s から消えた Event を、DB から消すために使う)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - uids: 削除する Event の UID。空なら、何もしない。
//
// 戻り値:
//   - 削除した件数。データベースのエラーは internal_error。
func (eventRepository *ProjectEventRepository) DeleteEventsByUIDs(requestContext context.Context, uids []string) (int64, error) {
	// 空なら、全件が対象になることは無いが、無駄な呼び出しをしない。
	if len(uids) == 0 {
		return 0, nil
	}
	// 指定した UID だけを削除する。
	result := eventRepository.database.WithContext(requestContext).Where("event_uid IN ?", uids).Delete(&model.ProjectEvent{})
	if result.Error != nil {
		return 0, apperror.Wrap(apperror.KindInternal, "イベントを削除できませんでした", result.Error)
	}
	return result.RowsAffected, nil
}
