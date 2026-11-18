package activity

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"gorm.io/datatypes"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/workflow"
)

// fakeServiceStore は、テスト用の ServiceStore。決めた service を返し、呼び出しを覚える。
type fakeServiceStore struct {
	service       model.Service
	found         bool
	findError     error
	markedActive  []string
	updates       []string
	deletedRecord []string
	failedMarks   []string
	commits       []string
	markError     error
}

// CommitPortsApply は、結果を覚えて、決めたエラーを返す。
func (fakeStore *fakeServiceStore) CommitPortsApply(_ context.Context, serviceID string, succeeded bool) (bool, error) {
	fakeStore.commits = append(fakeStore.commits, fmt.Sprintf("%s|%v", serviceID, succeeded))
	return true, fakeStore.markError
}

// FindForWorker は、決めた service とエラーを返す。
func (fakeStore *fakeServiceStore) FindForWorker(_ context.Context, _ string) (model.Service, error) {
	return fakeStore.service, fakeStore.findError
}

// FindByDeployIDForWorker は、決めた service と、有無を返す。
func (fakeStore *fakeServiceStore) FindByDeployIDForWorker(_ context.Context, _ string) (model.Service, bool, error) {
	return fakeStore.service, fakeStore.found, fakeStore.findError
}

// CompleteCreation は、成功なら記録する値を、失敗なら状態の変更を覚えて、決めたエラーを返す。
func (fakeStore *fakeServiceStore) CompleteCreation(_ context.Context, serviceID string, succeeded bool, dnsName string, clusterIP string) (bool, error) {
	if succeeded {
		fakeStore.markedActive = append(fakeStore.markedActive, serviceID+"|"+dnsName+"|"+clusterIP)
	} else {
		fakeStore.updates = append(fakeStore.updates, model.ServiceStatusPending+"->"+model.ServiceStatusFailed)
	}
	return true, fakeStore.markError
}

// DeleteRecord は、ID を覚える。
func (fakeStore *fakeServiceStore) DeleteRecord(_ context.Context, serviceID string) error {
	fakeStore.deletedRecord = append(fakeStore.deletedRecord, serviceID)
	return fakeStore.markError
}

// MarkDeleteFailed は、ID を覚える。
func (fakeStore *fakeServiceStore) MarkDeleteFailed(_ context.Context, serviceID string) error {
	fakeStore.failedMarks = append(fakeStore.failedMarks, serviceID)
	return fakeStore.markError
}

// fakeServiceOperator は、テスト用の ServiceOperator。呼び出しを覚え、決めた結果を返す。
type fakeServiceOperator struct {
	appliedDefinition *ServiceDefinition
	state             ServiceState
	applyError        error
	deleted           []string
	deleteError       error
}

// ApplyService は、内容を覚えて、決めた結果を返す。
func (fakeOperator *fakeServiceOperator) ApplyService(_ context.Context, definition ServiceDefinition) (ServiceState, error) {
	fakeOperator.appliedDefinition = &definition
	return fakeOperator.state, fakeOperator.applyError
}

// DeleteService は、削除の依頼を覚えて、決めたエラーを返す。
func (fakeOperator *fakeServiceOperator) DeleteService(_ context.Context, namespaceName string, serviceName string) error {
	fakeOperator.deleted = append(fakeOperator.deleted, namespaceName+"/"+serviceName)
	return fakeOperator.deleteError
}

// newTestService は、テスト用の service を作る。
//
// 戻り値:
//   - web(8080、tcp)と game(27015、udp)の、pending の service。
func newTestServiceModel() model.Service {
	return model.Service{
		ID: "service-1", DeployID: "deploy-1", ProjectID: "project-1", Status: model.ServiceStatusPending,
		Ports: datatypes.JSON(`[{"name":"web","port":8080,"protocol":"tcp"},{"name":"game","port":27015,"protocol":"udp"}]`),
	}
}

// newTestServiceActivities は、fake を持つ ServiceActivities を作る。
func newTestServiceActivities(fakeStore *fakeServiceStore, fakeOperator *fakeServiceOperator) *ServiceActivities {
	// ログは捨てる入れ物に出す。
	return NewServiceActivities(fakeStore, fakeStore, fakeOperator, ServiceSettings{ServiceType: "ClusterIP", ClusterDomain: "test-cluster.local"}, newTestActivities(nil, nil).appLogger)
}

// TestEnsureService は、DB の service から、種類・ポート・向き先の deploy を組み立てて k8s に渡し、
// 払い出された ClusterIP と、ドメインを含む DNS 名を返すことを確認する。
func TestEnsureService(testingT *testing.T) {
	testingT.Run("service の内容から Service を組み立て、DNS 名(namespace とドメイン入り)と ClusterIP を返す", func(subTest *testing.T) {
		fakeOperator := &fakeServiceOperator{state: ServiceState{ClusterIP: "10.112.0.5"}}
		activities := newTestServiceActivities(&fakeServiceStore{service: newTestServiceModel()}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.EnsureService)

		encoded, executeError := testEnvironment.ExecuteActivity(activities.EnsureService, workflow.EnsureServiceInput{ServiceID: "service-1"})

		var result workflow.EnsureServiceResult
		if executeError != nil || encoded.Get(&result) != nil {
			subTest.Fatalf("成功するはず: %v", executeError)
		}
		if result.DNSName != "service-1.project-1.svc.test-cluster.local" || result.ClusterIP != "10.112.0.5" {
			subTest.Errorf("DNS 名に namespace とドメインが入り、ClusterIP が返るはず: %+v", result)
		}
		definition := fakeOperator.appliedDefinition
		if definition == nil || definition.Namespace != "project-1" || definition.Name != "service-1" || definition.DeployID != "deploy-1" || definition.Type != "ClusterIP" ||
			len(definition.Ports) != 2 || definition.Ports[0] != (ServicePortDefinition{Name: "web", Port: 8080, Protocol: "tcp"}) || definition.Ports[1].Protocol != "udp" {
			subTest.Errorf("内容が違う: %+v", definition)
		}
	})
	testingT.Run("設定の種類(LoadBalancer)が、k8s に渡る", func(subTest *testing.T) {
		fakeOperator := &fakeServiceOperator{}
		fakeStore := &fakeServiceStore{service: newTestServiceModel()}
		activities := NewServiceActivities(fakeStore, fakeStore, fakeOperator,
			ServiceSettings{ServiceType: "LoadBalancer", ClusterDomain: "cluster.local"}, newTestActivities(nil, nil).appLogger)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.EnsureService)
		_, _ = testEnvironment.ExecuteActivity(activities.EnsureService, workflow.EnsureServiceInput{ServiceID: "service-1"})
		if fakeOperator.appliedDefinition == nil || fakeOperator.appliedDefinition.Type != "LoadBalancer" {
			subTest.Errorf("LoadBalancer のはず: %+v", fakeOperator.appliedDefinition)
		}
	})
	testingT.Run("ポートの JSON が壊れていたら、リトライしないエラーで、何も作らない", func(subTest *testing.T) {
		broken := newTestServiceModel()
		broken.Ports = datatypes.JSON(`{"not":"an array"}`)
		fakeOperator := &fakeServiceOperator{}
		activities := newTestServiceActivities(&fakeServiceStore{service: broken}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.EnsureService)
		_, executeError := testEnvironment.ExecuteActivity(activities.EnsureService, workflow.EnsureServiceInput{ServiceID: "service-1"})
		var applicationError *temporal.ApplicationError
		if !errors.As(executeError, &applicationError) || !applicationError.NonRetryable() || fakeOperator.appliedDefinition != nil {
			subTest.Errorf("リトライしないエラーで、何も作らないはず: %v", executeError)
		}
	})
	testingT.Run("k8s への依頼の失敗は、リトライできるエラー。名前の形の不正は、リトライしないエラー", func(subTest *testing.T) {
		for _, testCase := range []struct {
			applyError error
			retryable  bool
		}{{errors.New("k8s down"), true}, {ErrInvalidServiceName, false}, {ErrInvalidNamespaceName, false}} {
			activities := newTestServiceActivities(&fakeServiceStore{service: newTestServiceModel()}, &fakeServiceOperator{applyError: testCase.applyError})
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.EnsureService)
			_, executeError := testEnvironment.ExecuteActivity(activities.EnsureService, workflow.EnsureServiceInput{ServiceID: "service-1"})
			var applicationError *temporal.ApplicationError
			nonRetryable := errors.As(executeError, &applicationError) && applicationError.NonRetryable()
			if executeError == nil || nonRetryable == testCase.retryable {
				subTest.Errorf("%v: リトライ可否が違う: %v", testCase.applyError, executeError)
			}
		}
	})
}

// TestMarkServiceResult は、成功なら active にして値を記録し、失敗なら pending から failed にすることを確認する。
func TestMarkServiceResult(testingT *testing.T) {
	testingT.Run("成功なら、DNS 名と ClusterIP を記録して active にする", func(subTest *testing.T) {
		fakeStore := &fakeServiceStore{}
		activities := newTestServiceActivities(fakeStore, &fakeServiceOperator{})
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.MarkServiceResult)
		_, executeError := testEnvironment.ExecuteActivity(activities.MarkServiceResult, workflow.MarkServiceResultInput{ServiceID: "service-1", Succeeded: true, DNSName: "dns", ClusterIP: "10.0.0.1"})
		if executeError != nil || len(fakeStore.markedActive) != 1 || fakeStore.markedActive[0] != "service-1|dns|10.0.0.1" || len(fakeStore.updates) != 0 {
			subTest.Errorf("active にして記録するはず: %v %v %v", executeError, fakeStore.markedActive, fakeStore.updates)
		}
	})
	testingT.Run("失敗なら、pending から failed にする", func(subTest *testing.T) {
		fakeStore := &fakeServiceStore{}
		activities := newTestServiceActivities(fakeStore, &fakeServiceOperator{})
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.MarkServiceResult)
		_, executeError := testEnvironment.ExecuteActivity(activities.MarkServiceResult, workflow.MarkServiceResultInput{ServiceID: "service-1", Succeeded: false})
		if executeError != nil || len(fakeStore.updates) != 1 || fakeStore.updates[0] != "pending->failed" || len(fakeStore.markedActive) != 0 {
			subTest.Errorf("pending から failed のはず: %v %v", executeError, fakeStore.updates)
		}
	})
	testingT.Run("保存先のエラーは、返す(リトライされる)", func(subTest *testing.T) {
		activities := newTestServiceActivities(&fakeServiceStore{markError: errors.New("database down")}, &fakeServiceOperator{})
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.MarkServiceResult)
		if _, executeError := testEnvironment.ExecuteActivity(activities.MarkServiceResult, workflow.MarkServiceResultInput{ServiceID: "service-1", Succeeded: true, DNSName: "dns"}); executeError == nil {
			subTest.Errorf("エラーのはず")
		}
	})
}

// TestServiceDeletionActivities は、削除の Activity が、namespace と名前で k8s に頼み、行が無ければ成功とし、
// 名前の不正はリトライしないエラーにし、レコードの削除と失敗の記録を、保存先に渡すことを確認する。
func TestServiceDeletionActivities(testingT *testing.T) {
	testingT.Run("DeleteServiceObject は、プロジェクトの ID と service の ID で、Service の削除を頼む", func(subTest *testing.T) {
		fakeOperator := &fakeServiceOperator{}
		activities := newTestServiceActivities(&fakeServiceStore{service: newTestServiceModel()}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteServiceObject)
		_, executeError := testEnvironment.ExecuteActivity(activities.DeleteServiceObject, workflow.DeleteServiceObjectInput{ServiceID: "service-1"})
		if executeError != nil || len(fakeOperator.deleted) != 1 || fakeOperator.deleted[0] != "project-1/service-1" {
			subTest.Errorf("project-1/service-1 の削除を頼むはず: %v %v", executeError, fakeOperator.deleted)
		}
	})
	testingT.Run("DeleteServiceObject は、service の行がすでに無ければ、何もせず成功する", func(subTest *testing.T) {
		fakeOperator := &fakeServiceOperator{}
		notFound := apperror.New(apperror.KindNotFound, "service が見つかりません")
		activities := newTestServiceActivities(&fakeServiceStore{findError: notFound}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteServiceObject)
		_, executeError := testEnvironment.ExecuteActivity(activities.DeleteServiceObject, workflow.DeleteServiceObjectInput{ServiceID: "service-1"})
		if executeError != nil || len(fakeOperator.deleted) != 0 {
			subTest.Errorf("成功で、何も頼まないはず: %v %v", executeError, fakeOperator.deleted)
		}
	})
	testingT.Run("DeleteServiceObject は、名前の不正をリトライしないエラーにし、k8s の失敗はリトライできるエラーにする", func(subTest *testing.T) {
		for _, testCase := range []struct {
			deleteError error
			retryable   bool
		}{{ErrInvalidServiceName, false}, {errors.New("k8s down"), true}} {
			activities := newTestServiceActivities(&fakeServiceStore{service: newTestServiceModel()}, &fakeServiceOperator{deleteError: testCase.deleteError})
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.DeleteServiceObject)
			_, executeError := testEnvironment.ExecuteActivity(activities.DeleteServiceObject, workflow.DeleteServiceObjectInput{ServiceID: "service-1"})
			var applicationError *temporal.ApplicationError
			nonRetryable := errors.As(executeError, &applicationError) && applicationError.NonRetryable()
			if executeError == nil || nonRetryable == testCase.retryable {
				subTest.Errorf("%v: リトライ可否が違う: %v", testCase.deleteError, executeError)
			}
		}
	})
	testingT.Run("DeleteServiceRecord と MarkServiceDeleteFailed は、ID を保存先に渡す", func(subTest *testing.T) {
		fakeStore := &fakeServiceStore{}
		activities := newTestServiceActivities(fakeStore, &fakeServiceOperator{})
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteServiceRecord)
		testEnvironment.RegisterActivity(activities.MarkServiceDeleteFailed)
		_, recordError := testEnvironment.ExecuteActivity(activities.DeleteServiceRecord, workflow.DeleteServiceRecordInput{ServiceID: "service-1"})
		_, failedError := testEnvironment.ExecuteActivity(activities.MarkServiceDeleteFailed, workflow.MarkServiceDeleteFailedInput{ServiceID: "service-1"})
		if recordError != nil || failedError != nil || len(fakeStore.deletedRecord) != 1 || fakeStore.deletedRecord[0] != "service-1" || len(fakeStore.failedMarks) != 1 {
			subTest.Errorf("ID が渡るはず: %v %v %v %v", recordError, failedError, fakeStore.deletedRecord, fakeStore.failedMarks)
		}
	})
}

// TestDeleteDeployService は、deploy の削除のとき、その deploy の service の k8s の Service を削除し、
// service が無い deploy では何もしないことを確認する。
func TestDeleteDeployService(testingT *testing.T) {
	testingT.Run("deploy の service の、プロジェクトの ID と service の ID で、Service の削除を頼む", func(subTest *testing.T) {
		fakeOperator := &fakeServiceOperator{}
		activities := newTestServiceActivities(&fakeServiceStore{service: newTestServiceModel(), found: true}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteDeployService)
		_, executeError := testEnvironment.ExecuteActivity(activities.DeleteDeployService, workflow.DeleteDeployServiceInput{DeployID: "deploy-1"})
		if executeError != nil || len(fakeOperator.deleted) != 1 || fakeOperator.deleted[0] != "project-1/service-1" {
			subTest.Errorf("project-1/service-1 の削除を頼むはず: %v %v", executeError, fakeOperator.deleted)
		}
	})
	testingT.Run("service が無い deploy では、何もせず成功する", func(subTest *testing.T) {
		fakeOperator := &fakeServiceOperator{}
		activities := newTestServiceActivities(&fakeServiceStore{found: false}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteDeployService)
		_, executeError := testEnvironment.ExecuteActivity(activities.DeleteDeployService, workflow.DeleteDeployServiceInput{DeployID: "deploy-1"})
		if executeError != nil || len(fakeOperator.deleted) != 0 {
			subTest.Errorf("成功で、何も頼まないはず: %v %v", executeError, fakeOperator.deleted)
		}
	})
	testingT.Run("k8s の失敗は、リトライできるエラー。DB の失敗も、返す", func(subTest *testing.T) {
		activities := newTestServiceActivities(&fakeServiceStore{service: newTestServiceModel(), found: true}, &fakeServiceOperator{deleteError: errors.New("k8s down")})
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.DeleteDeployService)
		if _, executeError := testEnvironment.ExecuteActivity(activities.DeleteDeployService, workflow.DeleteDeployServiceInput{DeployID: "deploy-1"}); executeError == nil {
			subTest.Errorf("エラーのはず")
		}
		dbActivities := newTestServiceActivities(&fakeServiceStore{findError: errors.New("database down")}, &fakeServiceOperator{})
		dbEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		dbEnvironment.RegisterActivity(dbActivities.DeleteDeployService)
		if _, executeError := dbEnvironment.ExecuteActivity(dbActivities.DeleteDeployService, workflow.DeleteDeployServiceInput{DeployID: "deploy-1"}); executeError == nil {
			subTest.Errorf("DB の失敗は、返すはず")
		}
	})
}

// newPendingPortsService は、現在のポートと、pending のポートが違う service を作る。
//
// 戻り値:
//   - ポートは web(8080、tcp)、pending は web(8080、tcp)と metrics(9090、tcp)の、active の service。
func newPendingPortsService() model.Service {
	service := newTestServiceModel()
	service.Status = model.ServiceStatusActive
	service.PendingPorts = datatypes.JSON(`[{"name":"web","port":8080,"protocol":"tcp"},{"name":"metrics","port":9090,"protocol":"tcp"}]`)
	service.Ports = datatypes.JSON(`[{"name":"web","port":8080,"protocol":"tcp"}]`)
	return service
}

// TestApplyServicePorts は、k8s に渡る内容が、現在のポートではなく pending のポートになること、
// 適用するポートが無い・壊れているときはリトライしないエラーになることを確認する。
func TestApplyServicePorts(testingT *testing.T) {
	testingT.Run("pending のポートで Service を更新する(名前・namespace・向き先・種類は、そのまま)", func(subTest *testing.T) {
		fakeOperator := &fakeServiceOperator{}
		activities := newTestServiceActivities(&fakeServiceStore{service: newPendingPortsService()}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyServicePorts)

		_, executeError := testEnvironment.ExecuteActivity(activities.ApplyServicePorts, workflow.ApplyServicePortsActivityInput{ServiceID: "service-1"})

		definition := fakeOperator.appliedDefinition
		if executeError != nil || definition == nil || len(definition.Ports) != 2 || definition.Ports[1] != (ServicePortDefinition{Name: "metrics", Port: 9090, Protocol: "tcp"}) {
			subTest.Fatalf("pending の2つのポートのはず: %v %+v", executeError, definition)
		}
		if definition.Namespace != "project-1" || definition.Name != "service-1" || definition.DeployID != "deploy-1" || definition.Type != "ClusterIP" {
			subTest.Errorf("向き先と種類は変わらないはず: %+v", definition)
		}
	})
	for _, broken := range []struct {
		name   string
		modify func(service *model.Service)
	}{
		{"適用するポートが無ければ、リトライしないエラー", func(service *model.Service) { service.PendingPorts = nil }},
		{"pending のポートの JSON が壊れていたら、リトライしないエラー", func(service *model.Service) { service.PendingPorts = datatypes.JSON(`{"x":1}`) }},
	} {
		testingT.Run(broken.name, func(subTest *testing.T) {
			service := newPendingPortsService()
			broken.modify(&service)
			fakeOperator := &fakeServiceOperator{}
			activities := newTestServiceActivities(&fakeServiceStore{service: service}, fakeOperator)
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.ApplyServicePorts)
			_, executeError := testEnvironment.ExecuteActivity(activities.ApplyServicePorts, workflow.ApplyServicePortsActivityInput{ServiceID: "service-1"})
			var applicationError *temporal.ApplicationError
			if !errors.As(executeError, &applicationError) || !applicationError.NonRetryable() || fakeOperator.appliedDefinition != nil {
				subTest.Errorf("リトライしないエラーで、何も更新しないはず: %v", executeError)
			}
		})
	}
	testingT.Run("k8s の失敗は、リトライできるエラー。名前の形の不正は、リトライしないエラー", func(subTest *testing.T) {
		for _, testCase := range []struct {
			applyError error
			retryable  bool
		}{{errors.New("k8s down"), true}, {ErrInvalidServiceName, false}} {
			activities := newTestServiceActivities(&fakeServiceStore{service: newPendingPortsService()}, &fakeServiceOperator{applyError: testCase.applyError})
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.ApplyServicePorts)
			_, executeError := testEnvironment.ExecuteActivity(activities.ApplyServicePorts, workflow.ApplyServicePortsActivityInput{ServiceID: "service-1"})
			var applicationError *temporal.ApplicationError
			nonRetryable := errors.As(executeError, &applicationError) && applicationError.NonRetryable()
			if executeError == nil || nonRetryable == testCase.retryable {
				subTest.Errorf("%v: リトライ可否が違う: %v", testCase.applyError, executeError)
			}
		}
	})
}

// TestRollbackServicePorts は、巻き戻しが、pending ではなく、現在の(動いていた)ポートで Service を更新することを確認する。
func TestRollbackServicePorts(testingT *testing.T) {
	testingT.Run("現在のポート(pending ではない)で Service を戻す", func(subTest *testing.T) {
		fakeOperator := &fakeServiceOperator{}
		activities := newTestServiceActivities(&fakeServiceStore{service: newPendingPortsService()}, fakeOperator)
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.RollbackServicePorts)

		_, executeError := testEnvironment.ExecuteActivity(activities.RollbackServicePorts, workflow.ApplyServicePortsActivityInput{ServiceID: "service-1"})

		definition := fakeOperator.appliedDefinition
		if executeError != nil || definition == nil || len(definition.Ports) != 1 || definition.Ports[0].Name != "web" {
			subTest.Errorf("現在の1つのポート(web)のはず: %v %+v", executeError, definition)
		}
	})
	testingT.Run("k8s の失敗は、リトライできるエラー", func(subTest *testing.T) {
		activities := newTestServiceActivities(&fakeServiceStore{service: newPendingPortsService()}, &fakeServiceOperator{applyError: errors.New("k8s down")})
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.RollbackServicePorts)
		_, executeError := testEnvironment.ExecuteActivity(activities.RollbackServicePorts, workflow.ApplyServicePortsActivityInput{ServiceID: "service-1"})
		var applicationError *temporal.ApplicationError
		if executeError == nil || (errors.As(executeError, &applicationError) && applicationError.NonRetryable()) {
			subTest.Errorf("リトライできるエラーのはず: %v", executeError)
		}
	})
}

// TestCommitServicePortsResult は、成否が保存先にそのまま渡され、保存先のエラーが返ることを確認する。
func TestCommitServicePortsResult(testingT *testing.T) {
	for _, testCase := range []struct {
		name        string
		succeeded   bool
		commitError error
	}{{"成功を渡す", true, nil}, {"失敗を渡す", false, nil}, {"保存先のエラーは、返す(リトライされる)", true, errors.New("database down")}} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			fakeStore := &fakeServiceStore{markError: testCase.commitError}
			activities := newTestServiceActivities(fakeStore, &fakeServiceOperator{})
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.CommitServicePortsResult)
			_, executeError := testEnvironment.ExecuteActivity(activities.CommitServicePortsResult, workflow.CommitServicePortsResultInput{ServiceID: "service-1", Succeeded: testCase.succeeded})
			if (executeError != nil) != (testCase.commitError != nil) || len(fakeStore.commits) != 1 || fakeStore.commits[0] != fmt.Sprintf("service-1|%v", testCase.succeeded) {
				subTest.Errorf("成否が渡るはず: %v %v", executeError, fakeStore.commits)
			}
		})
	}
}
