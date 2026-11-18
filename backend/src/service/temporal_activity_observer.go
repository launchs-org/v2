package service

import (
	"context"
	"sort"
	"sync"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"

	"launchs/shared/projecttask"
)

// observeCacheTTL は、同じ Workflow の履歴を、読み直さずに使う時間(画面の自動更新で、Temporal に負担を掛けないため)。
const observeCacheTTL = 2 * time.Second

// cachedObservation は、読み取った結果の控え。
type cachedObservation struct {
	at        time.Time
	activites []projecttask.ObservedActivity
}

// TemporalActivityObserver は、Temporal の履歴から、Workflow が動かした Activity の様子を読み取る。
type TemporalActivityObserver struct {
	temporalClient client.Client
	mutex          sync.Mutex
	cache          map[string]cachedObservation
	// now は、現在の時刻を返す関数(試験で差し替える)。
	now func() time.Time
}

// NewTemporalActivityObserver は、TemporalActivityObserver を作る。
//
// 引数:
//   - temporalClient: Temporal サーバーへのクライアント。
//
// 戻り値:
//   - 作成した TemporalActivityObserver。
func NewTemporalActivityObserver(temporalClient client.Client) *TemporalActivityObserver {
	// クライアントと、控えの入れ物を持たせて返す。
	return &TemporalActivityObserver{temporalClient: temporalClient, cache: map[string]cachedObservation{}, now: time.Now}
}

// Observe は、Workflow の履歴を読み、Activity ごとの様子(成功の回数、失敗、動いているか、何回目の試行か)を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - workflowID: Workflow の ID。
//   - runID: 実行の ID。
//
// 戻り値:
//   - Activity ごとの様子(名前の順)。履歴が無い(保存期間を過ぎた)、Temporal につなげないときは、エラー。
func (observer *TemporalActivityObserver) Observe(requestContext context.Context, workflowID string, runID string) ([]projecttask.ObservedActivity, error) {
	// 直前に読んだものがあれば、それを使う。
	key := workflowID + "/" + runID
	observer.mutex.Lock()
	if cached, found := observer.cache[key]; found && observer.now().Sub(cached.at) < observeCacheTTL {
		observer.mutex.Unlock()
		return cached.activites, nil
	}
	observer.mutex.Unlock()
	// 履歴を、1 件ずつ読む。
	iterator := observer.temporalClient.GetWorkflowHistory(requestContext, workflowID, runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	var events []*historypb.HistoryEvent
	for iterator.HasNext() {
		event, nextError := iterator.Next()
		if nextError != nil {
			return nil, nextError
		}
		events = append(events, event)
	}
	// 動いている Activity の、何回目の試行か、直近の失敗の理由は、履歴には出ないので、Workflow の様子から読む。
	attempts := map[string]pendingInfo{}
	if description, describeError := observer.temporalClient.DescribeWorkflowExecution(requestContext, workflowID, runID); describeError == nil {
		for _, pending := range description.GetPendingActivities() {
			info := pendingInfo{attempt: int(pending.GetAttempt())}
			if pending.GetLastFailure() != nil {
				info.failure = pending.GetLastFailure().GetMessage()
			}
			attempts[pending.GetActivityType().GetName()] = info
		}
	}
	activities := summarizeActivities(events, attempts)
	observer.mutex.Lock()
	observer.cache[key] = cachedObservation{at: observer.now(), activites: activities}
	observer.mutex.Unlock()
	return activities, nil
}

// pendingInfo は、動いている Activity の、試行の回数と、直近の失敗の理由。
type pendingInfo struct {
	attempt int
	failure string
}

// summarizeActivities は、履歴のイベントから、Activity ごとの様子をまとめる。
// 予定された(Scheduled)Activity のうち、終わり(成功、失敗、時間切れ、取り消し)のイベントが無いものを、動いているものとする。
//
// 引数:
//   - events: 履歴のイベント。
//   - pending: 動いている Activity の名前 → 試行の回数と直近の失敗の理由(Describe の結果)。
//
// 戻り値:
//   - Activity の名前の順にそろえた、様子。
func summarizeActivities(events []*historypb.HistoryEvent, pending map[string]pendingInfo) []projecttask.ObservedActivity {
	// 予定されたイベントの番号 → Activity の名前。
	scheduledName := map[int64]string{}
	byName := map[string]*projecttask.ObservedActivity{}
	get := func(name string) *projecttask.ObservedActivity {
		if byName[name] == nil {
			byName[name] = &projecttask.ObservedActivity{Name: name}
		}
		return byName[name]
	}
	open := map[int64]bool{}
	for _, event := range events {
		switch event.GetEventType() {
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED:
			name := event.GetActivityTaskScheduledEventAttributes().GetActivityType().GetName()
			scheduledName[event.GetEventId()] = name
			open[event.GetEventId()] = true
			get(name)
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_COMPLETED:
			scheduledID := event.GetActivityTaskCompletedEventAttributes().GetScheduledEventId()
			get(scheduledName[scheduledID]).Completed++
			delete(open, scheduledID)
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_FAILED:
			attributes := event.GetActivityTaskFailedEventAttributes()
			activity := get(scheduledName[attributes.GetScheduledEventId()])
			activity.Failed++
			activity.Error = attributes.GetFailure().GetMessage()
			delete(open, attributes.GetScheduledEventId())
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_TIMED_OUT:
			attributes := event.GetActivityTaskTimedOutEventAttributes()
			activity := get(scheduledName[attributes.GetScheduledEventId()])
			activity.Failed++
			activity.Error = "時間切れになりました"
			delete(open, attributes.GetScheduledEventId())
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_CANCELED:
			delete(open, event.GetActivityTaskCanceledEventAttributes().GetScheduledEventId())
		}
	}
	// 終わりのイベントが無いものを、動いているものにする。
	for scheduledID := range open {
		name := scheduledName[scheduledID]
		activity := get(name)
		activity.Pending = true
		activity.Attempt = 1
		if info, found := pending[name]; found {
			activity.Attempt = info.attempt
			activity.Error = info.failure
		}
	}
	// 名前の順にそろえる。
	result := make([]projecttask.ObservedActivity, 0, len(byName))
	for _, activity := range byName {
		result = append(result, *activity)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}
