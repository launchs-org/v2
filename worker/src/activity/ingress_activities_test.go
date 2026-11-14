package activity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"gorm.io/datatypes"

	"launchs/shared/logger"
	"launchs/shared/model"
	"launchs/shared/workflow"
)

// fakeIngressStore は、テスト用の IngressStore。決めた ingress を返し、呼び出しを覚える。
type fakeIngressStore struct {
	ingress       model.Ingress
	findError     error
	commits       []string
	deletedRecord []string
	failedMarks   []string
	storeError    error
}

// FindForWorker は、決めた ingress とエラーを返す。
func (fakeStore *fakeIngressStore) FindForWorker(_ context.Context, _ string) (model.Ingress, error) {
	return fakeStore.ingress, fakeStore.findError
}

// CommitRulesApply は、結果を覚えて、決めたエラーを返す。
func (fakeStore *fakeIngressStore) CommitRulesApply(_ context.Context, ingressID string, succeeded bool) (bool, error) {
	fakeStore.commits = append(fakeStore.commits, fmt.Sprintf("%s|%v", ingressID, succeeded))
	return true, fakeStore.storeError
}

// DeleteRecord は、ID を覚える。
func (fakeStore *fakeIngressStore) DeleteRecord(_ context.Context, ingressID string) error {
	fakeStore.deletedRecord = append(fakeStore.deletedRecord, ingressID)
	return fakeStore.storeError
}

// MarkDeleteFailed は、ID を覚える。
func (fakeStore *fakeIngressStore) MarkDeleteFailed(_ context.Context, ingressID string) error {
	fakeStore.failedMarks = append(fakeStore.failedMarks, ingressID)
	return fakeStore.storeError
}

// fakeIngressOperator は、テスト用の IngressOperator。呼び出しを覚え、決めたエラーを返す。
type fakeIngressOperator struct {
	applied       []IngressRouteDefinition
	deletedRoutes []string
	err           error
}

// ApplyIngressRoute は、渡された内容を覚えて、決めたエラーを返す。
func (fakeOperator *fakeIngressOperator) ApplyIngressRoute(_ context.Context, definition IngressRouteDefinition) error {
	fakeOperator.applied = append(fakeOperator.applied, definition)
	return fakeOperator.err
}

// DeleteIngressRoute は、名前空間と名前を覚えて、決めたエラーを返す。
func (fakeOperator *fakeIngressOperator) DeleteIngressRoute(_ context.Context, namespaceName string, ingressName string) error {
	fakeOperator.deletedRoutes = append(fakeOperator.deletedRoutes, namespaceName+"/"+ingressName)
	return fakeOperator.err
}

// newTestIngress は、現在のルール(/ → web の 8080)と、pending のルール(/api → api の 9090)を持つ ingress を作る。
func newTestIngress() model.Ingress {
	return model.Ingress{
		ID: "ingress-1", ProjectID: "project-1", Host: "brave-blue-fox.launchs.org", Status: model.IngressStatusApplying,
		Rules:        datatypes.JSON(`[{"path":"/","serviceId":"service-web","port":8080}]`),
		PendingRules: datatypes.JSON(`[{"path":"/api","serviceId":"service-api","port":9090}]`),
	}
}

// newTestIngressActivities は、テスト用の IngressActivities を作る。
//
// 引数:
//   - store: データベースの代わり。
//   - operator: k8s の代わり。
//
// 戻り値:
//   - ログを捨てる IngressActivities。
func newTestIngressActivities(store *fakeIngressStore, operator *fakeIngressOperator) *IngressActivities {
	// ログは捨てる入れ物に出す。
	return NewIngressActivities(store, operator, logger.New(&bytes.Buffer{}, "worker", slog.LevelInfo))
}

// TestApplyAndRollbackIngressRules は、反映は pending のルール、巻き戻しは現在のルールで IngressRoute を作ることと、
// ルールが無い・壊れているときはリトライしないエラーになることを確認する。
func TestApplyAndRollbackIngressRules(testingT *testing.T) {
	// 実行のための、Temporal の Activity のテスト環境。
	run := func(activities *IngressActivities, activityFunction any, input any) error {
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activityFunction)
		_, runError := testEnvironment.ExecuteActivity(activityFunction, input)
		return runError
	}
	input := workflow.ApplyIngressRulesActivityInput{IngressID: "ingress-1"}

	testingT.Run("反映は、pending のルールで IngressRoute を作る(現在のルールは使わない)", func(subTest *testing.T) {
		operator := &fakeIngressOperator{}
		activities := newTestIngressActivities(&fakeIngressStore{ingress: newTestIngress()}, operator)
		if runError := run(activities, activities.ApplyIngressRules, input); runError != nil {
			subTest.Fatalf("成功するはず: %v", runError)
		}
		definition := operator.applied[0]
		if definition.Namespace != "project-1" || definition.Name != "ingress-1" || definition.Host != "brave-blue-fox.launchs.org" ||
			len(definition.Rules) != 1 || definition.Rules[0] != (IngressRouteRule{Path: "/api", ServiceName: "service-api", Port: 9090}) {
			subTest.Errorf("pending のルールが渡るはず: %+v", definition)
		}
	})
	testingT.Run("巻き戻しは、現在のルールで IngressRoute を作る。現在のルールが空なら、空のルールを渡す(IngressRoute が消える)", func(subTest *testing.T) {
		operator := &fakeIngressOperator{}
		store := &fakeIngressStore{ingress: newTestIngress()}
		activities := newTestIngressActivities(store, operator)
		if runError := run(activities, activities.RollbackIngressRules, input); runError != nil {
			subTest.Fatalf("成功するはず: %v", runError)
		}
		if operator.applied[0].Rules[0].Path != "/" || operator.applied[0].Rules[0].ServiceName != "service-web" {
			subTest.Errorf("現在のルールが渡るはず: %+v", operator.applied[0])
		}
		store.ingress.Rules = datatypes.JSON(`[]`)
		_ = run(activities, activities.RollbackIngressRules, input)
		if len(operator.applied[1].Rules) != 0 {
			subTest.Errorf("空のルールが渡るはず: %+v", operator.applied[1])
		}
	})
	testingT.Run("適用するルールが無い、壊れているなら、リトライしないエラー(k8s には触れない)", func(subTest *testing.T) {
		for name, ingress := range map[string]model.Ingress{
			"pending が無い": func() model.Ingress { ingress := newTestIngress(); ingress.PendingRules = nil; return ingress }(),
			"pending が壊れている": func() model.Ingress {
				ingress := newTestIngress()
				ingress.PendingRules = datatypes.JSON(`{`)
				return ingress
			}(),
		} {
			operator := &fakeIngressOperator{}
			activities := newTestIngressActivities(&fakeIngressStore{ingress: ingress}, operator)
			runError := run(activities, activities.ApplyIngressRules, input)
			var applicationError *temporal.ApplicationError
			if !errors.As(runError, &applicationError) || !applicationError.NonRetryable() || len(operator.applied) != 0 {
				subTest.Errorf("%s: リトライしないエラーのはず: %v", name, runError)
			}
		}
	})
	testingT.Run("ingress を読めない、k8s への依頼が失敗したら、エラーになる(リトライされる)", func(subTest *testing.T) {
		activities := newTestIngressActivities(&fakeIngressStore{findError: errors.New("db down")}, &fakeIngressOperator{})
		if runError := run(activities, activities.ApplyIngressRules, input); runError == nil {
			subTest.Error("エラーになるはず")
		}
		activities = newTestIngressActivities(&fakeIngressStore{ingress: newTestIngress()}, &fakeIngressOperator{err: errors.New("k8s down")})
		if runError := run(activities, activities.ApplyIngressRules, input); runError == nil {
			subTest.Error("エラーになるはず")
		}
	})
	testingT.Run("名前の形が不正なエラーは、リトライしない", func(subTest *testing.T) {
		activities := newTestIngressActivities(&fakeIngressStore{ingress: newTestIngress()}, &fakeIngressOperator{err: fmt.Errorf("%w: x", ErrInvalidIngressName)})
		runError := run(activities, activities.ApplyIngressRules, input)
		var applicationError *temporal.ApplicationError
		if !errors.As(runError, &applicationError) || !applicationError.NonRetryable() {
			subTest.Errorf("リトライしないはず: %v", runError)
		}
	})
}

// TestIngressCommitAndDelete は、結果の確定、IngressRoute とレコードの削除、削除失敗の記録を、保存先とk8sに任せることを確認する。
func TestIngressCommitAndDelete(testingT *testing.T) {
	// 実行のための、Temporal の Activity のテスト環境。
	run := func(activityFunction any, input any) error {
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activityFunction)
		_, runError := testEnvironment.ExecuteActivity(activityFunction, input)
		return runError
	}
	store := &fakeIngressStore{ingress: newTestIngress()}
	operator := &fakeIngressOperator{}
	activities := newTestIngressActivities(store, operator)

	// 結果の確定(成功と失敗)。
	_ = run(activities.CommitIngressRulesResult, workflow.CommitIngressRulesResultInput{IngressID: "ingress-1", Succeeded: true})
	_ = run(activities.CommitIngressRulesResult, workflow.CommitIngressRulesResultInput{IngressID: "ingress-1", Succeeded: false})
	if len(store.commits) != 2 || store.commits[0] != "ingress-1|true" || store.commits[1] != "ingress-1|false" {
		testingT.Errorf("確定が渡るはず: %v", store.commits)
	}
	// IngressRoute の削除は、ingress の namespace と名前で行う。
	if deleteError := run(activities.DeleteIngressObject, workflow.DeleteIngressObjectInput{IngressID: "ingress-1"}); deleteError != nil || len(operator.deletedRoutes) != 1 || operator.deletedRoutes[0] != "project-1/ingress-1" {
		testingT.Errorf("namespace と名前で削除するはず: %v %v", deleteError, operator.deletedRoutes)
	}
	// レコードの削除と、削除失敗の記録。
	_ = run(activities.DeleteIngressRecord, workflow.DeleteIngressRecordInput{IngressID: "ingress-1"})
	_ = run(activities.MarkIngressDeleteFailed, workflow.MarkIngressDeleteFailedInput{IngressID: "ingress-1"})
	if len(store.deletedRecord) != 1 || len(store.failedMarks) != 1 {
		testingT.Errorf("保存先に任せるはず: %v %v", store.deletedRecord, store.failedMarks)
	}
	// ingress を読めないときは、IngressRoute の削除はエラーになる。
	broken := newTestIngressActivities(&fakeIngressStore{findError: errors.New("db down")}, operator)
	if deleteError := run(broken.DeleteIngressObject, workflow.DeleteIngressObjectInput{IngressID: "ingress-1"}); deleteError == nil {
		testingT.Error("エラーになるはず")
	}
}
