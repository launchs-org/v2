package repository

import (
	"context"
	"encoding/json"
	"errors"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VolumeRepository は、volumes テーブルの読み書きを担当する。
// 状態の規則は model.Volume が持ち、ここは「ロックして読む → モデルを呼ぶ → 保存する」を行う。
type VolumeRepository struct {
	database *gorm.DB
}

// NewVolumeRepository は、VolumeRepository を作る。
//
// 引数:
//   - database: 使うデータベース。
//
// 戻り値:
//   - 作成した VolumeRepository。
func NewVolumeRepository(database *gorm.DB) *VolumeRepository {
	return &VolumeRepository{database: database}
}

// Create は、プロジェクトに volume を1つ保存する(status は active)。
//
// プロジェクトが、持ち主のものであること、active であることを、プロジェクトの行をロックして確かめる。
// PVC は、ここでは作らない(最初にマウントを適用するときに作る)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - volume: 保存する volume。ID、ProjectID、UserID、Name、SizeMB は、呼び出し側で設定しておく。
//
// 戻り値:
//   - プロジェクトが無い、または他のユーザーのものなら not_found。
//     active でない、同じ名前の volume がある、またはクォータを超えるなら conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (volumeRepository *VolumeRepository) Create(requestContext context.Context, volume *model.Volume) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(volume.ID, volume.ProjectID, volume.UserID, volume.Name) {
		return newEmptyConditionError()
	}
	// プロジェクトの確認と volume の保存を、1つのトランザクションで行う。
	return volumeRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// ユーザーのクォータを最初にロックして、使用量を数える(ロックの順番は、どの操作でも、クォータが先)。
		limits, usage, quotaError := loadQuota(transaction, volume.UserID, true)
		if quotaError != nil {
			return quotaError
		}
		// 持ち主のプロジェクトを、他から状態を変えられないよう、ロックして読む。
		var project model.Project
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Project{ID: volume.ProjectID, UserID: volume.UserID}).
			Take(&project).Error
		// 無い、または他のユーザーのものは、存在しないものとして not_found にする。
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "プロジェクトが見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", findError)
		}
		// 稼働中でないプロジェクトには、作れない。
		if project.Status != model.ProjectStatusActive {
			return apperror.New(apperror.KindConflict, "プロジェクトは "+project.Status+" のため、volume を作れません")
		}
		// volume の数と、大きさの合計が、上限に収まるかを確かめる。
		if limitError := limits.CheckVolumeCreate(usage, volume.SizeMB); limitError != nil {
			return limitError
		}
		// 作成直後は active。
		volume.Status = model.VolumeStatusActive
		volume.PendingSizeMB = nil
		createError := transaction.Create(volume).Error
		// 名前が重なったときは、conflict にする。
		if errors.Is(createError, gorm.ErrDuplicatedKey) {
			return apperror.New(apperror.KindConflict, "同じ名前の volume がすでにあります")
		}
		if createError != nil {
			return apperror.Wrap(apperror.KindInternal, "volume を保存できませんでした", createError)
		}
		return nil
	})
}

// ListByProject は、プロジェクトの volume を、新しい順に取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - limit: 1ページの件数。
//   - after: 2ページ目以降の、前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - volume の一覧と、次のページの位置(最後のページなら nil)。
//   - プロジェクトが無い(他人のものを含む)ときは not_found。
func (volumeRepository *VolumeRepository) ListByProject(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.Volume, *pagination.Position, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID) {
		return nil, nil, apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// プロジェクトが、持ち主のものかを確かめる。
	var projectCount int64
	if countError := volumeRepository.database.WithContext(requestContext).Model(&model.Project{}).
		Where(&model.Project{ID: projectID, UserID: userID}).Count(&projectCount).Error; countError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", countError)
	}
	if projectCount == 0 {
		return nil, nil, apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// 新しい順に並べる検索を組み立てる(次のページがあるかを知るため、1件多く読む)。
	query := volumeRepository.database.WithContext(requestContext).
		Where(&model.Volume{ProjectID: projectID}).
		Order("created_at DESC, id DESC").
		Limit(limit + 1)
	// 2ページ目以降なら、前のページの最後より後ろだけに絞る。
	if after != nil {
		query = query.Where("(created_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}
	// 検索を実行する。
	var volumes []model.Volume
	if findError := query.Find(&volumes).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "volume の一覧を取得できませんでした", findError)
	}
	// limit 件以下なら、次のページは無い。
	if len(volumes) <= limit {
		return volumes, nil, nil
	}
	// 確認のために多く読んだ1件を外し、次のページの位置を作る。
	volumes = volumes[:limit]
	lastVolume := volumes[limit-1]
	return volumes, &pagination.Position{CreatedAt: lastVolume.CreatedAt, ID: lastVolume.ID}, nil
}

// FindByID は、ユーザーのプロジェクトの volume を1件取得し、マウントしている deploy の ID も返す。
//
// 他のユーザーのものは、存在しないものとして扱う。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: volume の ID。
//
// 戻り値:
//   - volume と、現在マウントしている deploy の ID の一覧(無ければ空)。
//   - 無い(他人のものを含む)ときは not_found。
func (volumeRepository *VolumeRepository) FindByID(requestContext context.Context, userID string, projectID string, volumeID string) (model.Volume, []string, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, volumeID) {
		return model.Volume{}, nil, apperror.New(apperror.KindNotFound, "volume が見つかりません")
	}
	// 持ち主とプロジェクトの条件つきで探す。
	var volume model.Volume
	findError := volumeRepository.database.WithContext(requestContext).
		Where(&model.Volume{ID: volumeID, ProjectID: projectID, UserID: userID}).
		Take(&volume).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Volume{}, nil, apperror.Wrap(apperror.KindNotFound, "volume が見つかりません", findError)
	}
	if findError != nil {
		return model.Volume{}, nil, apperror.Wrap(apperror.KindInternal, "volume を取得できませんでした", findError)
	}
	// マウントしている deploy を調べる(現在のマウントだけ)。
	deployIDs, mountedError := findMountingDeploys(volumeRepository.database.WithContext(requestContext), volume.ProjectID, volume.ID, false)
	if mountedError != nil {
		return model.Volume{}, nil, mountedError
	}
	return volume, deployIDs, nil
}

// FindForWorker は、持ち主を問わずに、volume を ID で1件取得する(ワーカー用)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - volumeID: volume の ID。
//
// 戻り値:
//   - volume。無いときは not_found。
func (volumeRepository *VolumeRepository) FindForWorker(requestContext context.Context, volumeID string) (model.Volume, error) {
	// 条件の値が空だと、Gorm が条件を無視して先頭の1件を返すので、先に拒否する。
	if hasEmpty(volumeID) {
		return model.Volume{}, apperror.New(apperror.KindNotFound, "volume が見つかりません")
	}
	// ID で探す。
	var volume model.Volume
	findError := volumeRepository.database.WithContext(requestContext).Where(&model.Volume{ID: volumeID}).Take(&volume).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Volume{}, apperror.Wrap(apperror.KindNotFound, "volume が見つかりません", findError)
	}
	if findError != nil {
		return model.Volume{}, apperror.Wrap(apperror.KindInternal, "volume を取得できませんでした", findError)
	}
	return volume, nil
}

// FindManyForWorker は、同じプロジェクトの volume を、ID の一覧でまとめて取得する(ワーカー用)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - projectID: プロジェクトの ID。
//   - volumeIDs: 取得する volume の ID(空なら、空を返す)。
//
// 戻り値:
//   - 見つかった volume(無いものは含まない)。
func (volumeRepository *VolumeRepository) FindManyForWorker(requestContext context.Context, projectID string, volumeIDs []string) ([]model.Volume, error) {
	// 条件の値が空だと、全ての行が対象になりかねないので、先に拒否する(IDが無いときは、何も取らない)。
	if hasEmpty(projectID) {
		return nil, newEmptyConditionError()
	}
	if len(volumeIDs) == 0 {
		return []model.Volume{}, nil
	}
	// プロジェクトと ID の条件で取得する。
	var volumes []model.Volume
	findError := volumeRepository.database.WithContext(requestContext).
		Where("project_id = ? AND id IN ?", projectID, volumeIDs).Find(&volumes).Error
	if findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "volume を取得できませんでした", findError)
	}
	return volumes, nil
}

// findMountingDeploys は、volume をマウントしている deploy の ID を返す。
//
// 引数:
//   - database: 使うデータベース(トランザクションでもよい)。
//   - projectID: プロジェクトの ID。
//   - volumeID: volume の ID。
//   - includePending: true なら、まだ適用していないマウントの構成も、マウントとして数える。
//
// 戻り値:
//   - deploy の ID の一覧(無ければ空)。
func findMountingDeploys(database *gorm.DB, projectID string, volumeID string, includePending bool) ([]string, error) {
	// 検索する JSON(この volume の ID を含む要素を持つ配列)を作る。volumeId だけで一致させる(mountPath は問わない)。
	needle := `[{"volumeId":` + string(mustMarshalString(volumeID)) + `}]`
	// 条件を組み立てる。
	condition := "project_id = ? AND (mounts @> ?::jsonb"
	if includePending {
		condition += " OR pending_mounts @> ?::jsonb"
	}
	condition += ")"
	arguments := []any{projectID, needle}
	if includePending {
		arguments = append(arguments, needle)
	}
	// deploy の ID を集める。
	deployIDs := []string{}
	findError := database.Model(&model.Deploy{}).Where(condition, arguments...).Order("id").Pluck("id", &deployIDs).Error
	if findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "マウントしている deploy を調べられませんでした", findError)
	}
	return deployIDs, nil
}

// mustMarshalString は、文字列を、JSON の文字列にする(文字列は、必ず JSON にできる)。
//
// 引数:
//   - text: 文字列。
//
// 戻り値:
//   - 引用符つきの JSON の文字列。
func mustMarshalString(text string) []byte {
	// 文字列の JSON 化は失敗しないので、エラーは使わない。
	encoded, _ := json.Marshal(text)
	return encoded
}

// lockVolume は、ユーザーのプロジェクトの volume を、更新用にロックして取得する(トランザクションの中で使う)。状態は確かめない。
//
// 引数:
//   - transaction: 使っているトランザクション。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: volume の ID。
//
// 戻り値:
//   - ロックした volume。無い、または他のユーザーのものなら not_found。
func lockVolume(transaction *gorm.DB, userID string, projectID string, volumeID string) (model.Volume, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, volumeID) {
		return model.Volume{}, apperror.New(apperror.KindNotFound, "volume が見つかりません")
	}
	// 持ち主とプロジェクトの条件つきで、ロックして読む。
	var volume model.Volume
	findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&model.Volume{ID: volumeID, ProjectID: projectID, UserID: userID}).
		Take(&volume).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Volume{}, apperror.Wrap(apperror.KindNotFound, "volume が見つかりません", findError)
	}
	if findError != nil {
		return model.Volume{}, apperror.Wrap(apperror.KindInternal, "volume を取得できませんでした", findError)
	}
	return volume, nil
}

// transitionVolume は、volume 1行に対して transitionRow を行う。
//
// 引数:
//   - database: 使うデータベース。
//   - requestContext: キャンセルや期限を伝える context。
//   - volumeID: 操作する volume の ID。
//   - operation: モデルの操作。変えたら true を返す。
//
// 戻り値:
//   - 変えて保存したら true。
//   - データベースのエラー。
func transitionVolume(database *gorm.DB, requestContext context.Context, volumeID string, operation func(volume *model.Volume) bool) (bool, error) {
	// 型を決めて、共通の処理に任せる。
	return transitionRow[model.Volume](database, requestContext, volumeID, operation)
}

// updateVolume は、volume を、ロックして読み、モデルの操作(エラーを返すもの)を呼び、保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: 操作する volume の ID。
//   - failureMessage: 保存に失敗したときのメッセージ。
//   - operation: モデルの操作。エラーを返したら、保存せずにそのエラーを返す。
//
// 戻り値:
//   - volume が無ければ not_found。操作が返したエラー。データベースのエラーは internal_error。
func (volumeRepository *VolumeRepository) updateVolume(requestContext context.Context, userID string, projectID string, volumeID string, failureMessage string, operation func(volume *model.Volume) error) error {
	// 取得と更新を、1つのトランザクションで行う。
	return volumeRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む。
		volume, lockError := lockVolume(transaction, userID, projectID, volumeID)
		if lockError != nil {
			return lockError
		}
		// モデルに操作を任せる。
		if operationError := operation(&volume); operationError != nil {
			return operationError
		}
		// 変えた volume を保存する(NULL も書くので、全ての列を書く)。
		saveError := transaction.Save(&volume).Error
		if errors.Is(saveError, gorm.ErrDuplicatedKey) {
			return apperror.New(apperror.KindConflict, "同じ名前の volume がすでにあります")
		}
		if saveError != nil {
			return apperror.Wrap(apperror.KindInternal, failureMessage, saveError)
		}
		return nil
	})
}

// Rename は、volume の名前を変える(即時反映。pending を使わない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: volume の ID。
//   - name: 新しい名前(形は、確かめてある)。
//
// 戻り値:
//   - volume が無ければ not_found。反映中・削除中、または同じ名前があれば conflict。
func (volumeRepository *VolumeRepository) Rename(requestContext context.Context, userID string, projectID string, volumeID string, name string) error {
	// 名前が空のまま保存しないよう、先に拒否する。
	if hasEmpty(name) {
		return newEmptyConditionError()
	}
	// モデルに、名前の変更を任せる。
	return volumeRepository.updateVolume(requestContext, userID, projectID, volumeID, "名前を変更できませんでした", func(volume *model.Volume) error {
		return volume.Rename(name)
	})
}

// SavePendingSize は、拡張する大きさを、まだ適用しない値として保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: volume の ID。
//   - sizeMB: 新しい大きさ(MB)。
//   - maxSizeMB: 大きさの上限(MB)。
//
// 戻り値:
//   - volume が無ければ not_found。反映中・削除中、またはクォータ(大きさの合計)を超えるなら conflict。縮小・1つの上限超過なら invalid_request。
func (volumeRepository *VolumeRepository) SavePendingSize(requestContext context.Context, userID string, projectID string, volumeID string, sizeMB int, maxSizeMB int) error {
	// クォータの確認と更新を、1つのトランザクションで行う。
	return volumeRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// ユーザーのクォータを最初にロックして、使用量を数える(ロックの順番は、どの操作でも、クォータが先)。
		limits, usage, quotaError := loadQuota(transaction, userID, true)
		if quotaError != nil {
			return quotaError
		}
		// volume を、ロックして読む。
		volume, lockError := lockVolume(transaction, userID, projectID, volumeID)
		if lockError != nil {
			return lockError
		}
		// モデルに、保存を任せる(状態、縮小、1つの上限の確認は、モデルが持つ)。
		reservedBefore := volume.ReservedMB()
		if operationError := volume.SetPendingSize(sizeMB, maxSizeMB); operationError != nil {
			return operationError
		}
		// 予約が増えるときだけ、増える分で、大きさの合計が、上限に収まるかを確かめる(使用量には、いまの予約が入っている)。
		if added := volume.ReservedMB() - reservedBefore; added > 0 {
			if limitError := limits.CheckVolumeGrow(usage, added); limitError != nil {
				return limitError
			}
		}
		// 変えた volume を保存する(NULL も書くので、全ての列を書く)。
		if saveError := transaction.Save(&volume).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "大きさを保存できませんでした", saveError)
		}
		return nil
	})
}

// DiscardPendingSize は、pending の大きさを破棄する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: volume の ID。
//
// 戻り値:
//   - volume が無ければ not_found。反映中・削除中、または pending が無ければ conflict。
func (volumeRepository *VolumeRepository) DiscardPendingSize(requestContext context.Context, userID string, projectID string, volumeID string) error {
	// モデルに、破棄を任せる。
	return volumeRepository.updateVolume(requestContext, userID, projectID, volumeID, "大きさの変更を破棄できませんでした", func(volume *model.Volume) error {
		return volume.DiscardPendingSize()
	})
}

// BeginSizeApply は、拡張の反映を始める(状態を applying にする)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: 反映する volume の ID。
//
// 戻り値:
//   - 変える前の状態(開始に失敗したとき、元に戻すために使う)。
//   - volume が無ければ not_found。反映中・削除中、または pending が無ければ conflict。
func (volumeRepository *VolumeRepository) BeginSizeApply(requestContext context.Context, userID string, projectID string, volumeID string) (string, error) {
	// 前の状態を、トランザクションの外へ持ち出す入れ物。
	previousStatus := ""
	// モデルに、反映の開始を任せる。
	beginError := volumeRepository.updateVolume(requestContext, userID, projectID, volumeID, "volume の状態を更新できませんでした", func(volume *model.Volume) error {
		acceptedStatus, operationError := volume.BeginSizeApply()
		previousStatus = acceptedStatus
		return operationError
	})
	return previousStatus, beginError
}

// RevertApply は、反映の開始に失敗したとき、volume の状態を、反映を始める前の状態に戻す。
//
// applying でないとき、または無いときは、何もしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - volumeID: 戻す volume の ID。
//   - previousStatus: 戻す先の状態(BeginSizeApply が返したもの)。
//
// 戻り値:
//   - データベースのエラー。
func (volumeRepository *VolumeRepository) RevertApply(requestContext context.Context, volumeID string, previousStatus string) error {
	// 条件の値が空のときは拒否し、applying のときだけ、モデルに前の状態へ戻させる。
	if hasEmpty(previousStatus) {
		return newEmptyConditionError()
	}
	_, revertError := transitionVolume(volumeRepository.database, requestContext, volumeID, func(volume *model.Volume) bool {
		return volume.RevertApply(previousStatus)
	})
	return revertError
}

// CommitSizeApply は、拡張の反映の結果を確定する。
//
// 成功なら大きさを pending の値にして pending を空に戻し、active にする。失敗なら大きさと pending は変えず、failed にする。
// applying でなければ、何も変えず false を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - volumeID: 結果を確定する volume の ID。
//   - succeeded: 反映に成功したか。
//
// 戻り値:
//   - 確定したら true。
//   - データベースのエラー。
func (volumeRepository *VolumeRepository) CommitSizeApply(requestContext context.Context, volumeID string, succeeded bool) (bool, error) {
	// モデルに、結果の確定を任せる。
	return transitionVolume(volumeRepository.database, requestContext, volumeID, func(volume *model.Volume) bool {
		return volume.CommitSize(succeeded)
	})
}

// MarkDeleting は、volume の削除を受け付ける(状態を deleting にする)。
//
// どれかの deploy が、マウントしている(または、マウントの適用を待っている)ときは、受け付けない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - volumeID: 削除する volume の ID。
//
// 戻り値:
//   - 受け付ける前の状態(開始に失敗したとき、元に戻すために使う)。
//   - 無い(他人のものを含む)なら not_found。反映中・削除中、またはマウント中なら conflict。
func (volumeRepository *VolumeRepository) MarkDeleting(requestContext context.Context, userID string, projectID string, volumeID string) (string, error) {
	// 前の状態を、トランザクションの外へ持ち出す入れ物。
	previousStatus := ""
	// 取得と更新を、1つのトランザクションで行う。
	transactionError := volumeRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む(マウントの保存も、同じ行をロックするので、同時には進まない)。
		volume, lockError := lockVolume(transaction, userID, projectID, volumeID)
		if lockError != nil {
			return lockError
		}
		// マウントしている deploy があれば、受け付けない(適用待ちのマウントも含める)。
		deployIDs, mountedError := findMountingDeploys(transaction, volume.ProjectID, volume.ID, true)
		if mountedError != nil {
			return mountedError
		}
		if len(deployIDs) > 0 {
			return apperror.New(apperror.KindConflict, "volume は deploy "+deployIDs[0]+" にマウントされているため、削除できません(マウントを外してから削除してください)")
		}
		// モデルに、削除の受け付けを任せる。
		acceptedStatus, beginError := volume.BeginDelete()
		if beginError != nil {
			return beginError
		}
		// 変えた volume を保存する。
		if saveError := transaction.Save(&volume).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "volume を削除中にできませんでした", saveError)
		}
		previousStatus = acceptedStatus
		return nil
	})
	return previousStatus, transactionError
}

// RevertDeleting は、削除中にした volume を、削除を受け付ける前の状態に戻す(Workflow を開始できなかったとき)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - volumeID: 戻す volume の ID。
//   - previousStatus: 戻す先の状態(MarkDeleting が返したもの)。
//
// 戻り値:
//   - データベースのエラー。
func (volumeRepository *VolumeRepository) RevertDeleting(requestContext context.Context, volumeID string, previousStatus string) error {
	// 条件の値が空のときは拒否し、削除中のときだけ、モデルに前の状態へ戻させる。
	if hasEmpty(previousStatus) {
		return newEmptyConditionError()
	}
	_, revertError := transitionVolume(volumeRepository.database, requestContext, volumeID, func(volume *model.Volume) bool {
		return volume.RevertDeleting(previousStatus)
	})
	return revertError
}

// MarkDeleteFailed は、削除中の volume を、failed にする(削除の Workflow が、リトライの期限までに終えられなかったとき)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - volumeID: 削除に失敗した volume の ID。
//
// 戻り値:
//   - データベースのエラー。
func (volumeRepository *VolumeRepository) MarkDeleteFailed(requestContext context.Context, volumeID string) error {
	// 削除中のときだけ、モデルに failed へ変えさせる。
	_, markError := transitionVolume(volumeRepository.database, requestContext, volumeID, func(volume *model.Volume) bool {
		return volume.MarkDeleteFailed()
	})
	return markError
}

// DeleteRecord は、volume を物理削除する。削除の Workflow が、最後に使う。
//
// 削除中(deleting)の volume だけを消す(誤って消さないための安全装置)。繰り返し実行されても壊れない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - volumeID: 削除する volume の ID。
//
// 戻り値:
//   - データベースのエラー。
func (volumeRepository *VolumeRepository) DeleteRecord(requestContext context.Context, volumeID string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(volumeID) {
		return newEmptyConditionError()
	}
	// 削除中の volume だけを物理削除する。
	deleteError := volumeRepository.database.WithContext(requestContext).
		Where(&model.Volume{ID: volumeID, Status: model.VolumeStatusDeleting}).
		Delete(&model.Volume{}).Error
	if deleteError != nil {
		return apperror.Wrap(apperror.KindInternal, "volume を削除できませんでした", deleteError)
	}
	return nil
}
