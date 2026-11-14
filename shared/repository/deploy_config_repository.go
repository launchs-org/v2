package repository

import (
	"context"
	"encoding/json"
	"errors"
	"launchs/shared/apperror"
	"launchs/shared/model"
	"reflect"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SavePendingConfig は、起動設定(環境変数、起動コマンド、起動引数)を、まだ適用しない値として保存する。
//
// 3つは、常にまとめて置き換える。すでに適用していない値があるときは、新しい値で上書きする。
// 現在反映されている値(env、command、args)は変えない。
// 現在の値と同じ内容を送ったときは、変更が無いものとして、pending を空にする。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 設定を保存する deploy の ID。
//   - env: 環境変数(JSON のオブジェクト)。
//   - command: 起動コマンド。nil ならイメージの既定。
//   - args: 起動引数(JSON の配列)。nil ならイメージの既定。
//
// 戻り値:
//   - deploy が無い、または他のユーザーのものなら not_found。反映中や削除の途中なら conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (deployRepository *DeployRepository) SavePendingConfig(requestContext context.Context, userID string, deployID string, env datatypes.JSON, command *string, args datatypes.JSON) error {
	// 取得と更新を、1つのトランザクションで行う。
	return deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 編集できる状態であることを、行をロックして確かめる。
		deploy, lockError := lockEditableDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// 現在の値と同じ内容なら、変更が無いので、pending は保存せず空(NULL)にする(以前の pending も消える)。
		pendingToSave := model.Deploy{PendingEnv: env, PendingCommand: command, PendingArgs: args}
		if sameConfig(deploy, env, command, args) {
			pendingToSave = model.Deploy{}
		}
		// 3つの列を、nil のものも含めて、まとめて更新する(更新日時は Gorm が更新する)。
		updateError := transaction.Model(&model.Deploy{}).
			Where(&model.Deploy{ID: deployID}).
			Select("pending_env", "pending_command", "pending_args").
			Updates(&pendingToSave).Error
		// データベースのエラーは、内部エラーにして返す。
		if updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "起動設定を保存できませんでした", updateError)
		}
		return nil
	})
}

// DiscardPendingConfig は、まだ適用していない起動設定を、適用せずに破棄する。
//
// 現在反映されている値(env、command、args)は変えない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 破棄する deploy の ID。
//
// 戻り値:
//   - deploy が無い、または他のユーザーのものなら not_found。
//     反映中や削除の途中、または破棄する値が無いなら conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (deployRepository *DeployRepository) DiscardPendingConfig(requestContext context.Context, userID string, deployID string) error {
	// 取得と更新を、1つのトランザクションで行う。
	return deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 編集できる状態であることを、行をロックして確かめる。
		deploy, lockError := lockEditableDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// 3つとも無いときは、破棄するものが無い。保存のたびに env は必ず入るので、env で判定できる。
		if len(deploy.PendingEnv) == 0 && deploy.PendingCommand == nil && len(deploy.PendingArgs) == 0 {
			return apperror.New(apperror.KindConflict, "破棄する起動設定の変更がありません")
		}
		// 3つの列を NULL に戻す(更新日時は Gorm が更新する)。
		updateError := transaction.Model(&model.Deploy{}).
			Where(&model.Deploy{ID: deployID}).
			Select("pending_env", "pending_command", "pending_args").
			Updates(&model.Deploy{}).Error
		// データベースのエラーは、内部エラーにして返す。
		if updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "起動設定を破棄できませんでした", updateError)
		}
		return nil
	})
}

// BeginConfigApply は、起動設定の反映を始めるため、deploy の状態を applying にする。
//
// 反映できるのは、適用していない起動設定があり、通常の状態(active、scaled_zero、failed)の deploy だけ。
// 行をロックして確かめるので、同時に2つの反映が始まることはない(多重 apply は、後の方が conflict になる)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 反映する deploy の ID。
//
// 戻り値:
//   - 変える前の状態(開始に失敗したとき、元に戻すために使う)。
//   - deploy が無い、または他のユーザーのものなら not_found。
//     反映中・削除の途中・起動中(pending)、または適用する値が無いなら conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (deployRepository *DeployRepository) BeginConfigApply(requestContext context.Context, userID string, deployID string) (string, error) {
	// 前の状態を、トランザクションの外へ持ち出す入れ物。
	previousStatus := ""
	// 取得と更新を、1つのトランザクションで行う。
	transactionError := deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 編集できる状態(applying、deleting、delete_failed 以外)であることを、行をロックして確かめる。
		deploy, lockError := lockEditableDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// 作成の起動中(pending)は、作成の処理が状態を決めるので、反映は始められない。
		if deploy.IsStarting() {
			return apperror.New(apperror.KindConflict, "deploy は起動中のため、起動設定を適用できません")
		}
		// 適用する値が無いときは、反映する意味が無い。
		if len(deploy.PendingEnv) == 0 && deploy.PendingCommand == nil && len(deploy.PendingArgs) == 0 {
			return apperror.New(apperror.KindConflict, "適用する起動設定の変更がありません")
		}
		// 状態を applying にする(更新日時は Gorm が更新する)。
		updateError := transaction.Model(&model.Deploy{}).
			Where(&model.Deploy{ID: deployID}).
			Updates(&model.Deploy{Status: model.DeployStatusApplying}).Error
		// データベースのエラーは、内部エラーにして返す。
		if updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "deploy の状態を更新できませんでした", updateError)
		}
		// 元に戻せるよう、変える前の状態を覚える。
		previousStatus = deploy.Status
		return nil
	})
	return previousStatus, transactionError
}

// CommitConfigResult は、起動設定の反映の結果を確定する。
//
// applying の deploy だけを対象にする。
//   - 成功: 正式な値(env、command、args)を、反映した pending の内容にし、pending を空に戻し、
//     status を active(replicas が 0 なら scaled_zero)にする。
//   - 失敗: 正式な値は変えない(k8s は、古い値に巻き戻してある前提)。pending は、試した値として残し、
//     status を failed にする。利用者は、pending を直して再度適用するか、破棄できる。
//
// 正式な値は常に「いま動いている値」を表し、status が最後の反映の成否を表す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 結果を確定する deploy の ID。
//   - succeeded: 反映に成功したか。
//
// 戻り値:
//   - 確定したら true。applying でなかったとき(すでに確定済みなど)は、何も変えず false。
//   - データベースのエラー。
func (deployRepository *DeployRepository) CommitConfigResult(requestContext context.Context, deployID string, succeeded bool) (bool, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(deployID) {
		return false, newEmptyConditionError()
	}
	// 確定したかを、トランザクションの外へ持ち出す入れ物。
	committed := false
	// 取得と更新を、1つのトランザクションで行う。
	transactionError := deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む。無いとき(削除された)は、確定するものが無いので、何もしない。
		var deploy model.Deploy
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Deploy{ID: deployID}).
			Take(&deploy).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return nil
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "deploy を取得できませんでした", findError)
		}
		// applying でなければ、別の処理が先に確定している(または削除が始まっている)ので、何もしない。
		if !deploy.IsApplying() {
			return nil
		}
		// 失敗のときは、status だけを failed にする(正式な値と pending は、そのまま)。
		if !succeeded {
			failError := transaction.Model(&model.Deploy{}).
				Where(&model.Deploy{ID: deployID}).
				Updates(&model.Deploy{Status: model.DeployStatusFailed}).Error
			if failError != nil {
				return apperror.Wrap(apperror.KindInternal, "起動設定の結果を確定できませんでした", failError)
			}
			committed = true
			return nil
		}
		// 成功のときの status を決める。
		targetStatus := model.DeployStatusActive
		if deploy.Replicas == 0 {
			targetStatus = model.DeployStatusScaledZero
		}
		// 適用する値が無いとき(通常は起きない)は、正式な値を NULL で壊さないよう、状態だけを変える。
		if len(deploy.PendingEnv) == 0 {
			statusError := transaction.Model(&model.Deploy{}).
				Where(&model.Deploy{ID: deployID}).
				Updates(&model.Deploy{Status: targetStatus}).Error
			if statusError != nil {
				return apperror.Wrap(apperror.KindInternal, "起動設定の結果を確定できませんでした", statusError)
			}
			committed = true
			return nil
		}
		// 正式な値を pending の内容にし、pending を NULL に戻す(列を指定して、NULL も書く)。
		updateError := transaction.Model(&model.Deploy{}).
			Where(&model.Deploy{ID: deployID}).
			Select("env", "command", "args", "pending_env", "pending_command", "pending_args", "status").
			Updates(&model.Deploy{
				Env:     deploy.PendingEnv,
				Command: deploy.PendingCommand,
				Args:    deploy.PendingArgs,
				Status:  targetStatus,
			}).Error
		// データベースのエラーは、内部エラーにして返す。
		if updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "起動設定の結果を確定できませんでした", updateError)
		}
		committed = true
		return nil
	})
	return committed, transactionError
}

// sameConfig は、送られた起動設定が、deploy の現在の値と同じ内容かを返す。
//
// 環境変数と引数は、JSON の書式(空白やキーの順)ではなく、中身で比べる。
//
// 引数:
//   - deploy: 現在の値を持つ deploy。
//   - env: 送られた環境変数(JSON)。
//   - command: 送られた起動コマンド。
//   - args: 送られた起動引数(JSON)。nil はイメージの既定。
//
// 戻り値:
//   - 3つとも同じなら true。
func sameConfig(deploy model.Deploy, env datatypes.JSON, command *string, args datatypes.JSON) bool {
	// 起動コマンドを比べる(両方 nil なら同じ)。
	if (command == nil) != (deploy.Command == nil) || (command != nil && *command != *deploy.Command) {
		return false
	}
	// 環境変数を、中身で比べる。
	if !sameJSON(deploy.Env, env) {
		return false
	}
	// 起動引数を、中身で比べる。
	return sameJSON(deploy.Args, args)
}

// sameJSON は、2つの JSON の値が、中身として同じかを返す。空(NULL)と JSON の null は、同じとして扱う。
//
// 引数:
//   - first: 1つ目の JSON。
//   - second: 2つ目の JSON。
//
// 戻り値:
//   - 同じなら true。どちらかが JSON として読めなければ false。
func sameJSON(first []byte, second []byte) bool {
	// 空と null は、どちらも「値なし」にそろえる。
	var firstValue, secondValue any
	if len(first) > 0 {
		if json.Unmarshal(first, &firstValue) != nil {
			return false
		}
	}
	if len(second) > 0 {
		if json.Unmarshal(second, &secondValue) != nil {
			return false
		}
	}
	// 読み込んだ値で比べる。
	return reflect.DeepEqual(firstValue, secondValue)
}
