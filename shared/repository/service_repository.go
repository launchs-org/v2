package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// ServiceRepository は、services テーブルの読み書きを担当する。
type ServiceRepository struct {
	database *gorm.DB
}

// NewServiceRepository は、ServiceRepository を作る。
//
// 引数:
//   - database: 使うデータベースの接続。
//
// 戻り値:
//   - 作成した ServiceRepository。
func NewServiceRepository(database *gorm.DB) *ServiceRepository {
	return &ServiceRepository{database: database}
}

// Create は、deploy に service を1つ保存する(status は pending)。
//
// 保存する前に、deploy が、持ち主のものであること、削除の途中でないこと、まだ service が無いことを、
// deploy の行をロックして確かめる。ロックは、確認のあとに、deploy の削除の受け付けが入り込んで、
// 削除中の deploy に service が作られるのを防ぐ。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - service: 保存する service。ID、DeployID、UserID、Ports は、呼び出し側で設定しておく。ProjectID は、deploy から設定する。
//
// 戻り値:
//   - deploy が無い、または他のユーザーのものなら not_found。
//   - deploy が削除の途中(deleting、delete_failed)、または、すでに service があるなら conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (serviceRepository *ServiceRepository) Create(requestContext context.Context, service *model.Service) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(service.ID, service.DeployID, service.UserID) {
		return newEmptyConditionError()
	}
	// deploy の確認と service の保存を、1つのトランザクションで行う。
	return serviceRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 持ち主の deploy を、他から状態を変えられないよう、ロックして読む。
		var deploy model.Deploy
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Deploy{ID: service.DeployID, UserID: service.UserID}).
			Take(&deploy).Error
		// 無い、または他のユーザーのものは、存在しないものとして not_found にする。
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "deploy が見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "deploy を取得できませんでした", findError)
		}
		// 削除の途中の deploy には、作れない。
		if deploy.Status == model.DeployStatusDeleting || deploy.Status == model.DeployStatusDeleteFailed {
			return apperror.New(apperror.KindConflict, "deploy は "+deploy.Status+" のため、service を作れません")
		}
		// 1 deploy につき、service は1つだけ。
		var existingCount int64
		if countError := transaction.Model(&model.Service{}).Where(&model.Service{DeployID: service.DeployID}).Count(&existingCount).Error; countError != nil {
			return apperror.Wrap(apperror.KindInternal, "service を確認できませんでした", countError)
		}
		if existingCount > 0 {
			return apperror.New(apperror.KindConflict, "この deploy には、すでに service があります")
		}

		// 属するプロジェクトは、deploy から決める。pending で保存する。
		service.ProjectID = deploy.ProjectID
		service.Status = model.ServiceStatusPending
		if createError := transaction.Create(service).Error; createError != nil {
			return apperror.Wrap(apperror.KindInternal, "service を保存できませんでした", createError)
		}
		return nil
	})
}

// FindByDeployID は、ユーザーの deploy の service を1件取得し、あわせて deploy の状態も返す。
//
// 他のユーザーの deploy は、存在しないものとして扱う(共通ルール)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - service。
//   - deploy の状態(deploy が削除中のとき、service も削除中として見せるために使う)。
//   - deploy が無い(他人のものを含む)、または service が無いときの、not_found のエラー。
func (serviceRepository *ServiceRepository) FindByDeployID(requestContext context.Context, userID string, deployID string) (model.Service, string, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, deployID) {
		return model.Service{}, "", apperror.New(apperror.KindNotFound, "deploy が見つかりません")
	}
	// 持ち主の deploy を探す。
	var deploy model.Deploy
	deployError := serviceRepository.database.WithContext(requestContext).
		Where(&model.Deploy{ID: deployID, UserID: userID}).Take(&deploy).Error
	if errors.Is(deployError, gorm.ErrRecordNotFound) {
		return model.Service{}, "", apperror.Wrap(apperror.KindNotFound, "deploy が見つかりません", deployError)
	}
	if deployError != nil {
		return model.Service{}, "", apperror.Wrap(apperror.KindInternal, "deploy を取得できませんでした", deployError)
	}
	// deploy の service を探す。
	var service model.Service
	serviceError := serviceRepository.database.WithContext(requestContext).
		Where(&model.Service{DeployID: deployID}).Take(&service).Error
	if errors.Is(serviceError, gorm.ErrRecordNotFound) {
		return model.Service{}, "", apperror.Wrap(apperror.KindNotFound, "service が見つかりません", serviceError)
	}
	if serviceError != nil {
		return model.Service{}, "", apperror.Wrap(apperror.KindInternal, "service を取得できませんでした", serviceError)
	}
	return service, deploy.Status, nil
}

// FindForWorker は、ワーカーが、持ち主を問わずに、service を ID で1件取得する(内部の処理用)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: service の ID。
//
// 戻り値:
//   - 見つかった service。
//   - 無いときの not_found のエラー。
func (serviceRepository *ServiceRepository) FindForWorker(requestContext context.Context, serviceID string) (model.Service, error) {
	// 条件の値が空だと、先頭の1件が返るので、先に拒否する。
	if hasEmpty(serviceID) {
		return model.Service{}, apperror.New(apperror.KindNotFound, "service が見つかりません")
	}
	// ID だけで探す。
	var service model.Service
	findError := serviceRepository.database.WithContext(requestContext).
		Where(&model.Service{ID: serviceID}).Take(&service).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Service{}, apperror.Wrap(apperror.KindNotFound, "service が見つかりません", findError)
	}
	if findError != nil {
		return model.Service{}, apperror.Wrap(apperror.KindInternal, "service を取得できませんでした", findError)
	}
	return service, nil
}

// FindByDeployIDForWorker は、ワーカーが、deploy の service を、持ち主を問わずに取得する(内部の処理用)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - 見つかった service。
//   - service が無いときは false(エラーにはしない)。
//   - データベースのエラー。
func (serviceRepository *ServiceRepository) FindByDeployIDForWorker(requestContext context.Context, deployID string) (model.Service, bool, error) {
	// 条件の値が空だと、先頭の1件が返るので、先に拒否する。
	if hasEmpty(deployID) {
		return model.Service{}, false, newEmptyConditionError()
	}
	// deploy の ID で探す。
	var service model.Service
	findError := serviceRepository.database.WithContext(requestContext).
		Where(&model.Service{DeployID: deployID}).Take(&service).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Service{}, false, nil
	}
	if findError != nil {
		return model.Service{}, false, apperror.Wrap(apperror.KindInternal, "service を取得できませんでした", findError)
	}
	return service, true, nil
}

// MarkActive は、k8s に作れた service を、active にし、払い出された DNS 名と ClusterIP を記録する。
//
// pending のときだけ変える(別の処理が先に変えていたら、何もしない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: service の ID。
//   - dnsName: DNS 名。
//   - clusterIP: ClusterIP(None のとき、または払い出されないときは空)。
//
// 戻り値:
//   - 変えられたら true。pending でなかったとき、または無いときは false。
//   - データベースのエラー。
func (serviceRepository *ServiceRepository) MarkActive(requestContext context.Context, serviceID string, dnsName string, clusterIP string) (bool, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(serviceID, dnsName) {
		return false, newEmptyConditionError()
	}
	// 空の ClusterIP は NULL のままにする(ポインタにして、空なら nil)。
	var clusterIPValue *string
	if clusterIP != "" {
		clusterIPValue = &clusterIP
	}
	// pending のときだけ、active にして、DNS 名と ClusterIP を書く。
	updateResult := serviceRepository.database.WithContext(requestContext).
		Model(&model.Service{}).
		Where(&model.Service{ID: serviceID, Status: model.ServiceStatusPending}).
		Updates(&model.Service{Status: model.ServiceStatusActive, DNSName: &dnsName, ClusterIP: clusterIPValue})
	if updateResult.Error != nil {
		return false, apperror.Wrap(apperror.KindInternal, "service を更新できませんでした", updateResult.Error)
	}
	return updateResult.RowsAffected > 0, nil
}

// UpdateStatus は、service の状態を、期待する状態から別の状態へ変える。
//
// 期待する状態でないとき(別の処理が先に変えたとき)は、何も変えず、false を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: 状態を変える service の ID。
//   - fromStatus: 変更前に期待する状態。
//   - toStatus: 変更後の状態。
//
// 戻り値:
//   - 変えられたら true。期待する状態でなかったとき、または無いときは false。
//   - データベースのエラー。
func (serviceRepository *ServiceRepository) UpdateStatus(requestContext context.Context, serviceID string, fromStatus string, toStatus string) (bool, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(serviceID, fromStatus, toStatus) {
		return false, newEmptyConditionError()
	}
	// 期待する状態のときだけ、状態を更新する(更新日時は Gorm が更新する)。
	updateResult := serviceRepository.database.WithContext(requestContext).
		Model(&model.Service{}).
		Where(&model.Service{ID: serviceID, Status: fromStatus}).
		Updates(&model.Service{Status: toStatus})
	if updateResult.Error != nil {
		return false, apperror.Wrap(apperror.KindInternal, "service の状態を更新できませんでした", updateResult.Error)
	}
	return updateResult.RowsAffected > 0, nil
}

// DeleteUnstarted は、作成の Workflow を開始できなかった service(pending)を、物理削除する。
//
// 作成を取り消すために使う。pending のものだけを消す。すでに無いときは、何もしない(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: 取り消す service の ID。
//
// 戻り値:
//   - データベースのエラー。
func (serviceRepository *ServiceRepository) DeleteUnstarted(requestContext context.Context, serviceID string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(serviceID) {
		return newEmptyConditionError()
	}
	// pending のものだけを物理削除する。
	deleteError := serviceRepository.database.WithContext(requestContext).
		Where(&model.Service{ID: serviceID, Status: model.ServiceStatusPending}).
		Delete(&model.Service{}).Error
	if deleteError != nil {
		return apperror.Wrap(apperror.KindInternal, "service の作成を取り消せませんでした", deleteError)
	}
	return nil
}

// MarkDeleting は、service の状態を、削除中(deleting)にする。
//
// 通常の状態(active)と、作成または削除に失敗した状態(failed)が対象。作成中(pending)と、すでに削除中は conflict。
// deploy が削除の途中のときも conflict(deploy の削除が、service も片付ける)。実体の削除は、ここでは行わない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: service が属する deploy の ID。
//
// 戻り値:
//   - 削除する service の ID。
//   - 更新する前の状態(Workflow を開始できなかったときに、戻すために使う)。
//   - deploy や service が無い、または他のユーザーのものなら not_found。受け付けられない状態なら conflict。
func (serviceRepository *ServiceRepository) MarkDeleting(requestContext context.Context, userID string, deployID string) (string, string, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, deployID) {
		return "", "", apperror.New(apperror.KindNotFound, "deploy が見つかりません")
	}
	// 結果を、トランザクションの外へ持ち出す入れ物。
	serviceID, previousStatus := "", ""
	// 確認と更新を、1つのトランザクションで行う。
	transactionError := serviceRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 持ち主の deploy を、ロックして読む。
		var deploy model.Deploy
		deployError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Deploy{ID: deployID, UserID: userID}).Take(&deploy).Error
		if errors.Is(deployError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "deploy が見つかりません", deployError)
		}
		if deployError != nil {
			return apperror.Wrap(apperror.KindInternal, "deploy を取得できませんでした", deployError)
		}
		// deploy が削除の途中なら、deploy の削除が、service も片付けるので、受け付けない。
		if deploy.Status == model.DeployStatusDeleting || deploy.Status == model.DeployStatusDeleteFailed {
			return apperror.New(apperror.KindConflict, "deploy は "+deploy.Status+" のため、service を削除できません")
		}
		// service を、ロックして読む。
		var service model.Service
		serviceError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Service{DeployID: deployID}).Take(&service).Error
		if errors.Is(serviceError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "service が見つかりません", serviceError)
		}
		if serviceError != nil {
			return apperror.Wrap(apperror.KindInternal, "service を取得できませんでした", serviceError)
		}
		// 作成中と削除中は、受け付けない。
		if service.Status == model.ServiceStatusPending || service.Status == model.ServiceStatusDeleting {
			return apperror.New(apperror.KindConflict, "service は "+service.Status+" のため、削除できません")
		}
		// 状態を deleting にする(更新日時は Gorm が更新する)。
		updateError := transaction.Model(&model.Service{}).Where(&model.Service{ID: service.ID}).
			Updates(&model.Service{Status: model.ServiceStatusDeleting}).Error
		if updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "service を削除中にできませんでした", updateError)
		}
		serviceID, previousStatus = service.ID, service.Status
		return nil
	})
	if transactionError != nil {
		return "", "", transactionError
	}
	return serviceID, previousStatus, nil
}

// RevertDeleting は、削除中(deleting)にした service を、削除を受け付ける前の状態に戻す。
//
// 削除の Workflow を開始できなかったときに使う。削除中でないとき、または無いときは、何もしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: 状態を戻す service の ID。
//   - previousStatus: 戻す先の状態(MarkDeleting が返したもの)。
//
// 戻り値:
//   - データベースのエラー。
func (serviceRepository *ServiceRepository) RevertDeleting(requestContext context.Context, serviceID string, previousStatus string) error {
	// 削除中のときだけ、前の状態に戻す(UpdateStatus は、条件の空値も拒否する)。
	_, revertError := serviceRepository.UpdateStatus(requestContext, serviceID, model.ServiceStatusDeleting, previousStatus)
	return revertError
}

// MarkDeleteFailed は、削除中(deleting)の service を、失敗(failed)にする。
//
// 削除の Workflow が、リトライの期限までに終えられなかったときの補償。failed の service は、もう一度の削除を受け付ける。
// 削除中でないとき、または無いときは、何もしない(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: 削除に失敗した service の ID。
//
// 戻り値:
//   - データベースのエラー。
func (serviceRepository *ServiceRepository) MarkDeleteFailed(requestContext context.Context, serviceID string) error {
	// 削除中のときだけ、failed にする。
	_, markError := serviceRepository.UpdateStatus(requestContext, serviceID, model.ServiceStatusDeleting, model.ServiceStatusFailed)
	return markError
}

// DeleteRecord は、service を物理削除する。削除の Workflow が、最後に使う。
//
// 削除中(deleting)の service だけを消す(誤って消さないための安全装置)。繰り返し実行されても壊れない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: 削除する service の ID。
//
// 戻り値:
//   - データベースのエラー。
func (serviceRepository *ServiceRepository) DeleteRecord(requestContext context.Context, serviceID string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(serviceID) {
		return newEmptyConditionError()
	}
	// 削除中の service だけを物理削除する。
	deleteError := serviceRepository.database.WithContext(requestContext).
		Where(&model.Service{ID: serviceID, Status: model.ServiceStatusDeleting}).
		Delete(&model.Service{}).Error
	if deleteError != nil {
		return apperror.Wrap(apperror.KindInternal, "service を削除できませんでした", deleteError)
	}
	return nil
}
