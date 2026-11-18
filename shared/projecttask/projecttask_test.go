package projecttask

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	sdkworkflow "go.temporal.io/sdk/workflow"

	"launchs/shared/workflow"
)

// TestKindOf は、全ての Workflow に種類があること(記録の対象)と、知らない Workflow は対象外であることを確認する。
func TestKindOf(testingT *testing.T) {
	for _, name := range []string{workflow.CreateDeployWorkflowName, workflow.ApplyIngressRulesWorkflowName, workflow.ApplyTemplateWorkflowName, workflow.DeleteProjectWorkflowName} {
		if kind, found := KindOf(name); !found || kind == "" {
			testingT.Errorf("%s: 種類があるはず", name)
		}
	}
	if _, found := KindOf("UnknownWorkflow"); found {
		testingT.Error("知らない Workflow は、対象外のはず")
	}
	if len(Kinds()) != 18 {
		testingT.Errorf("種類は 18 個のはず: %d", len(Kinds()))
	}
}

// TestSanitize は、秘密の伏せ方、1 行にすること、長さの切り方を確認する。
func TestSanitize(testingT *testing.T) {
	for name, testCase := range map[string]struct{ input, want, notWant string }{
		"URL の認証情報":     {"dial postgres://postgres:s3cret@postgres:5432/app failed", "postgres://***@postgres:5432/app", "s3cret"},
		"password の組":   {"login failed: password=hunter2 for user", "password=***", "hunter2"},
		"token の組(コロン)": {`request {"token": "abc123"}`, "token", "abc123"},
		"Bearer":        {"401 with Authorization: Bearer eyJhbGciOi.payload.sig", "Bearer ***", "eyJhbGciOi"},
		"改行と空白は 1 行にする": {"line1\n\n  line2\t line3", "line1 line2 line3", "\n"},
	} {
		got := Sanitize(testCase.input)
		if !strings.Contains(got, testCase.want) || strings.Contains(got, testCase.notWant) {
			testingT.Errorf("%s: 期待 %q を含み、%q を含まない。実際 %q", name, testCase.want, testCase.notWant, got)
		}
	}
	long := Sanitize(strings.Repeat("あ", MaxErrorRunes+50))
	if len([]rune(long)) != MaxErrorRunes+1 || !strings.HasSuffix(long, "…") {
		testingT.Errorf("長さを切るはず: %d", len([]rune(long)))
	}
}

// TestErrorMessage は、包まれたエラーから、原因のメッセージを取り出すことを確認する。
func TestErrorMessage(testingT *testing.T) {
	cause := temporal.NewNonRetryableApplicationError("イメージを取得できません: password=abc", "ImagePull", nil)
	wrapped := fmt.Errorf("workflow execution error: %w", fmt.Errorf("activity error (type: X): %w", cause))
	if got := ErrorMessage(wrapped); got != "イメージを取得できません: password=***" {
		testingT.Errorf("原因のメッセージで、秘密は伏せるはず: %q", got)
	}
	if got := ErrorMessage(errors.New("plain failure")); got != "plain failure" {
		testingT.Errorf("普通のエラーは、そのまま: %q", got)
	}
	if got := ErrorMessage(temporal.NewCanceledError()); got != "取り消されました" {
		testingT.Errorf("取り消しは決まった文: %q", got)
	}
}

// TestErrorMessageTimeout は、Activity が時間切れになった Workflow の失敗に、処理の名前が添えられることを確認する。
func TestErrorMessageTimeout(testingT *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(_ context.Context) error { return nil }, activity.RegisterOptions{Name: "WaitDeploymentReady"})
	workflowFunction := func(workflowContext sdkworkflow.Context) error {
		activityContext := sdkworkflow.WithActivityOptions(workflowContext, sdkworkflow.ActivityOptions{StartToCloseTimeout: time.Minute, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}})
		return sdkworkflow.ExecuteActivity(activityContext, "WaitDeploymentReady").Get(activityContext, nil)
	}
	env.RegisterWorkflow(workflowFunction)
	env.OnActivity("WaitDeploymentReady", mock.Anything).Return(temporal.NewTimeoutError(enums.TIMEOUT_TYPE_START_TO_CLOSE, nil))
	env.ExecuteWorkflow(workflowFunction)
	if got := ErrorMessage(env.GetWorkflowError()); got != "処理「WaitDeploymentReady」が、時間切れになりました" {
		testingT.Errorf("時間切れは、処理の名前を添えるはず: %q", got)
	}
}
