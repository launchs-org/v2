package workflow

import (
	"time"

	"github.com/google/uuid"
)

const (
	// CreateServiceWorkflowName は、service を k8s の Service として作る Workflow の名前。
	CreateServiceWorkflowName = "CreateServiceWorkflow"

	// EnsureServiceActivityName は、service に対応する k8s の Service を作る(または更新する) Activity の名前。
	EnsureServiceActivityName = "EnsureService"

	// MarkServiceResultActivityName は、service の作成の結果(active または failed)を DB に記録する Activity の名前。
	MarkServiceResultActivityName = "MarkServiceResult"

	// DeleteServiceWorkflowName は、service を削除する Workflow の名前。
	DeleteServiceWorkflowName = "DeleteServiceWorkflow"

	// DeleteServiceObjectActivityName は、service に対応する k8s の Service を削除する Activity の名前。
	DeleteServiceObjectActivityName = "DeleteServiceObject"

	// DeleteServiceRecordActivityName は、service を DB から削除する Activity の名前。
	DeleteServiceRecordActivityName = "DeleteServiceRecord"

	// MarkServiceDeleteFailedActivityName は、削除に失敗した service を failed にする Activity の名前。
	MarkServiceDeleteFailedActivityName = "MarkServiceDeleteFailed"

	// DeleteDeployServiceActivityName は、deploy の削除のとき、その deploy の service の k8s の Service を削除する Activity の名前。
	// service が無い deploy では、何もしない。
	DeleteDeployServiceActivityName = "DeleteDeployService"

	// CreateServiceWorkflowTimeout は、service 作成の Workflow 全体の最長の実行時間(仮)。
	CreateServiceWorkflowTimeout = 15 * time.Minute
)

// CreateServiceInput は、CreateServiceWorkflow の入力。
type CreateServiceInput struct {
	// ServiceID は、作成する service の ID。内容は、Activity が DB から読む。
	ServiceID string
}

// EnsureServiceInput は、EnsureService Activity の入力。
type EnsureServiceInput struct {
	// ServiceID は、k8s の Service を作る service の ID。
	ServiceID string
}

// EnsureServiceResult は、EnsureService Activity の結果。k8s が払い出した値と、そこから決めた DNS 名。
type EnsureServiceResult struct {
	// DNSName は、service の DNS 名("<serviceId>.<projectId>.svc.<クラスターのドメイン>")。
	DNSName string

	// ClusterIP は、k8s が払い出した ClusterIP。払い出されないとき(None など)は空。
	ClusterIP string
}

// MarkServiceResultInput は、MarkServiceResult Activity の入力。
type MarkServiceResultInput struct {
	// ServiceID は、結果を記録する service の ID。
	ServiceID string

	// Succeeded は、k8s の Service を作れたか。true なら active にして、DNSName と ClusterIP を記録する。false なら failed にする。
	Succeeded bool

	// DNSName と ClusterIP は、成功したときに記録する値。
	DNSName   string
	ClusterIP string
}

// DeleteServiceInput は、DeleteServiceWorkflow の入力。
type DeleteServiceInput struct {
	// ServiceID は、削除する service の ID。
	ServiceID string
}

// DeleteServiceObjectInput は、DeleteServiceObject Activity の入力。
type DeleteServiceObjectInput struct {
	// ServiceID は、k8s の Service を削除する service の ID。
	ServiceID string
}

// DeleteServiceRecordInput は、DeleteServiceRecord Activity の入力。
type DeleteServiceRecordInput struct {
	// ServiceID は、DB から削除する service の ID。
	ServiceID string
}

// MarkServiceDeleteFailedInput は、MarkServiceDeleteFailed Activity の入力。
type MarkServiceDeleteFailedInput struct {
	// ServiceID は、削除に失敗した service の ID。
	ServiceID string
}

// DeleteDeployServiceInput は、DeleteDeployService Activity の入力。
type DeleteDeployServiceInput struct {
	// DeployID は、削除される deploy の ID。その deploy の service の k8s の Service を削除する。
	DeployID string
}

// NewCreateServiceWorkflowID は、service 作成の Workflow ID を作る。形式は `{操作}-{リソースID}-{uuid}`。
//
// 引数:
//   - serviceID: 作成する service の ID。
//
// 戻り値:
//   - Workflow ID。
func NewCreateServiceWorkflowID(serviceID string) string {
	// 操作ごとに一意にするため、末尾に UUID を付ける。
	return "create-" + serviceID + "-" + uuid.NewString()
}

// NewDeleteServiceWorkflowID は、service 削除の Workflow ID を作る。形式は `{操作}-{リソースID}-{uuid}`。
//
// 引数:
//   - serviceID: 削除する service の ID。
//
// 戻り値:
//   - Workflow ID。
func NewDeleteServiceWorkflowID(serviceID string) string {
	// 操作ごとに一意にするため、末尾に UUID を付ける。
	return "delete-" + serviceID + "-" + uuid.NewString()
}
