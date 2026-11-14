package repository

import (
	"context"
	"errors"

	"launchs/shared/apperror"
	"launchs/shared/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// transitionRow は、1行(deploy、service、project など)をロックして読み、モデルの操作を呼び、変わったときだけ保存する。
//
// 行が無いとき、または操作が「変えるものが無い」(false)を返したときは、何も保存せず false を返す。
// これで、別の処理が先に状態を変えていても、古い結果で上書きしない。状態の規則は、モデルの操作が持つ。
//
// 型引数:
//   - Row: 行のモデル(model.Deploy、model.Service、model.Project)。主キーは id 列。
//
// 引数:
//   - database: 使うデータベース。
//   - requestContext: キャンセルや期限を伝える context。
//   - rowID: 操作する行の ID。
//   - operation: モデルの操作。変えたら true を返す。
//
// 戻り値:
//   - 変えて保存したら true。
//   - データベースのエラー。
func transitionRow[Row any](database *gorm.DB, requestContext context.Context, rowID string, operation func(row *Row) bool) (bool, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(rowID) {
		return false, newEmptyConditionError()
	}
	// 変えたかを、トランザクションの外へ持ち出す入れ物。
	changed := false
	// 取得と更新を、1つのトランザクションで行う。
	transactionError := database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む。無いときは、変えるものが無い。
		var row Row
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", rowID).Take(&row).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return nil
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "行を取得できませんでした", findError)
		}
		// モデルに操作を任せる。変わらなければ、保存しない。
		if !operation(&row) {
			return nil
		}
		// 変えた行を保存する。
		if saveError := transaction.Save(&row).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "状態を更新できませんでした", saveError)
		}
		changed = true
		return nil
	})
	return changed, transactionError
}

// transitionDeploy は、deploy 1行に対して transitionRow を行う。
//
// 引数:
//   - database: 使うデータベース。
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 操作する deploy の ID。
//   - operation: モデルの操作。変えたら true を返す。
//
// 戻り値:
//   - 変えて保存したら true。
//   - データベースのエラー。
func transitionDeploy(database *gorm.DB, requestContext context.Context, deployID string, operation func(deploy *model.Deploy) bool) (bool, error) {
	// 型を決めて、共通の処理に任せる。
	return transitionRow[model.Deploy](database, requestContext, deployID, operation)
}

// transitionService は、service 1行に対して transitionRow を行う。
//
// 引数:
//   - database: 使うデータベース。
//   - requestContext: キャンセルや期限を伝える context。
//   - serviceID: 操作する service の ID。
//   - operation: モデルの操作。変えたら true を返す。
//
// 戻り値:
//   - 変えて保存したら true。
//   - データベースのエラー。
func transitionService(database *gorm.DB, requestContext context.Context, serviceID string, operation func(service *model.Service) bool) (bool, error) {
	// 型を決めて、共通の処理に任せる。
	return transitionRow[model.Service](database, requestContext, serviceID, operation)
}

// transitionProject は、project 1行に対して transitionRow を行う。
//
// 引数:
//   - database: 使うデータベース。
//   - requestContext: キャンセルや期限を伝える context。
//   - projectID: 操作する project の ID。
//   - operation: モデルの操作。変えたら true を返す。
//
// 戻り値:
//   - 変えて保存したら true。
//   - データベースのエラー。
func transitionProject(database *gorm.DB, requestContext context.Context, projectID string, operation func(project *model.Project) bool) (bool, error) {
	// 型を決めて、共通の処理に任せる。
	return transitionRow[model.Project](database, requestContext, projectID, operation)
}
