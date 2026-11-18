package repository

import (
	"context"
	"encoding/json"

	"launchs/shared/apperror"
	"launchs/shared/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// SavePendingFiles は、ファイルの構成を、まだ適用しない値として保存する(一括置き換え)。
//
// 現在のファイルと同じ内容(JSON の書式ではなく、中身で比べる)なら、変更が無いものとして、pending を保存せず空にする
// (以前の pending も消える)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//   - files: 保存するファイル(形は、呼び出し側で確かめてある)。空の配列は、全て外す変更。
//
// 戻り値:
//   - deploy が無い(他人のものを含む)なら not_found。反映中・削除の途中なら conflict。
//   - マウント先と同じ path があるなど、不正なら invalid_request。
func (deployRepository *DeployRepository) SavePendingFiles(requestContext context.Context, userID string, deployID string, files []model.FileSpec) error {
	// 保存する JSON を作る(nil は、空の配列にそろえる)。
	if files == nil {
		files = []model.FileSpec{}
	}
	encoded, marshalError := json.Marshal(files)
	if marshalError != nil {
		return apperror.Wrap(apperror.KindInternal, "ファイルを保存できませんでした", marshalError)
	}
	// 取得と更新を、1つのトランザクションで行う。
	return deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 編集できる状態であることを、行をロックして確かめる。
		deploy, lockError := lockEditableDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// マウント先との重なりを、行をロックした状態で確かめる(マウントの保存と、同時には進まない)。
		// 照らすマウントは、pending があればそれ、無ければ、現在のもの。
		mountsRaw := deploy.Mounts
		if deploy.HasPendingMounts() {
			mountsRaw = deploy.PendingMounts
		}
		mounts, parseError := model.ParseMounts(mountsRaw)
		if parseError != nil {
			return apperror.Wrap(apperror.KindInternal, "マウントを読めませんでした", parseError)
		}
		if validateError := model.ValidateFiles(files, mounts); validateError != nil {
			return validateError
		}
		// ConfigMap のマウントの path と、まったく同じでないこと、数の合計が上限以内であることを確かめる(照らす相手は、pending があればそれ、無ければ現在のもの)。
		if overlapError := checkConfigMapMountOverlaps(effectiveJSON(deploy.ConfigMapMounts, deploy.PendingConfigMapMounts), encoded, mountsRaw); overlapError != nil {
			return overlapError
		}
		// 現在と同じ内容なら、pending は空にする(現在が NULL のときは、空の配列と同じ内容として比べる)。
		deploy.PendingFiles = datatypes.JSON(encoded)
		if sameFiles(deploy.Files, encoded) {
			deploy.PendingFiles = nil
		}
		// 変えた deploy を保存する。
		if saveError := transaction.Save(&deploy).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "ファイルを保存できませんでした", saveError)
		}
		return nil
	})
}

// sameFiles は、2つのファイル構成が、中身として同じかを返す。NULL と空の配列は、同じ(ファイル無し)として扱う。
//
// 引数:
//   - current: 現在のファイル(NULL かもしれない)。
//   - next: 新しいファイル(JSON の配列)。
//
// 戻り値:
//   - 同じなら true。
func sameFiles(current []byte, next []byte) bool {
	// 現在が空(NULL)のときは、空の配列として比べる。
	if len(current) == 0 {
		current = []byte("[]")
	}
	return sameJSON(current, next)
}

// DiscardPendingFiles は、pending のファイルの構成を破棄する(ファイルには影響しない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - deploy が無ければ not_found。反映中・削除の途中、または pending が無ければ conflict。
func (deployRepository *DeployRepository) DiscardPendingFiles(requestContext context.Context, userID string, deployID string) error {
	// 取得と更新を、1つのトランザクションで行う。
	return deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 編集できる状態であることを、行をロックして確かめる。
		deploy, lockError := lockEditableDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// 破棄する値が無いときは、conflict。
		if !deploy.HasPendingFiles() {
			return apperror.New(apperror.KindConflict, "破棄するファイルの変更がありません")
		}
		// pending を空にして保存する。
		deploy.PendingFiles = nil
		if saveError := transaction.Save(&deploy).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "ファイルを破棄できませんでした", saveError)
		}
		return nil
	})
}

// BeginFilesApply は、ファイルの反映を始める(状態を applying にする)。
//
// ファイルの保存から適用までの間に、マウントが変わって、ファイルの path と、同じマウント先ができていることがあるので、ここで改めて確かめる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 反映する deploy の ID。
//
// 戻り値:
//   - 変える前の状態(開始に失敗したとき、元に戻すために使う)。
//   - deploy が無ければ not_found。反映中・削除の途中・起動中、適用する値が無い、マウント先と重なるなら conflict。
func (deployRepository *DeployRepository) BeginFilesApply(requestContext context.Context, userID string, deployID string) (string, error) {
	// 前の状態を、トランザクションの外へ持ち出す入れ物。
	previousStatus := ""
	// 取得と更新を、1つのトランザクションで行う。
	transactionError := deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む(持ち主の条件つき)。
		deploy, lockError := lockDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// モデルに、反映の開始を任せる(状態の規則は、モデルが持つ)。
		acceptedStatus, beginError := deploy.BeginFilesApply()
		if beginError != nil {
			return beginError
		}
		// pending のファイルと、いま反映されているマウントを読み、重なりを改めて確かめる(pending のマウントは、まだ使われないので、照らさない)。
		pendingFiles, parseFilesError := model.ParseFiles(deploy.PendingFiles)
		if parseFilesError != nil {
			return apperror.Wrap(apperror.KindInternal, "pending のファイルを読めませんでした", parseFilesError)
		}
		mounts, parseMountsError := model.ParseMounts(deploy.Mounts)
		if parseMountsError != nil {
			return apperror.Wrap(apperror.KindInternal, "マウントを読めませんでした", parseMountsError)
		}
		if validateError := model.ValidateFiles(pendingFiles, mounts); validateError != nil {
			return apperror.New(apperror.KindConflict, "適用するファイルが、マウントと重なっています: "+validateError.Error())
		}
		// 反映されている ConfigMap のマウントとも、重ならないことを確かめる。
		if overlapError := checkConfigMapMountOverlaps(deploy.ConfigMapMounts, deploy.PendingFiles, deploy.Mounts); overlapError != nil {
			return apperror.New(apperror.KindConflict, "適用するファイルが、ConfigMap のマウントと重なっています: "+overlapError.Error())
		}
		// 変えた deploy を保存する。
		if saveError := transaction.Save(&deploy).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "deploy の状態を更新できませんでした", saveError)
		}
		previousStatus = acceptedStatus
		return nil
	})
	return previousStatus, transactionError
}

// CommitFilesResult は、ファイルの反映の結果を確定する。
//
// 成功ならファイルを pending の内容にして pending を空に戻し、落ち着いた状態にする。失敗ならファイルと pending は変えず、failed にする。
// applying でなければ、何も変えず false を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: 結果を確定する deploy の ID。
//   - succeeded: 反映に成功したか。
//
// 戻り値:
//   - 確定したら true。
//   - データベースのエラー。
func (deployRepository *DeployRepository) CommitFilesResult(requestContext context.Context, deployID string, succeeded bool) (bool, error) {
	// モデルに、結果の確定を任せる。
	return transitionDeploy(deployRepository.database, requestContext, deployID, func(deploy *model.Deploy) bool {
		return deploy.CommitFiles(succeeded)
	})
}
