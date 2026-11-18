package workflow

import (
	"reflect"
	"testing"
	"time"

	"go.temporal.io/sdk/temporal"
	sdkworkflow "go.temporal.io/sdk/workflow"
)

// oldRetryPolicy は、まとめる前に、全ての Workflow に書かれていた再試行の方針(生の数字で書き写したもの)。
func oldRetryPolicy() *temporal.RetryPolicy {
	return &temporal.RetryPolicy{
		InitialInterval:    2 * time.Second,
		MaximumInterval:    time.Minute,
		BackoffCoefficient: 2,
	}
}

// TestActivityOptionsAreSameAsBeforeRefactoring は、まとめた関数が返す設定が、まとめる前に各 Workflow に
// 書かれていた設定と、完全に同じ値であることを確認する(整理で、期限や間隔が変わっていないことの確認)。
func TestActivityOptionsAreSameAsBeforeRefactoring(testingT *testing.T) {
	// まとめる前の、5 つの形(値は、生の数字。定数を経由しない)。
	testCases := []struct {
		name     string
		actual   sdkworkflow.ActivityOptions
		expected sdkworkflow.ActivityOptions
	}{
		{
			name:   "k8s への依頼(2分まで再試行)",
			actual: infrastructureOptions(),
			expected: sdkworkflow.ActivityOptions{
				StartToCloseTimeout: 5 * time.Minute, ScheduleToCloseTimeout: 2 * time.Minute, RetryPolicy: oldRetryPolicy(),
			},
		},
		{
			name:   "結果の記録・補償(10分まで再試行)",
			actual: compensationOptions(),
			expected: sdkworkflow.ActivityOptions{
				StartToCloseTimeout: 5 * time.Minute, ScheduleToCloseTimeout: 10 * time.Minute, RetryPolicy: oldRetryPolicy(),
			},
		},
		{
			name:   "削除(15分まで再試行。ハートビートなし)",
			actual: retryUntil(15 * time.Minute),
			expected: sdkworkflow.ActivityOptions{
				StartToCloseTimeout: 5 * time.Minute, ScheduleToCloseTimeout: 15 * time.Minute, RetryPolicy: oldRetryPolicy(),
			},
		},
		{
			name:   "プロジェクトの削除(15分まで再試行。ハートビート 30 秒)",
			actual: retryUntilWithHeartbeat(15 * time.Minute),
			expected: sdkworkflow.ActivityOptions{
				StartToCloseTimeout: 5 * time.Minute, ScheduleToCloseTimeout: 15 * time.Minute, HeartbeatTimeout: 30 * time.Second, RetryPolicy: oldRetryPolicy(),
			},
		},
		{
			name:   "起動待ち(1回も全体も 300 秒。ハートビート 30 秒)",
			actual: startupWaitOptions(),
			expected: sdkworkflow.ActivityOptions{
				StartToCloseTimeout: 300 * time.Second, ScheduleToCloseTimeout: 300 * time.Second, HeartbeatTimeout: 30 * time.Second, RetryPolicy: oldRetryPolicy(),
			},
		},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			if !reflect.DeepEqual(testCase.actual, testCase.expected) {
				subTest.Errorf("設定が、まとめる前と違う:\n実際 %+v (RetryPolicy %+v)\n期待 %+v (RetryPolicy %+v)",
					testCase.actual, testCase.actual.RetryPolicy, testCase.expected, testCase.expected.RetryPolicy)
			}
		})
	}
}

// TestStandardRetryPolicy は、再試行の方針が、回数無制限で、間隔が 2 秒から倍々に増えて 1 分で頭打ちになることを確認する。
func TestStandardRetryPolicy(testingT *testing.T) {
	policy := standardRetryPolicy()
	if policy.MaximumAttempts != 0 || policy.InitialInterval != 2*time.Second || policy.MaximumInterval != time.Minute || policy.BackoffCoefficient != 2 {
		testingT.Errorf("方針が違う: %+v", policy)
	}
	// 呼ぶたびに、別のオブジェクトを返す(Workflow の間で、設定を共有して書き換えない)。
	if standardRetryPolicy() == policy {
		testingT.Errorf("毎回、新しいオブジェクトのはず")
	}
}

// TestRetryUntilWithHeartbeatDoesNotChangeRetryUntil は、ハートビート付きの設定を作っても、元の設定(retryUntil)が
// 変わらず、ハートビートが付かないことを確認する。
func TestRetryUntilWithHeartbeatDoesNotChangeRetryUntil(testingT *testing.T) {
	_ = retryUntilWithHeartbeat(time.Minute)
	if options := retryUntil(time.Minute); options.HeartbeatTimeout != 0 {
		testingT.Errorf("retryUntil にハートビートは付かないはず: %v", options.HeartbeatTimeout)
	}
}
