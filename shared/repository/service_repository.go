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
		if deploy.IsBeingDeleted() {
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

// CompleteCreation は、service の作成の結果を確定する。pending の service だけが対象。
//
//   - 成功: active にして、DNS 名と ClusterIP を記録する。
//   - 失敗: failed にする。
//
// 別の処理が先に決めていたとき(pending でないとき)や、service が無いときは、何も変えず false を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: 結果を確定する service の ID。
//   - succeeded: 作成に成功したか。
//   - dnsName: 払い出された DNS 名(成功のときだけ使う。成功のときは空にできない)。
//   - clusterIP: 払い出された ClusterIP(成功のときだけ使う。空なら記録しない)。
//
// 戻り値:
//   - 確定したら true。
//   - データベースのエラー。
func (serviceRepository *ServiceRepository) CompleteCreation(requestContext context.Context, serviceID string, succeeded bool, dnsName string, clusterIP string) (bool, error) {
	// 成功のときに DNS 名が空だと、壊れた値を書いてしまうので、先に拒否する。
	if succeeded && hasEmpty(dnsName) {
		return false, newEmptyConditionError()
	}
	// モデルに、結果の確定を任せる。
	return transitionService(serviceRepository.database, requestContext, serviceID, func(service *model.Service) bool {
		return service.CompleteCreation(succeeded, dnsName, clusterIP)
	})
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
		if deploy.IsBeingDeleted() {
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
		// モデルに、削除の受け付けを任せる(作成中と削除中は、受け付けない)。
		acceptedStatus, beginError := service.BeginDelete()
		if beginError != nil {
			return beginError
		}
		// 変えた service を保存する。
		if saveError := transaction.Save(&service).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "service を削除中にできませんでした", saveError)
		}
		serviceID, previousStatus = service.ID, acceptedStatus
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
	// 条件の値が空のときは拒否し、削除中のときだけ、モデルに前の状態へ戻させる。
	if hasEmpty(previousStatus) {
		return newEmptyConditionError()
	}
	_, revertError := transitionService(serviceRepository.database, requestContext, serviceID, func(service *model.Service) bool {
		return service.RevertDeleting(previousStatus)
	})
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
	// 削除中のときだけ、モデルに failed へ変えさせる。
	_, markError := transitionService(serviceRepository.database, requestContext, serviceID, func(service *model.Service) bool {
		return service.MarkDeleteFailed()
	})
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
