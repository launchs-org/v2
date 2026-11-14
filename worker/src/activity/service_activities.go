package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"go.temporal.io/sdk/temporal"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/workflow"
)

// ServiceStore は、ServiceActivities が必要とする、service の保存先の操作。
//
// 実体は shared/repository の ServiceRepository。
type ServiceStore interface {
	// FindForWorker は、service を ID で1件取得する。
	FindForWorker(requestContext context.Context, serviceID string) (model.Service, error)

	// FindByDeployIDForWorker は、deploy の service を取得する。無ければ false。
	FindByDeployIDForWorker(requestContext context.Context, deployID string) (model.Service, bool, error)

	// MarkActive は、pending の service を active にして、DNS 名と ClusterIP を記録する。
	MarkActive(requestContext context.Context, serviceID string, dnsName string, clusterIP string) (bool, error)

	// UpdateStatus は、期待する状態のときだけ、状態を変える。
	UpdateStatus(requestContext context.Context, serviceID string, fromStatus string, toStatus string) (bool, error)

	// DeleteRecord は、削除中の service を、DB から物理削除する。
	DeleteRecord(requestContext context.Context, serviceID string) error

	// MarkDeleteFailed は、削除中の service を、failed にする。
	MarkDeleteFailed(requestContext context.Context, serviceID string) error
}

// ServiceSettings は、ServiceActivities の設定。
type ServiceSettings struct {
	// ServiceType は、作る Service の種類("ClusterIP"、"NodePort"、"LoadBalancer")。環境変数 SERVICE_TYPE。
	ServiceType string

	// ClusterDomain は、クラスターのドメイン(例: cluster.local)。DNS 名の最後の部分。環境変数 CLUSTER_DOMAIN。
	ClusterDomain string
}

// ServiceActivities は、service の作成・削除の Workflow が呼ぶ Activity をまとめる。
type ServiceActivities struct {
	serviceStore    ServiceStore
	serviceOperator ServiceOperator
	settings        ServiceSettings
	appLogger       *slog.Logger
}

// NewServiceActivities は、ServiceActivities を作る。
//
// 引数:
//   - serviceStore: service の保存先。
//   - serviceOperator: k8s の Service を操作するもの。
//   - settings: Service の種類と、クラスターのドメイン。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した ServiceActivities。
func NewServiceActivities(serviceStore ServiceStore, serviceOperator ServiceOperator, settings ServiceSettings, appLogger *slog.Logger) *ServiceActivities {
	// 依存するものを持たせて返す。
	return &ServiceActivities{serviceStore: serviceStore, serviceOperator: serviceOperator, settings: settings, appLogger: appLogger}
}

// EnsureService は、service に対応する k8s の Service を作る(すでにあれば更新する)。
//
// DB の service から、k8s に作る Service の内容(種類、ポート、向き先の deploy)を組み立てて、k8s を操作するものに渡す。
// 払い出された ClusterIP と、そこから決めた DNS 名を返す。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: Service を作る service の ID。
//
// 戻り値:
//   - DNS 名と ClusterIP。
//   - service を読めなかったとき、ポートの JSON が壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (serviceActivities *ServiceActivities) EnsureService(requestContext context.Context, input workflow.EnsureServiceInput) (workflow.EnsureServiceResult, error) {
	// どの Activity が何のために動いているかを、ログに残す。
	serviceActivities.appLogger.Info("k8s の Service を作る", "service_id", input.ServiceID, "activity", workflow.EnsureServiceActivityName)

	// DB から service を読む。
	service, findError := serviceActivities.serviceStore.FindForWorker(requestContext, input.ServiceID)
	if findError != nil {
		return workflow.EnsureServiceResult{}, findError
	}
	// ポートの JSON を読む。壊れていたら、何度やっても同じなのでリトライしない。
	var ports []model.ServicePort
	if unmarshalError := json.Unmarshal(service.Ports, &ports); unmarshalError != nil {
		return workflow.EnsureServiceResult{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("ポートを読み取れません: %v", unmarshalError), "InvalidService", unmarshalError)
	}
	definition := ServiceDefinition{Namespace: service.ProjectID, Name: service.ID, DeployID: service.DeployID, Type: serviceActivities.settings.ServiceType}
	for _, port := range ports {
		definition.Ports = append(definition.Ports, ServicePortDefinition{Name: port.Name, Port: int32(port.Port), Protocol: port.Protocol})
	}

	// 作成は、k8s を操作するものに任せる。名前の形の不正は、リトライしない印を付ける。
	state, applyError := serviceActivities.serviceOperator.ApplyService(requestContext, definition)
	if applyError != nil {
		return workflow.EnsureServiceResult{}, markNonRetryable(applyError)
	}
	// DNS 名を決める(Kubernetes の Service の正式な名前は、<名前>.<namespace>.svc.<クラスターのドメイン>)。
	dnsName := fmt.Sprintf("%s.%s.svc.%s", service.ID, service.ProjectID, serviceActivities.settings.ClusterDomain)
	return workflow.EnsureServiceResult{DNSName: dnsName, ClusterIP: state.ClusterIP}, nil
}

// MarkServiceResult は、service の作成の結果を、DB に記録する。
//
// pending の service だけを変える(別の処理が先に状態を変えていたら、上書きしない)。
//   - 成功: active にして、DNS 名と ClusterIP を記録する
//   - 失敗: failed にする
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 結果を記録する service の ID と、成功したか、記録する値。
//
// 戻り値:
//   - 状態を更新できなかったときのエラー。
func (serviceActivities *ServiceActivities) MarkServiceResult(requestContext context.Context, input workflow.MarkServiceResultInput) error {
	// 失敗のときは、運用で気づけるよう、警告にする。
	if !input.Succeeded {
		serviceActivities.appLogger.Warn("service の作成に失敗した", "service_id", input.ServiceID, "activity", workflow.MarkServiceResultActivityName)
		_, failError := serviceActivities.serviceStore.UpdateStatus(requestContext, input.ServiceID, model.ServiceStatusPending, model.ServiceStatusFailed)
		return failError
	}
	// 成功のときは、active にして、払い出された値を記録する(変えられなくても、別の処理が先に決めたので、エラーにしない)。
	serviceActivities.appLogger.Info("service を作った", "service_id", input.ServiceID, "dns_name", input.DNSName, "cluster_ip", input.ClusterIP, "activity", workflow.MarkServiceResultActivityName)
	_, markError := serviceActivities.serviceStore.MarkActive(requestContext, input.ServiceID, input.DNSName, input.ClusterIP)
	return markError
}

// DeleteServiceObject は、service に対応する k8s の Service を削除する。
//
// 冪等。service の行がすでに無いときは、消すものが無いので、成功にする。名前の形が不正なときは、リトライしないエラーにする。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: k8s の Service を削除する service の ID。
//
// 戻り値:
//   - service を読めなかったとき、k8s への依頼に失敗したとき、名前の形が不正なとき(リトライしない)のエラー。
func (serviceActivities *ServiceActivities) DeleteServiceObject(requestContext context.Context, input workflow.DeleteServiceObjectInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	serviceActivities.appLogger.Info("k8s の Service を削除する", "service_id", input.ServiceID, "activity", workflow.DeleteServiceObjectActivityName)

	// namespace の名前(プロジェクトの ID)を知るため、service を読む。
	service, findError := serviceActivities.serviceStore.FindForWorker(requestContext, input.ServiceID)
	if apperror.KindOf(findError) == apperror.KindNotFound {
		// すでに行が無いので、消すものも無い。
		return nil
	}
	if findError != nil {
		return findError
	}
	// 削除は、k8s を操作するものに任せる。
	return markNonRetryable(serviceActivities.serviceOperator.DeleteService(requestContext, service.ProjectID, service.ID))
}

// DeleteServiceRecord は、service を DB から削除する。
//
// 冪等。削除中でない service は消さない(ServiceRepository.DeleteRecord が守る)。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除する service の ID。
//
// 戻り値:
//   - データベースのエラー。
func (serviceActivities *ServiceActivities) DeleteServiceRecord(requestContext context.Context, input workflow.DeleteServiceRecordInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	serviceActivities.appLogger.Info("service のレコードを削除する", "service_id", input.ServiceID, "activity", workflow.DeleteServiceRecordActivityName)
	// 削除は、保存先に任せる。
	return serviceActivities.serviceStore.DeleteRecord(requestContext, input.ServiceID)
}

// MarkServiceDeleteFailed は、削除に失敗した service を、failed にする(補償)。
//
// 冪等。削除中でなければ、何もしない。failed の service は、もう一度の削除を受け付ける。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除に失敗した service の ID。
//
// 戻り値:
//   - データベースのエラー。
func (serviceActivities *ServiceActivities) MarkServiceDeleteFailed(requestContext context.Context, input workflow.MarkServiceDeleteFailedInput) error {
	// 運用で気づけるよう、警告で残す。
	serviceActivities.appLogger.Warn("service の削除に失敗したので failed にする", "service_id", input.ServiceID, "activity", workflow.MarkServiceDeleteFailedActivityName)
	// 状態の変更は、保存先に任せる。
	return serviceActivities.serviceStore.MarkDeleteFailed(requestContext, input.ServiceID)
}

// DeleteDeployService は、deploy の削除のとき、その deploy の service の、k8s の Service を削除する。
//
// 冪等。service が無い deploy では、何もしない。DB の service の行は、deploy の行を消すときに、一緒に消える(CASCADE)。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除される deploy の ID。
//
// 戻り値:
//   - データベースのエラー、k8s への依頼の失敗、名前の形が不正なとき(リトライしない)のエラー。
func (serviceActivities *ServiceActivities) DeleteDeployService(requestContext context.Context, input workflow.DeleteDeployServiceInput) error {
	// どの Activity が何のために動いているかを、ログに残す。
	serviceActivities.appLogger.Info("deploy の service の k8s の Service を削除する", "deploy_id", input.DeployID, "activity", workflow.DeleteDeployServiceActivityName)
	// deploy の service を探す。無ければ、何もしない。
	service, found, findError := serviceActivities.serviceStore.FindByDeployIDForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}
	if !found {
		return nil
	}
	// 削除は、k8s を操作するものに任せる。
	return markNonRetryable(serviceActivities.serviceOperator.DeleteService(requestContext, service.ProjectID, service.ID))
}
