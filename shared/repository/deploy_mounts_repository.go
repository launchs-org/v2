package repository

import (
	"context"
	"encoding/json"
	"sort"

	"launchs/shared/apperror"
	"launchs/shared/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// validateMountTargets は、マウント先の volume が、同じプロジェクトにあり、マウントできる状態であることを確かめる(トランザクションの中で使う)。
//
// 確かめる volume の行は、更新用にロックする(volume の削除の受け付けと、同時には進まないようにするため)。
// ロックは、デッドロックを避けるため、ID の順に取る。
//
// 引数:
//   - transaction: 使っているトランザクション。
//   - projectID: deploy のプロジェクトの ID。
//   - mounts: 確かめるマウント。
//   - failureKind: 不正なときのエラーの種類(保存のときは invalid_request、適用のときは conflict)。
//
// 戻り値:
//   - 不正なら、failureKind のエラー。
func validateMountTargets(transaction *gorm.DB, projectID string, mounts []model.MountSpec, failureKind apperror.Kind) error {
	// マウントが無ければ、確かめるものが無い。
	if len(mounts) == 0 {
		return nil
	}
	// volume の ID を、順に並べて集める。
	volumeIDs := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		volumeIDs = append(volumeIDs, mount.VolumeID)
	}
	sort.Strings(volumeIDs)
	// 同じプロジェクトの volume を、ロックして読む。
	var volumes []model.Volume
	findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("project_id = ? AND id IN ?", projectID, volumeIDs).Order("id").Find(&volumes).Error
	if findError != nil {
		return apperror.Wrap(apperror.KindInternal, "volume を取得できませんでした", findError)
	}
	// ID から引けるようにする。
	volumesByID := map[string]model.Volume{}
	for _, volume := range volumes {
		volumesByID[volume.ID] = volume
	}
	// マウントごとに、volume があり、マウントできる状態かを確かめる。
	for _, mount := range mounts {
		volume, exists := volumesByID[mount.VolumeID]
		if !exists || !volume.CanBeMounted() {
			return apperror.New(failureKind, "このプロジェクトに、マウントできる volume がありません: "+mount.VolumeID)
		}
	}
	return nil
}

// SavePendingMounts は、マウントの構成を、まだ適用しない値として保存する(一括置き換え)。
//
// 現在のマウントと同じ内容(JSON の書式ではなく、中身で比べる)なら、変更が無いものとして、pending を保存せず空にする
// (以前の pending も消える)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//   - mounts: 保存するマウント(形は、呼び出し側で確かめてある)。空の配列は、全て外す変更。
//
// 戻り値:
//   - deploy が無い(他人のものを含む)なら not_found。反映中・削除の途中なら conflict。
//   - マウント先の volume が不正なら invalid_request。
func (deployRepository *DeployRepository) SavePendingMounts(requestContext context.Context, userID string, deployID string, mounts []model.MountSpec) error {
	// 保存する JSON を作る(nil は、空の配列にそろえる)。
	if mounts == nil {
		mounts = []model.MountSpec{}
	}
	encoded, marshalError := json.Marshal(mounts)
	if marshalError != nil {
		return apperror.Wrap(apperror.KindInternal, "マウントを保存できませんでした", marshalError)
	}
	// 取得と更新を、1つのトランザクションで行う。
	return deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 編集できる状態であることを、行をロックして確かめる。
		deploy, lockError := lockEditableDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// マウント先が、同じプロジェクトの、マウントできる volume であることを確かめる。
		if targetError := validateMountTargets(transaction, deploy.ProjectID, mounts, apperror.KindInvalidRequest); targetError != nil {
			return targetError
		}
		// マウント先が、ファイルの path と、まったく同じでないことを確かめる(ファイルで、ボリュームを覆わないため)。
		// 照らすファイルは、pending があればそれ、無ければ、現在のもの。
		filesRaw := deploy.Files
		if deploy.HasPendingFiles() {
			filesRaw = deploy.PendingFiles
		}
		files, parseFilesError := model.ParseFiles(filesRaw)
		if parseFilesError != nil {
			return apperror.Wrap(apperror.KindInternal, "ファイルを読めませんでした", parseFilesError)
		}
		if overlapError := model.ValidateFiles(files, mounts); overlapError != nil {
			return apperror.New(apperror.KindInvalidRequest, "マウント先が、ファイルの path と重なっています: "+overlapError.Error())
		}
		// ConfigMap のマウントの path とも、まったく同じでないことを確かめる(照らす相手は、pending があればそれ、無ければ現在のもの)。
		if overlapError := checkConfigMapMountOverlaps(effectiveJSON(deploy.ConfigMapMounts, deploy.PendingConfigMapMounts), effectiveJSON(deploy.Files, deploy.PendingFiles), encoded); overlapError != nil {
			return apperror.New(apperror.KindInvalidRequest, "マウント先が、ConfigMap のマウントの path と重なっています: "+overlapError.Error())
		}
		// 現在と同じ内容なら、pending は空にする(現在が NULL のときは、空の配列と同じ内容として比べる)。
		deploy.PendingMounts = datatypes.JSON(encoded)
		if sameMounts(deploy.Mounts, encoded) {
			deploy.PendingMounts = nil
		}
		// 変えた deploy を保存する。
		if saveError := transaction.Save(&deploy).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "マウントを保存できませんでした", saveError)
		}
		return nil
	})
}

// sameMounts は、2つのマウント構成が、中身として同じかを返す。NULL と空の配列は、同じ(マウント無し)として扱う。
//
// 引数:
//   - current: 現在のマウント(NULL かもしれない)。
//   - next: 新しいマウント(JSON の配列)。
//
// 戻り値:
//   - 同じなら true。
func sameMounts(current []byte, next []byte) bool {
	// 現在が空(NULL)のときは、空の配列として比べる。
	if len(current) == 0 {
		current = []byte("[]")
	}
	return sameJSON(current, next)
}

// DiscardPendingMounts は、pending のマウントの構成を破棄する(マウントには影響しない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - deploy が無ければ not_found。反映中・削除の途中、または pending が無ければ conflict。
func (deployRepository *DeployRepository) DiscardPendingMounts(requestContext context.Context, userID string, deployID string) error {
	// 取得と更新を、1つのトランザクションで行う。
	return deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 編集できる状態であることを、行をロックして確かめる。
		deploy, lockError := lockEditableDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// 破棄する値が無いときは、conflict。
		if !deploy.HasPendingMounts() {
			return apperror.New(apperror.KindConflict, "破棄するマウントの変更がありません")
		}
		// pending を空にして保存する。
		deploy.PendingMounts = nil
		if saveError := transaction.Save(&deploy).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "マウントを破棄できませんでした", saveError)
		}
		return nil
	})
}

// BeginMountsApply は、マウントの反映を始める(状態を applying にする)。
//
// マウントの保存から適用までの間に、マウント先の volume が削除中になっていることがあるので、ここで改めて確かめる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 反映する deploy の ID。
//
// 戻り値:
//   - 変える前の状態(開始に失敗したとき、元に戻すために使う)。
//   - deploy が無ければ not_found。反映中・削除の途中・起動中、適用する値が無い、volume が不正なら conflict。
func (deployRepository *DeployRepository) BeginMountsApply(requestContext context.Context, userID string, deployID string) (string, error) {
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
		acceptedStatus, beginError := deploy.BeginMountsApply()
		if beginError != nil {
			return beginError
		}
		// pending のマウントを読み、マウント先を改めて確かめる。
		pendingMounts, parseError := model.ParseMounts(deploy.PendingMounts)
		if parseError != nil {
			return apperror.Wrap(apperror.KindInternal, "pending のマウントを読めませんでした", parseError)
		}
		if targetError := validateMountTargets(transaction, deploy.ProjectID, pendingMounts, apperror.KindConflict); targetError != nil {
			return targetError
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

// CommitMountsResult は、マウントの反映の結果を確定する。
//
// 成功ならマウントを pending の内容にして pending を空に戻し、落ち着いた状態にする。失敗ならマウントと pending は変えず、failed にする。
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
func (deployRepository *DeployRepository) CommitMountsResult(requestContext context.Context, deployID string, succeeded bool) (bool, error) {
	// モデルに、結果の確定を任せる。
	return transitionDeploy(deployRepository.database, requestContext, deployID, func(deploy *model.Deploy) bool {
		return deploy.CommitMounts(succeeded)
	})
}
