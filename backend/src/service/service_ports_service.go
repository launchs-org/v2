package service

import (
	"context"
	"encoding/json"

	"gorm.io/datatypes"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// UpdatePorts は、service のポートを、まだ適用しない値(pending_ports)として保存する。
//
// ポートは、常にまとめて置き換える。k8s の Service への反映は、適用(apply)を呼ぶまで行われない。
// 現在のポートと同じ内容なら、変更が無いものとして、pending_ports は保存しない(以前の pending_ports も消える)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: service が属する deploy の ID。
//   - ports: 新しいポート(検査済みのもの)。
//
// 戻り値:
//   - deploy や service が無い、または他のユーザーのものなら not_found。
//     deploy が反映中・削除の途中、または service が active でないなら conflict。
//   - 保存できなかったときは internal_error。
func (serviceService *ServiceService) UpdatePorts(requestContext context.Context, userID string, deployID string, ports []model.ServicePort) error {
	// ポートを JSON にする。
	portsJSON, marshalError := json.Marshal(ports)
	if marshalError != nil {
		return apperror.Wrap(apperror.KindInternal, "ポートを保存できませんでした", marshalError)
	}
	// 保存を、保存先に任せる。
	return serviceService.serviceStore.SavePendingPorts(requestContext, userID, deployID, datatypes.JSON(portsJSON))
}

// DiscardPorts は、まだ適用していないポート(pending_ports)を、適用せずに破棄する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: service が属する deploy の ID。
//
// 戻り値:
//   - deploy や service が無い、または他のユーザーのものなら not_found。
//     deploy が反映中・削除の途中、service が active でない、または破棄する値が無いなら conflict。
func (serviceService *ServiceService) DiscardPorts(requestContext context.Context, userID string, deployID string) error {
	// 破棄を、保存先に任せる。
	return serviceService.serviceStore.DiscardPendingPorts(requestContext, userID, deployID)
}

// ApplyPorts は、まだ適用していないポート(pending_ports)の、k8s の Service への反映を開始する。
//
// 処理の流れ:
//  1. deploy の状態を applying にする(適用する値が無い・反映中・起動中・削除の途中・service が active でないなら conflict)
//  2. 反映の Workflow を開始する(完了は待たない。結果は deploy の status で確かめる)
//  3. 手順 2 に失敗したら、deploy の状態を元に戻す(反映されない applying を残さない)
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: service が属する deploy の ID。
//
// 戻り値:
//   - deploy や service が無い、または他のユーザーのものなら not_found。上記の場合は conflict。
//   - Workflow を開始できなかったときは internal_error(状態は元に戻してある)。
func (serviceService *ServiceService) ApplyPorts(requestContext context.Context, userID string, deployID string) error {
	// 1. deploy の状態を applying にする。元の状態は、開始に失敗したときの巻き戻しに使う。
	serviceID, previousStatus, beginError := serviceService.serviceStore.BeginPortsApply(requestContext, userID, deployID)
	if beginError != nil {
		return beginError
	}

	// 2. 反映の Workflow を開始する。
	startError := serviceService.serviceWorkflowStarter.StartPortsApply(requestContext, serviceID)
	if startError == nil {
		return nil
	}

	// 3. 開始できなかったので、状態を元に戻す。リクエストが中断されていても戻せるよう、context を切り離す。
	if revertError := serviceService.serviceStore.RevertPortsApply(context.WithoutCancel(requestContext), deployID, previousStatus); revertError != nil {
		// 戻せなかったときは、両方の原因を残す(applying のまま残る)。
		return apperror.Wrap(apperror.KindInternal, "service のポートの反映を開始できませんでした", joinErrors(startError, revertError))
	}
	return apperror.Wrap(apperror.KindInternal, "service のポートの反映を開始できませんでした", startError)
}
