package repository

import (
	"context"
	"encoding/json"
	"sort"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// resolveConfigMapMounts は、ConfigMap のマウント構成を、データベースと照らして確かめ、版を決めて返す(トランザクションの中で使う)。
//
// 確かめること: ConfigMap が、deploy と同じプロジェクト・同じ持ち主にあること、版が残っていること、その版に key があること。
// 版の決め方: 指定があれば、それ。省略(0)なら、deploy が(pending があれば pending、無ければ現在)すでに同じ ConfigMap の同じ key を
// マウントしているときは、その版を保つ。無ければ、ConfigMap の最新の版。
// ConfigMap の行は、共有ロックする(ConfigMap の削除は、更新用にロックして、使用中かを確かめるので、同時には進まない)。ロックは、ID の順に取る。
//
// 引数:
//   - transaction: 使っているトランザクション。
//   - deploy: 対象の deploy(版を保つための、すでにあるマウントを読む)。
//   - mounts: 確かめるマウント(形は、確かめてある)。
//   - failureKind: 不正なときのエラーの種類(保存のときは invalid_request、適用のときは conflict)。
//
// 戻り値:
//   - 版を決めたマウント(引数と同じ順)。
//   - 不正なら、failureKind のエラー。
func resolveConfigMapMounts(transaction *gorm.DB, deploy model.Deploy, mounts []model.ConfigMapMountSpec, failureKind apperror.Kind) ([]model.ConfigMapMountSpec, error) {
	// マウントが無ければ、確かめるものが無い。
	if len(mounts) == 0 {
		return mounts, nil
	}
	// 版を保つための、すでにあるマウントを読む(pending があればそれ、無ければ現在のもの)。
	existingRaw := deploy.ConfigMapMounts
	if deploy.HasPendingConfigMapMounts() {
		existingRaw = deploy.PendingConfigMapMounts
	}
	existingMounts, parseError := model.ParseConfigMapMounts(existingRaw)
	if parseError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ConfigMap のマウントを読めませんでした", parseError)
	}
	existingRevisions := map[string]int{}
	for _, existing := range existingMounts {
		existingRevisions[existing.ConfigMapID+"\x00"+existing.Key] = existing.Revision
	}
	// ConfigMap の ID を、順に並べて集める。
	idSet := map[string]bool{}
	for _, mount := range mounts {
		idSet[mount.ConfigMapID] = true
	}
	configMapIDs := make([]string, 0, len(idSet))
	for configMapID := range idSet {
		configMapIDs = append(configMapIDs, configMapID)
	}
	sort.Strings(configMapIDs)
	// 同じプロジェクト・同じ持ち主の ConfigMap を、共有ロックして読む。
	var configMaps []model.ConfigMap
	findError := transaction.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("project_id = ? AND user_id = ? AND id IN ?", deploy.ProjectID, deploy.UserID, configMapIDs).Order("id").Find(&configMaps).Error
	if findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ConfigMap を取得できませんでした", findError)
	}
	configMapsByID := map[string]model.ConfigMap{}
	for _, configMap := range configMaps {
		configMapsByID[configMap.ID] = configMap
	}
	// マウントごとに、版を決めて、その版に key があるかを確かめる。
	resolved := make([]model.ConfigMapMountSpec, 0, len(mounts))
	loadedFiles := map[string][]model.ConfigMapFile{}
	for _, mount := range mounts {
		configMap, exists := configMapsByID[mount.ConfigMapID]
		if !exists {
			return nil, apperror.New(failureKind, "このプロジェクトに、この ConfigMap がありません: "+mount.ConfigMapID)
		}
		// 版を決める(指定、すでにある版、最新の版の順)。
		if mount.Revision == 0 {
			if kept, keeps := existingRevisions[mount.ConfigMapID+"\x00"+mount.Key]; keeps && kept > 0 {
				mount.Revision = kept
			} else {
				mount.Revision = configMap.CurrentRevision
			}
		}
		// その版の中身を読む(同じ版は、1度だけ読む)。
		cacheKey := model.ConfigMapRevisionResourceName(mount.ConfigMapID, mount.Revision)
		files, loaded := loadedFiles[cacheKey]
		if !loaded {
			var revision model.ConfigMapRevision
			revisionError := transaction.Where("configmap_id = ? AND revision = ?", mount.ConfigMapID, mount.Revision).Limit(1).Find(&revision).Error
			if revisionError != nil {
				return nil, apperror.Wrap(apperror.KindInternal, "ConfigMap の版を取得できませんでした", revisionError)
			}
			if revision.ConfigMapID == "" {
				return nil, apperror.New(failureKind, "ConfigMap の版が残っていません: "+cacheKey)
			}
			parsedFiles, filesError := model.ParseConfigMapFiles(revision.Files)
			if filesError != nil {
				return nil, apperror.Wrap(apperror.KindInternal, "ConfigMap のファイルを読めませんでした", filesError)
			}
			files = parsedFiles
			loadedFiles[cacheKey] = files
		}
		hasKey := false
		for _, file := range files {
			if file.Key == mount.Key {
				hasKey = true
			}
		}
		if !hasKey {
			return nil, apperror.New(failureKind, "ConfigMap の版に、この key のファイルがありません: "+cacheKey+" の "+mount.Key)
		}
		resolved = append(resolved, mount)
	}
	return resolved, nil
}

// checkConfigMapMountOverlaps は、ConfigMap のマウントの path が、ファイルの path と、ボリュームのマウント先と、まったく同じでないこと、
// および数の合計が上限以内であることを、JSON の列から読んで確かめる。
//
// 引数:
//   - configMapMountsRaw: ConfigMap のマウントの JSON。
//   - filesRaw: ファイルの JSON。
//   - mountsRaw: ボリュームのマウントの JSON。
//
// 戻り値:
//   - 重なれば invalid_request(読めなければ internal_error)。
func checkConfigMapMountOverlaps(configMapMountsRaw datatypes.JSON, filesRaw datatypes.JSON, mountsRaw datatypes.JSON) error {
	// 3つとも読み込む。
	configMapMounts, configMapError := model.ParseConfigMapMounts(configMapMountsRaw)
	if configMapError != nil {
		return apperror.Wrap(apperror.KindInternal, "ConfigMap のマウントを読めませんでした", configMapError)
	}
	files, filesError := model.ParseFiles(filesRaw)
	if filesError != nil {
		return apperror.Wrap(apperror.KindInternal, "ファイルを読めませんでした", filesError)
	}
	mounts, mountsError := model.ParseMounts(mountsRaw)
	if mountsError != nil {
		return apperror.Wrap(apperror.KindInternal, "マウントを読めませんでした", mountsError)
	}
	// モデルの規則で確かめる。
	return model.ValidateConfigMapMountPaths(configMapMounts, files, mounts)
}

// effectiveJSON は、pending があれば pending、無ければ現在の値を返す(他の種類の構成と、重なりを照らすときに使う)。
//
// 引数:
//   - current: 現在の値。
//   - pending: pending の値。
//
// 戻り値:
//   - pending が(空の配列を含めて)あれば pending。無ければ current。
func effectiveJSON(current datatypes.JSON, pending datatypes.JSON) datatypes.JSON {
	// pending があれば、それ。
	if len(pending) > 0 {
		return pending
	}
	return current
}

// SavePendingConfigMapMounts は、ConfigMap のマウント構成を、まだ適用しない値として保存する(一括置き換え)。
//
// 版は、保存するときに決めて固定する(resolveConfigMapMounts)。現在と同じ内容なら、pending を保存せず空にする。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//   - mounts: 保存するマウント(形は、呼び出し側で確かめてある)。空の配列は、全て外す変更。
//
// 戻り値:
//   - deploy が無い(他人のものを含む)なら not_found。反映中・削除の途中なら conflict。
//   - ConfigMap や key が無い、path が重なるなら invalid_request。
func (deployRepository *DeployRepository) SavePendingConfigMapMounts(requestContext context.Context, userID string, deployID string, mounts []model.ConfigMapMountSpec) error {
	// nil は、空の配列にそろえる。
	if mounts == nil {
		mounts = []model.ConfigMapMountSpec{}
	}
	// 取得と更新を、1つのトランザクションで行う。
	return deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 編集できる状態であることを、行をロックして確かめる。
		deploy, lockError := lockEditableDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// ConfigMap と key を確かめ、版を決める。
		resolved, resolveError := resolveConfigMapMounts(transaction, deploy, mounts, apperror.KindInvalidRequest)
		if resolveError != nil {
			return resolveError
		}
		encoded, marshalError := json.Marshal(resolved)
		if marshalError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap のマウントを保存できませんでした", marshalError)
		}
		// ファイルと、ボリュームのマウント先と、重ならないことを確かめる(照らす相手は、pending があればそれ、無ければ現在のもの)。
		if overlapError := checkConfigMapMountOverlaps(datatypes.JSON(encoded), effectiveJSON(deploy.Files, deploy.PendingFiles), effectiveJSON(deploy.Mounts, deploy.PendingMounts)); overlapError != nil {
			return overlapError
		}
		// 現在と同じ内容なら、pending は空にする(現在が NULL のときは、空の配列と同じ内容として比べる)。
		deploy.PendingConfigMapMounts = datatypes.JSON(encoded)
		current := []byte(deploy.ConfigMapMounts)
		if len(current) == 0 {
			current = []byte("[]")
		}
		if sameJSON(current, encoded) {
			deploy.PendingConfigMapMounts = nil
		}
		// 変えた deploy を保存する。
		if saveError := transaction.Save(&deploy).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap のマウントを保存できませんでした", saveError)
		}
		return nil
	})
}

// DiscardPendingConfigMapMounts は、pending の ConfigMap のマウント構成を破棄する(マウントには影響しない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - deploy が無ければ not_found。反映中・削除の途中、または pending が無ければ conflict。
func (deployRepository *DeployRepository) DiscardPendingConfigMapMounts(requestContext context.Context, userID string, deployID string) error {
	// 取得と更新を、1つのトランザクションで行う。
	return deployRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 編集できる状態であることを、行をロックして確かめる。
		deploy, lockError := lockEditableDeploy(transaction, userID, deployID)
		if lockError != nil {
			return lockError
		}
		// 破棄する値が無いときは、conflict。
		if !deploy.HasPendingConfigMapMounts() {
			return apperror.New(apperror.KindConflict, "破棄する ConfigMap のマウントの変更がありません")
		}
		// pending を空にして保存する。
		deploy.PendingConfigMapMounts = nil
		if saveError := transaction.Save(&deploy).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap のマウントを破棄できませんでした", saveError)
		}
		return nil
	})
}

// BeginConfigMapMountsApply は、ConfigMap のマウントの反映を始める(状態を applying にする)。
//
// 保存から適用までの間に、ConfigMap が消えたり、他のファイルと path が重なったりしていないかを、改めて確かめる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 反映する deploy の ID。
//
// 戻り値:
//   - 変える前の状態(開始に失敗したとき、元に戻すために使う)。
//   - deploy が無ければ not_found。反映中・削除の途中・起動中、適用する値が無い、ConfigMap が不正、path が重なるなら conflict。
func (deployRepository *DeployRepository) BeginConfigMapMountsApply(requestContext context.Context, userID string, deployID string) (string, error) {
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
		acceptedStatus, beginError := deploy.BeginConfigMapMountsApply()
		if beginError != nil {
			return beginError
		}
		// pending のマウントを読み、ConfigMap と版と key を、改めて確かめる(版は、保存のときに固定してある)。
		pendingMounts, parseError := model.ParseConfigMapMounts(deploy.PendingConfigMapMounts)
		if parseError != nil {
			return apperror.Wrap(apperror.KindInternal, "pending の ConfigMap のマウントを読めませんでした", parseError)
		}
		if _, resolveError := resolveConfigMapMounts(transaction, deploy, pendingMounts, apperror.KindConflict); resolveError != nil {
			return resolveError
		}
		// 反映されているファイルとマウントと、path が重ならないことを、改めて確かめる(pending のものは、まだ使われないので、照らさない)。
		if overlapError := checkConfigMapMountOverlaps(deploy.PendingConfigMapMounts, deploy.Files, deploy.Mounts); overlapError != nil {
			return apperror.New(apperror.KindConflict, "適用する ConfigMap のマウントが、他のファイルやマウントと重なっています: "+overlapError.Error())
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

// CommitConfigMapMountsResult は、ConfigMap のマウントの反映の結果を確定する。
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
func (deployRepository *DeployRepository) CommitConfigMapMountsResult(requestContext context.Context, deployID string, succeeded bool) (bool, error) {
	// モデルに、結果の確定を任せる。
	return transitionDeploy(deployRepository.database, requestContext, deployID, func(deploy *model.Deploy) bool {
		return deploy.CommitConfigMapMounts(succeeded)
	})
}

// FindConfigMapLatestRevisions は、同じプロジェクトの ConfigMap の、最新の版の番号と名前を、まとめて取得する(画面に「新しい版があります」と出すため)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - projectID: プロジェクトの ID。
//   - configMapIDs: ConfigMap の ID(空なら、空を返す)。
//
// 戻り値:
//   - ConfigMap の ID から、ConfigMap の行への対応(無いものは含まない)。
func (deployRepository *DeployRepository) FindConfigMapLatestRevisions(requestContext context.Context, projectID string, configMapIDs []string) (map[string]model.ConfigMap, error) {
	// 条件の値が空だと、全ての行が対象になりかねないので、先に拒否する。
	if hasEmpty(projectID) {
		return nil, newEmptyConditionError()
	}
	result := map[string]model.ConfigMap{}
	if len(configMapIDs) == 0 {
		return result, nil
	}
	// プロジェクトと ID の条件で取得する。
	var configMaps []model.ConfigMap
	if findError := deployRepository.database.WithContext(requestContext).Where("project_id = ? AND id IN ?", projectID, configMapIDs).Find(&configMaps).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ConfigMap を取得できませんでした", findError)
	}
	for _, configMap := range configMaps {
		result[configMap.ID] = configMap
	}
	return result, nil
}
