package service

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// ServiceStore は、ServiceService が必要とする、service の保存先の操作。
//
// 実体は shared/repository の ServiceRepository。テストでは fake に差し替える。
type ServiceStore interface {
	Create(requestContext context.Context, service *model.Service) error
	FindByDeployID(requestContext context.Context, userID string, deployID string) (model.Service, string, error)
	DeleteUnstarted(requestContext context.Context, serviceID string) error
	MarkDeleting(requestContext context.Context, userID string, deployID string) (string, string, error)
	RevertDeleting(requestContext context.Context, serviceID string, previousStatus string) error
	SavePendingPorts(requestContext context.Context, userID string, deployID string, ports datatypes.JSON) error
	DiscardPendingPorts(requestContext context.Context, userID string, deployID string) error
	BeginPortsApply(requestContext context.Context, userID string, deployID string) (string, string, error)
	RevertPortsApply(requestContext context.Context, deployID string, previousStatus string) error
}

// ServiceWorkflowStarter は、service の作成・削除の処理(Workflow)を開始する。
//
// 実体は TemporalServiceWorkflowStarter。テストでは fake に差し替える。
type ServiceWorkflowStarter interface {
	// StartServiceCreation は、service を k8s の Service として作る処理を開始する。完了は待たない。
	StartServiceCreation(requestContext context.Context, serviceID string) error

	// StartServiceDeletion は、service を削除する処理を開始する。完了は待たない。
	StartServiceDeletion(requestContext context.Context, serviceID string) error

	// StartPortsApply は、service の pending のポートを、k8s の Service に反映する処理を開始する。完了は待たない。
	StartPortsApply(requestContext context.Context, serviceID string) error
}

// ServiceService は、service(deploy のポートの公開)に関する操作のビジネスルールを担当する。
type ServiceService struct {
	serviceStore           ServiceStore
	serviceWorkflowStarter ServiceWorkflowStarter
}

// NewServiceService は、ServiceService を作る。
//
// 引数:
//   - serviceStore: service の保存先。
//   - serviceWorkflowStarter: service の作成・削除の処理を開始するもの。
//
// 戻り値:
//   - 作成した ServiceService。
func NewServiceService(serviceStore ServiceStore, serviceWorkflowStarter ServiceWorkflowStarter) *ServiceService {
	// 保存先と、処理の開始役を持たせた ServiceService を返す。
	return &ServiceService{serviceStore: serviceStore, serviceWorkflowStarter: serviceWorkflowStarter}
}

// Create は、deploy に service を作り、k8s の Service を作る処理を開始する。
//
// 処理の流れ:
//  1. service を pending 状態で保存する(deploy が無い・他人のものなら not_found、削除の途中・すでに service があるなら conflict)
//  2. 作成の Workflow を開始する(完了は待たない。作れると、status が active になり、DNS 名と ClusterIP が入る)
//  3. 手順 2 に失敗したら、手順 1 の保存を取り消す(作られない service を残さない)
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: service を作る deploy の ID。
//   - ports: 公開するポート(検査済みのもの)。
//
// 戻り値:
//   - 保存した service(status は pending)。
//   - 上記のエラー。Workflow を開始できなかったときは internal_error(保存は取り消してある)。
func (serviceService *ServiceService) Create(requestContext context.Context, userID string, deployID string, ports []model.ServicePort) (model.Service, error) {
	// ポートを JSON にして、service を組み立てる。
	portsJSON, marshalError := json.Marshal(ports)
	if marshalError != nil {
		return model.Service{}, apperror.Wrap(apperror.KindInternal, "ポートを保存できませんでした", marshalError)
	}
	service := model.Service{ID: model.ServiceIDPrefix + uuid.NewString(), DeployID: deployID, UserID: userID, Ports: datatypes.JSON(portsJSON)}

	// 1. pending で保存する。
	if createError := serviceService.serviceStore.Create(requestContext, &service); createError != nil {
		return model.Service{}, createError
	}

	// 2. 作成の Workflow を開始する。成功したら、保存した service を返す。
	startError := serviceService.serviceWorkflowStarter.StartServiceCreation(requestContext, service.ID)
	if startError == nil {
		return service, nil
	}

	// 3. 開始できなかったので、保存を取り消す。リクエストが中断されていても取り消せるよう、context を切り離す。
	if cancelError := serviceService.serviceStore.DeleteUnstarted(context.WithoutCancel(requestContext), service.ID); cancelError != nil {
		// 取り消しにも失敗したときは、両方の原因を残す(pending の service が残る)。
		return model.Service{}, apperror.Wrap(apperror.KindInternal, "service の作成を開始できませんでした", joinErrors(startError, cancelError))
	}
	return model.Service{}, apperror.Wrap(apperror.KindInternal, "service の作成を開始できませんでした", startError)
}

// Get は、ユーザーの deploy の service を1件取得する。
//
// deploy が削除中(deleting)のときは、deploy の削除が service も片付けるので、service の status も、deleting として返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - service。
//   - deploy が無い、または他のユーザーのもの、または service が無いなら not_found。
func (serviceService *ServiceService) Get(requestContext context.Context, userID string, deployID string) (model.Service, error) {
	// ユーザーと deploy ID の両方で探す。他のユーザーのものは、見つからない扱いになる。
	service, deployStatus, findError := serviceService.serviceStore.FindByDeployID(requestContext, userID, deployID)
	if findError != nil {
		return model.Service{}, findError
	}
	// deploy が削除中なら、service も削除中として見せる(仕様)。
	if deployStatus == model.DeployStatusDeleting {
		service.Status = model.ServiceStatusDeleting
	}
	return service, nil
}

// Delete は、service の削除を受け付け、実体の削除を開始する。
//
// 処理の流れ:
//  1. service の状態を deleting にする(作成中・すでに削除中・deploy が削除の途中なら conflict。failed からは、やり直しになる)
//  2. 削除の Workflow を開始する(完了は待たない。完了すると、レコードが消えて GET が 404 になる)
//  3. 手順 2 に失敗したら、状態を元に戻す(削除中のまま残さない)
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: service が属する deploy の ID。
//
// 戻り値:
//   - deploy や service が無い、または他のユーザーのものなら not_found。受け付けられない状態なら conflict。
//   - Workflow を開始できなかったときは internal_error(状態は元に戻してある)。
func (serviceService *ServiceService) Delete(requestContext context.Context, userID string, deployID string) error {
	// 1. 状態を deleting にする。元の状態は、開始に失敗したときの巻き戻しに使う。
	serviceID, previousStatus, markError := serviceService.serviceStore.MarkDeleting(requestContext, userID, deployID)
	if markError != nil {
		return markError
	}

	// 2. 削除の Workflow を開始する。
	startError := serviceService.serviceWorkflowStarter.StartServiceDeletion(requestContext, serviceID)
	if startError == nil {
		return nil
	}

	// 3. 開始できなかったので、状態を元に戻す。リクエストが中断されていても戻せるよう、context を切り離す。
	if revertError := serviceService.serviceStore.RevertDeleting(context.WithoutCancel(requestContext), serviceID, previousStatus); revertError != nil {
		// 戻せなかったときは、両方の原因を残す(deleting のまま残る)。
		return apperror.Wrap(apperror.KindInternal, "service の削除を開始できませんでした", joinErrors(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "service の削除を開始できませんでした", startError)
}
