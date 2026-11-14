package repository

import (
	"context"
	"errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// lockDeployAndActiveService は、ポートの更新・適用・破棄の前に、deploy と service を、更新用にロックして取得し、
// 操作できる状態かを確かめる(トランザクションの中で使う)。
//
// 確かめること:
//   - deploy が持ち主のもので、反映中(applying)でなく、削除の途中(deleting、delete_failed)でない
//   - service があり、active である(作成中・失敗・削除中の service は、ポートを変えられない)
//
// 引数:
//   - transaction: 使っているトランザクション。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - ロックした deploy と service。
//   - deploy や service が無い(他人のものを含む)なら not_found。操作できない状態なら conflict。
func lockDeployAndActiveService(transaction *gorm.DB, userID string, deployID string) (model.Deploy, model.Service, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, deployID) {
		return model.Deploy{}, model.Service{}, apperror.New(apperror.KindNotFound, "deploy が見つかりません")
	}
	// deploy を、持ち主の条件つきで、ロックして読む。
	var deploy model.Deploy
	deployError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&model.Deploy{ID: deployID, UserID: userID}).Take(&deploy).Error
	if errors.Is(deployError, gorm.ErrRecordNotFound) {
		return model.Deploy{}, model.Service{}, apperror.Wrap(apperror.KindNotFound, "deploy が見つかりません", deployError)
	}
	if deployError != nil {
		return model.Deploy{}, model.Service{}, apperror.Wrap(apperror.KindInternal, "deploy を取得できませんでした", deployError)
	}
	// 反映中や削除の途中は、操作できない(排他制御)。
	if !deploy.CanEditSettings() {
		return model.Deploy{}, model.Service{}, apperror.New(apperror.KindConflict, "deploy は "+deploy.Status+" のため、service のポートを操作できません")
	}
	// service を、ロックして読む。
	var service model.Service
	serviceError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&model.Service{DeployID: deployID}).Take(&service).Error
	if errors.Is(serviceError, gorm.ErrRecordNotFound) {
		return model.Deploy{}, model.Service{}, apperror.Wrap(apperror.KindNotFound, "service が見つかりません", serviceError)
	}
	if serviceError != nil {
		return model.Deploy{}, model.Service{}, apperror.Wrap(apperror.KindInternal, "service を取得できませんでした", serviceError)
	}
	// active でない service は、ポートを変えられない。
	if !service.CanChangePorts() {
		return model.Deploy{}, model.Service{}, apperror.New(apperror.KindConflict, "service は "+service.Status+" のため、ポートを操作できません")
	}
	return deploy, service, nil
}

// SavePendingPorts は、ポートを、まだ適用しない値として保存する。
//
// 現在のポートと同じ内容(JSON の書式ではなく、中身で比べる)なら、変更が無いものとして、pending を保存せず空にする
// (以前の pending も消える)。現在反映されているポートは変えない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: service が属する deploy の ID。
//   - ports: 新しいポート(ServicePort の JSON の配列。検査済みのもの)。
//
// 戻り値:
//   - deploy や service が無い、または他のユーザーのものなら not_found。
//     deploy が反映中・削除の途中、または service が active でないなら conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (serviceRepository *ServiceRepository) SavePendingPorts(requestContext context.Context, userID string, deployID string, ports datatypes.JSON) error {
	// 確認と更新を、1つのトランザクションで行う。
	return serviceRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 操作できる状態であることを、行をロックして確かめる。
		_, service, lockError := lockDeployAndActiveService(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// 現在のポートと同じなら、変更が無いので、pending は保存せず空にする。
		pendingToSave := model.Service{PendingPorts: ports}
		if sameJSON(service.Ports, ports) {
			pendingToSave = model.Service{}
		}
		// pending_ports の列を、nil のときも含めて更新する(更新日時は Gorm が更新する)。
		updateError := transaction.Model(&model.Service{}).
			Where(&model.Service{ID: service.ID}).
			Select("pending_ports").
			Updates(&pendingToSave).Error
		if updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "ポートを保存できませんでした", updateError)
		}
		return nil
	})
}

// DiscardPendingPorts は、まだ適用していないポートを、適用せずに破棄する。現在反映されているポートは変えない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: service が属する deploy の ID。
//
// 戻り値:
//   - deploy や service が無い、または他のユーザーのものなら not_found。
//     deploy が反映中・削除の途中、service が active でない、または破棄する値が無いなら conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (serviceRepository *ServiceRepository) DiscardPendingPorts(requestContext context.Context, userID string, deployID string) error {
	// 確認と更新を、1つのトランザクションで行う。
	return serviceRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 操作できる状態であることを、行をロックして確かめる。
		_, service, lockError := lockDeployAndActiveService(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// 破棄する値が無いときは、conflict にする。
		if len(service.PendingPorts) == 0 {
			return apperror.New(apperror.KindConflict, "破棄するポートの変更がありません")
		}
		// pending_ports を NULL に戻す(更新日時は Gorm が更新する)。
		updateError := transaction.Model(&model.Service{}).
			Where(&model.Service{ID: service.ID}).
			Select("pending_ports").
			Updates(&model.Service{}).Error
		if updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "ポートを破棄できませんでした", updateError)
		}
		return nil
	})
}

// BeginPortsApply は、pending のポートの反映を始めるため、deploy の状態を applying にする。
//
// 反映できるのは、active の service に、適用していないポートがあり、deploy が通常の状態(active、scaled_zero、failed)のとき。
// 起動中(pending)の deploy は、作成の処理が状態を決めるので、conflict にする。行をロックして確かめるので、同時に2つの反映は始まらない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: service が属する deploy の ID。
//
// 戻り値:
//   - service の ID。
//   - 変える前の deploy の状態(開始に失敗したとき、元に戻すために使う)。
//   - deploy や service が無い、または他のユーザーのものなら not_found。
//     反映中・起動中・削除の途中、service が active でない、または適用する値が無いなら conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (serviceRepository *ServiceRepository) BeginPortsApply(requestContext context.Context, userID string, deployID string) (string, string, error) {
	// 結果を、トランザクションの外へ持ち出す入れ物。
	serviceID, previousStatus := "", ""
	// 確認と更新を、1つのトランザクションで行う。
	transactionError := serviceRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 操作できる状態であることを、行をロックして確かめる。
		deploy, service, lockError := lockDeployAndActiveService(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// service 側の条件(適用する値があること)を、モデルに確かめさせる。
		if beginError := service.BeginPortsApply(); beginError != nil {
			return beginError
		}
		// deploy を反映中にすることを、モデルに任せる。
		acceptedStatus, beginError := deploy.BeginServicePortsApply()
		if beginError != nil {
			return beginError
		}
		// 変えた deploy を保存する。
		if saveError := transaction.Save(&deploy).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "deploy の状態を更新できませんでした", saveError)
		}
		serviceID, previousStatus = service.ID, acceptedStatus
		return nil
	})
	if transactionError != nil {
		return "", "", transactionError
	}
	return serviceID, previousStatus, nil
}

// RevertPortsApply は、ポートの反映を始められなかったとき、deploy の状態を、元に戻す。
//
// applying のときだけ戻す(別の処理が先に変えていたら、何もしない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 状態を戻す deploy の ID。
//   - previousStatus: 戻す先の状態(BeginPortsApply が返したもの)。
//
// 戻り値:
//   - データベースのエラー。
func (serviceRepository *ServiceRepository) RevertPortsApply(requestContext context.Context, deployID string, previousStatus string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(deployID, previousStatus) {
		return newEmptyConditionError()
	}
	// applying のときだけ、前の状態に戻す(更新日時は Gorm が更新する)。
	// applying のときだけ、モデルに前の状態へ戻させる。
	_, updateError := transitionDeploy(serviceRepository.database, requestContext, deployID, func(deploy *model.Deploy) bool {
		return deploy.RevertApply(previousStatus)
	})
	if updateError != nil {
		return updateError
	}
	return nil
}

// CommitPortsApply は、ポートの反映の結果を確定する。
//
// deploy が applying のときだけ対象にする(別の処理が先に変えていたら、何もしない)。
//   - 成功: ポートを、反映した pending の内容にし、pending を空に戻し、deploy を active(replicas が 0 なら scaled_zero)に戻す。
//   - 失敗: ポートと pending は変えない(k8s は、元のポートに戻してある前提)。deploy を failed にする。
//
// ポートは常に「いま動いている値」を表し、deploy の status が、最後の反映の成否を表す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: 結果を確定する service の ID。
//   - succeeded: 反映に成功したか。
//
// 戻り値:
//   - 確定したら true。deploy が applying でなかったとき、または service が無いときは、何も変えず false。
//   - データベースのエラー。
func (serviceRepository *ServiceRepository) CommitPortsApply(requestContext context.Context, serviceID string, succeeded bool) (bool, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(serviceID) {
		return false, newEmptyConditionError()
	}
	// 確定したかを、トランザクションの外へ持ち出す入れ物。
	committed := false
	// 確認と更新を、1つのトランザクションで行う。
	transactionError := serviceRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// service を、ロックして読む。無いとき(削除された)は、確定するものが無いので、何もしない。
		var service model.Service
		serviceError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Service{ID: serviceID}).Take(&service).Error
		if errors.Is(serviceError, gorm.ErrRecordNotFound) {
			return nil
		}
		if serviceError != nil {
			return serviceError
		}
		// deploy を、ロックして読む。applying でなければ、別の処理が先に確定している(または削除が始まっている)ので、何もしない。
		var deploy model.Deploy
		deployError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Deploy{ID: service.DeployID}).Take(&deploy).Error
		if errors.Is(deployError, gorm.ErrRecordNotFound) {
			return nil
		}
		if deployError != nil {
			return deployError
		}
		// モデルに、結果の確定を任せる(applying でなければ、何も変わらない)。
		if !deploy.CommitServicePorts(succeeded) {
			return nil
		}
		// 成功のときだけ、pending のポートを、現在のポートにする。
		if succeeded {
			service.PromotePendingPorts()
			if saveError := transaction.Save(&service).Error; saveError != nil {
				return saveError
			}
		}
		// 変えた deploy を保存する。
		if saveError := transaction.Save(&deploy).Error; saveError != nil {
			return saveError
		}
		committed = true
		return nil
	})
	if transactionError != nil {
		return false, apperror.Wrap(apperror.KindInternal, "ポートの反映の結果を確定できませんでした", transactionError)
	}
	return committed, nil
}
