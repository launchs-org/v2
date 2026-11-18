package service

import (
	"testing"

	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	failurepb "go.temporal.io/api/failure/v1"
	historypb "go.temporal.io/api/history/v1"
)

// scheduledEvent は、Activity が予定されたイベントを作る(試験用)。
func scheduledEvent(eventID int64, name string) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: eventID, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED,
		Attributes: &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{ActivityTaskScheduledEventAttributes: &historypb.ActivityTaskScheduledEventAttributes{ActivityType: &commonpb.ActivityType{Name: name}}}}
}

// TestSummarizeActivities は、履歴のイベントから、成功、失敗、動いているものを、読み取れることを確認する。
func TestSummarizeActivities(testingT *testing.T) {
	events := []*historypb.HistoryEvent{
		scheduledEvent(5, "EnsureProjectNamespace"),
		{EventId: 8, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_COMPLETED, Attributes: &historypb.HistoryEvent_ActivityTaskCompletedEventAttributes{ActivityTaskCompletedEventAttributes: &historypb.ActivityTaskCompletedEventAttributes{ScheduledEventId: 5}}},
		scheduledEvent(11, "ApplyDeployment"),
		{EventId: 14, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_FAILED, Attributes: &historypb.HistoryEvent_ActivityTaskFailedEventAttributes{ActivityTaskFailedEventAttributes: &historypb.ActivityTaskFailedEventAttributes{ScheduledEventId: 11, Failure: &failurepb.Failure{Message: "api down"}}}},
		scheduledEvent(17, "WaitDeploymentReady"),
	}
	result := summarizeActivities(events, map[string]pendingInfo{"WaitDeploymentReady": {attempt: 3, failure: "not ready"}})
	if len(result) != 3 {
		testingT.Fatalf("3 種類のはず: %+v", result)
	}
	// 名前の順: ApplyDeployment、EnsureProjectNamespace、WaitDeploymentReady。
	if result[0].Name != "ApplyDeployment" || result[0].Failed != 1 || result[0].Error != "api down" || result[0].Pending {
		testingT.Errorf("失敗のはず: %+v", result[0])
	}
	if result[1].Name != "EnsureProjectNamespace" || result[1].Completed != 1 || result[1].Pending {
		testingT.Errorf("成功のはず: %+v", result[1])
	}
	if result[2].Name != "WaitDeploymentReady" || !result[2].Pending || result[2].Attempt != 3 || result[2].Error != "not ready" {
		testingT.Errorf("動いていて、3 回目の試行のはず: %+v", result[2])
	}
}
