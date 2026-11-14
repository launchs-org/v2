package repository

import (
	"context"
	"errors"
	"launchs/shared/apperror"
	"launchs/shared/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DeployRepository は、deploys テーブルと deploy_specs テーブルの読み書きを担当する。
type DeployRepository struct {
	database *gorm.DB
}

// NewDeployRepository は、DeployRepository を作る。
//
// 引数:
//   - database: 使うデータベースの接続。
//
// 戻り値:
//   - 作成した DeployRepository。
func NewDeployRepository(database *gorm.DB) *DeployRepository {
	return &DeployRepository{database: database}
}

// FindSpec は、スペックの名前から、CPU とメモリの大きさを取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - specName: スペックの名前(small、medium、large)。
//
// 戻り値:
//   - スペック。
//   - 無いときの invalid_request のエラー。その他のデータベースのエラーは internal_error。
func (deployRepository *DeployRepository) FindSpec(requestContext context.Context, specName string) (model.DeploySpec, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(specName) {
		return model.DeploySpec{}, apperror.New(apperror.KindInvalidRequest, "指定できないスペックです")
	}
	// 名前でスペックを1件探す。
	var spec model.DeploySpec
	findError := deployRepository.database.WithContext(requestContext).
		Where(&model.DeploySpec{Name: specName}).
		Take(&spec).Error
	// 無い名前は、リクエストの値が不正なので invalid_request にする。
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.DeploySpec{}, apperror.Wrap(apperror.KindInvalidRequest, "指定できないスペックです", findError)
	}
	// それ以外のデータベースのエラーは、内部エラーにする。
	if findError != nil {
		return model.DeploySpec{}, apperror.Wrap(apperror.KindInternal, "スペックを取得できませんでした", findError)
	}
	return spec, nil
}

// Create は、プロジェクトの配下に deploy を1件保存する。
//
// 保存する前に、プロジェクトが、持ち主のもので、通常(active)の状態であることを、
// 行をロックして確かめる。削除中のプロジェクトの配下には、作れない。
// ロックは、確認のあとに、プロジェクトの削除の受け付けが入り込んで、削除中のプロジェクトに
// deploy が作られるのを防ぐ。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deploy: 保存する deploy。ID は、呼び出し側で作っておく。
//
// 戻り値:
//   - プロジェクトが無い、または他のユーザーのものなら not_found。
//   - プロジェクトが active でないなら conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (deployRepository *DeployRepository) Create(requestContext context.Context, deploy *model.Deploy) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(deploy.ProjectID, deploy.UserID, deploy.ID) {
		return newEmptyConditionError()
	}
	// プロジェクトの確認と deploy の保存を、1つのトランザクションで行う。
	transactionError := deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 持ち主のプロジェクトを、他から状態を変えられないよう、共有ロックで読む。
		var project model.Project
		findError := transaction.Clauses(clause.Locking{Strength: "SHARE"}).
			Where(&model.Project{ID: deploy.ProjectID, UserID: deploy.UserID}).
			Take(&project).Error
		// 無い、または他のユーザーのものは、存在しないものとして not_found にする。
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "プロジェクトが見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", findError)
		}
		// 削除中などの、通常でないプロジェクトの配下には作れない。
		if project.Status != model.ProjectStatusActive {
			return apperror.New(apperror.KindConflict, "プロジェクトは "+project.Status+" のため、deploy を作れません")
		}

		// deploy を保存する。
		if createError := transaction.Create(deploy).Error; createError != nil {
			return apperror.Wrap(apperror.KindInternal, "deploy を保存できませんでした", createError)
		}
		return nil
	})
	return transactionError
}

// FindByID は、ユーザーの deploy を1件取得する。
//
// 他のユーザーの deploy は、存在しないものとして扱う(共通ルール)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 取得する deploy の ID。
//
// 戻り値:
//   - 見つかった deploy。
//   - 無い、または他のユーザーのものだったときの、not_found のエラー。
func (deployRepository *DeployRepository) FindByID(requestContext context.Context, userID string, deployID string) (model.Deploy, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, deployID) {
		return model.Deploy{}, apperror.New(apperror.KindNotFound, "deploy が見つかりません")
	}
	// ID と持ち主の両方で探す。
	var deploy model.Deploy
	findError := deployRepository.database.WithContext(requestContext).
		Where(&model.Deploy{ID: deployID, UserID: userID}).
		Take(&deploy).Error
	// 見つからなければ not_found にする。
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Deploy{}, apperror.Wrap(apperror.KindNotFound, "deploy が見つかりません", findError)
	}
	// それ以外のデータベースのエラーは、内部エラーにする。
	if findError != nil {
		return model.Deploy{}, apperror.Wrap(apperror.KindInternal, "deploy を取得できませんでした", findError)
	}
	return deploy, nil
}

// FindForWorker は、ワーカーが、持ち主を問わずに deploy を1件取得する(内部の処理用)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 取得する deploy の ID。
//
// 戻り値:
//   - 見つかった deploy。
//   - 無いときの not_found のエラー。
func (deployRepository *DeployRepository) FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(deployID) {
		return model.Deploy{}, apperror.New(apperror.KindNotFound, "deploy が見つかりません")
	}
	// ID だけで探す。
	var deploy model.Deploy
	findError := deployRepository.database.WithContext(requestContext).
		Where(&model.Deploy{ID: deployID}).
		Take(&deploy).Error
	// 見つからなければ not_found にする。
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Deploy{}, apperror.Wrap(apperror.KindNotFound, "deploy が見つかりません", findError)
	}
	// それ以外のデータベースのエラーは、内部エラーにする。
	if findError != nil {
		return model.Deploy{}, apperror.Wrap(apperror.KindInternal, "deploy を取得できませんでした", findError)
	}
	return deploy, nil
}

// DeleteUnstarted は、まだ起動を始めていない deploy を、物理削除する。
//
// deploy を保存したあと、起動の Workflow を開始できなかったときに、作成を取り消すために使う。
// pending の deploy だけを消す。すでに無いときは、何もしない(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 取り消す deploy の ID。
//
// 戻り値:
//   - データベースのエラー。
func (deployRepository *DeployRepository) DeleteUnstarted(requestContext context.Context, deployID string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(deployID) {
		return newEmptyConditionError()
	}
	// pending の deploy だけを物理削除する。
	deleteError := deployRepository.database.WithContext(requestContext).
		Where(&model.Deploy{ID: deployID, Status: model.DeployStatusPending}).
		Delete(&model.Deploy{}).Error
	// データベースのエラーは、内部エラーにして返す。
	if deleteError != nil {
		return apperror.Wrap(apperror.KindInternal, "deploy の作成を取り消せませんでした", deleteError)
	}
	return nil
}

// UpdateStatus は、deploy の状態を、期待する状態から別の状態へ変える。
//
// 期待する状態でないとき(別の処理が先に変えたとき)は、何も変えず、false を返す。
// これで、同時に別の処理が状態を変えても、古い結果で上書きしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 状態を変える deploy の ID。
//   - fromStatus: 変更前に期待する状態。
//   - toStatus: 変更後の状態。
//
// 戻り値:
//   - 変えられたら true。期待する状態でなかったとき、または deploy が無いときは false。
//   - データベースのエラー。
func (deployRepository *DeployRepository) UpdateStatus(requestContext context.Context, deployID string, fromStatus string, toStatus string) (bool, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(deployID, fromStatus, toStatus) {
		return false, newEmptyConditionError()
	}
	// 期待する状態のときだけ、状態を更新する(更新日時は Gorm が更新する)。
	updateResult := deployRepository.database.WithContext(requestContext).
		Model(&model.Deploy{}).
		Where(&model.Deploy{ID: deployID, Status: fromStatus}).
		Updates(&model.Deploy{Status: toStatus})
	// データベースのエラーは、内部エラーにして返す。
	if updateResult.Error != nil {
		return false, apperror.Wrap(apperror.KindInternal, "deploy の状態を更新できませんでした", updateResult.Error)
	}
	// 1行以上更新できたときだけ、変えられたことにする。
	return updateResult.RowsAffected > 0, nil
}

// lockDeploy は、ユーザーの deploy を、更新用にロックして取得する(トランザクションの中で使う)。状態は確かめない。
//
// 他のユーザーの deploy は、存在しないものとして扱う。
//
// 引数:
//   - transaction: 使っているトランザクション。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 取得する deploy の ID。
//
// 戻り値:
//   - ロックした deploy。
//   - 無い、または他のユーザーのものなら not_found。
func lockDeploy(transaction *gorm.DB, userID string, deployID string) (model.Deploy, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, deployID) {
		return model.Deploy{}, apperror.New(apperror.KindNotFound, "deploy が見つかりません")
	}
	// ID と持ち主の両方で探し、同時に別の処理が状態を変えないよう、行をロックする。
	var deploy model.Deploy
	findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&model.Deploy{ID: deployID, UserID: userID}).
		Take(&deploy).Error
	// 見つからなければ not_found にする。
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Deploy{}, apperror.Wrap(apperror.KindNotFound, "deploy が見つかりません", findError)
	}
	// それ以外のデータベースのエラーは、内部エラーにする。
	if findError != nil {
		return model.Deploy{}, apperror.Wrap(apperror.KindInternal, "deploy を取得できませんでした", findError)
	}
	return deploy, nil
}

// lockEditableDeploy は、起動設定を編集できる deploy を、更新用にロックして取得する(トランザクションの中で使う)。
//
// 他のユーザーの deploy は、存在しないものとして扱う。反映中(applying)と削除の途中(deleting、delete_failed)の
// deploy は、起動設定を変えられないので、conflict にする。
//
// 引数:
//   - transaction: 使っているトランザクション。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 取得する deploy の ID。
//
// 戻り値:
//   - ロックした deploy。
//   - 無い、または他のユーザーのものなら not_found。編集できない状態なら conflict。
func lockEditableDeploy(transaction *gorm.DB, userID string, deployID string) (model.Deploy, error) {
	// 行をロックして取得する。
	deploy, lockError := lockDeploy(transaction, userID, deployID)
	if lockError != nil {
		return model.Deploy{}, lockError
	}
	// 反映中や削除の途中は、起動設定を変えられない。
	if !deploy.CanEditSettings() {
		return model.Deploy{}, apperror.New(apperror.KindConflict, "deploy は "+deploy.Status+" のため、起動設定を操作できません")
	}
	return deploy, nil
}
