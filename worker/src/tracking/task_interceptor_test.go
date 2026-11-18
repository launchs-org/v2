package tracking

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	sdkworkflow "go.temporal.io/sdk/workflow"

	"launchs/shared/model"
	sharedworkflow "launchs/shared/workflow"
)

// fakeStore は、記録された呼び出しを覚える、試験用の保存先。
type fakeStore struct {
	mutex    sync.Mutex
	started  []model.ProjectTask
	finished []finishRecord
	missing  bool
}

// ResolveProject は、入力の DeployID から、決まったプロジェクトを返す(missing なら、見つからない)。
func (store *fakeStore) ResolveProject(_ context.Context, input map[string]any) (string, string, bool, error) {
	deployID, _ := input["DeployID"].(string)
	return "project-1", deployID, !store.missing && deployID != "", nil
}

// Start は、タスクを覚える。
func (store *fakeStore) Start(_ context.Context, task model.ProjectTask) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.started = append(store.started, task)
	return nil
}

// Finish は、結果を覚える。
func (store *fakeStore) Finish(_ context.Context, workflowID string, runID string, status string, reason string, finishedAt time.Time) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.finished = append(store.finished, finishRecord{WorkflowID: workflowID, RunID: runID, Status: status, Reason: reason, FinishedAt: finishedAt})
	return nil
}

// testInput は、試験用の Workflow の入力(deploy の Workflow と同じ、DeployID を持つ形)。
type testInput struct{ DeployID string }

// runWorkflow は、interceptor つきの試験環境で、CreateDeployWorkflow という名前の Workflow を、決めた結果で動かす。
func runWorkflow(testingT *testing.T, store *fakeStore, input testInput, result error) error {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(func(_ sdkworkflow.Context, _ testInput) error { return result }, sdkworkflow.RegisterOptions{Name: sharedworkflow.CreateDeployWorkflowName})
	return executeWithInterceptor(env, store, input)
}

// executeWithInterceptor は、interceptor を付けて、Workflow を実行する。
func executeWithInterceptor(env *testsuite.TestWorkflowEnvironment, store *fakeStore, input testInput) error {
	env.SetWorkerOptions(worker.Options{Interceptors: []interceptor.WorkerInterceptor{NewTaskInterceptor(store, slog.New(slog.NewTextHandler(io.Discard, nil)))}})
	env.ExecuteWorkflow(sharedworkflow.CreateDeployWorkflowName, input)
	return env.GetWorkflowError()
}

// TestTaskInterceptor は、成功・失敗・対象なしの記録を確認する。
func TestTaskInterceptor(testingT *testing.T) {
	testingT.Run("成功: 開始と、成功の終わりが記録される", func(subTest *testing.T) {
		store := &fakeStore{}
		if err := runWorkflow(subTest, store, testInput{DeployID: "deploy-1"}, nil); err != nil {
			subTest.Fatal(err)
		}
		if len(store.started) != 1 || store.started[0].ProjectID != "project-1" || store.started[0].Kind != "deploy_create" || store.started[0].ResourceID != "deploy-1" || store.started[0].WorkflowType != sharedworkflow.CreateDeployWorkflowName {
			subTest.Fatalf("開始が記録されるはず: %+v", store.started)
		}
		if len(store.finished) != 1 || store.finished[0].Status != model.TaskStatusSucceeded || store.finished[0].Reason != "" || store.finished[0].WorkflowID != store.started[0].WorkflowID {
			subTest.Errorf("成功が記録されるはず: %+v", store.finished)
		}
	})
	testingT.Run("失敗: 理由(秘密は伏せる)が記録され、Workflow の結果は、そのまま失敗", func(subTest *testing.T) {
		store := &fakeStore{}
		err := runWorkflow(subTest, store, testInput{DeployID: "deploy-1"}, temporal.NewNonRetryableApplicationError("起動できません: password=abc", "Boom", errors.New("cause")))
		if err == nil {
			subTest.Fatal("Workflow は失敗するはず")
		}
		if len(store.finished) != 1 || store.finished[0].Status != model.TaskStatusFailed || store.finished[0].Reason != "起動できません: password=***" {
			subTest.Errorf("失敗の理由が記録されるはず: %+v", store.finished)
		}
	})
	testingT.Run("対象のリソースが無ければ、記録しない", func(subTest *testing.T) {
		store := &fakeStore{missing: true}
		if err := runWorkflow(subTest, store, testInput{DeployID: "deploy-1"}, nil); err != nil {
			subTest.Fatal(err)
		}
		if len(store.started) != 0 || len(store.finished) != 0 {
			subTest.Errorf("記録されないはず: %+v %+v", store.started, store.finished)
		}
	})
}
