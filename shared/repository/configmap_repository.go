package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// ConfigMapRepository は、configmaps と configmap_revisions テーブルの読み書きを担当する。
// ConfigMap の編集は、版を足すだけで、k8s には触れない(docs/configmap.md §3)。
type ConfigMapRepository struct {
	database *gorm.DB
}

// NewConfigMapRepository は、ConfigMapRepository を作る。
//
// 引数:
//   - database: 使うデータベース。
//
// 戻り値:
//   - 作成した ConfigMapRepository。
func NewConfigMapRepository(database *gorm.DB) *ConfigMapRepository {
	// データベースを持たせて返す。
	return &ConfigMapRepository{database: database}
}

// encodeConfigMapFiles は、ファイルの配列を、保存する JSON にする。
//
// 引数:
//   - files: ファイルの配列(nil なら、空の配列として保存する)。
//
// 戻り値:
//   - JSON。作れなければ internal_error。
func encodeConfigMapFiles(files []model.ConfigMapFile) (datatypes.JSON, error) {
	// nil は、JSON の null ではなく、空の配列にする。
	if files == nil {
		files = []model.ConfigMapFile{}
	}
	// JSON にする。
	encoded, marshalError := json.Marshal(files)
	if marshalError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ファイルを保存する形にできませんでした", marshalError)
	}
	return encoded, nil
}

// Create は、プロジェクトに ConfigMap を作る(版 1 も、同じトランザクションで作る)。
//
// プロジェクトが、持ち主のものであること、active であることを、プロジェクトの行をロックして確かめる。
// k8s には、何もしない(deploy が適用するときに作る)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - configMap: 保存する ConfigMap。ID、ProjectID、UserID、Name は、呼び出し側で設定しておく(CurrentRevision は、1 にする)。
//   - files: 版 1 のファイル(検査済みで、key の昇順であること)。
//
// 戻り値:
//   - プロジェクトが無い、または他のユーザーのものなら not_found。
//     active でない、同じ名前がある、または 1 プロジェクトの上限(50)に達していれば conflict。
//   - それ以外のデータベースのエラーは internal_error。
func (configMapRepository *ConfigMapRepository) Create(requestContext context.Context, configMap *model.ConfigMap, files []model.ConfigMapFile) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(configMap.ID, configMap.ProjectID, configMap.UserID, configMap.Name) {
		return newEmptyConditionError()
	}
	// 保存する JSON を先に作る(失敗したら、何もしない)。
	encodedFiles, encodeError := encodeConfigMapFiles(files)
	if encodeError != nil {
		return encodeError
	}
	// プロジェクトの確認と、ConfigMap と版の保存を、1つのトランザクションで行う。
	return configMapRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 持ち主のプロジェクトを、他から状態を変えられないよう、ロックして読む。
		var project model.Project
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Project{ID: configMap.ProjectID, UserID: configMap.UserID}).
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
			return apperror.New(apperror.KindConflict, "プロジェクトは "+project.Status+" のため、ConfigMap を作れません")
		}
		// プロジェクトの行をロックしているので、同時に作られても、数えた値は正しい。
		var configMapCount int64
		if countError := transaction.Model(&model.ConfigMap{}).Where(&model.ConfigMap{ProjectID: configMap.ProjectID}).Count(&configMapCount).Error; countError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap の数を数えられませんでした", countError)
		}
		if configMapCount >= model.MaxConfigMapsPerProject {
			return apperror.New(apperror.KindConflict, "1つのプロジェクトに作れる ConfigMap は、"+itoaForMessage(model.MaxConfigMapsPerProject)+"個までです")
		}
		// 最初の版は 1。
		configMap.CurrentRevision = 1
		createError := transaction.Create(configMap).Error
		// 名前が重なったときは、conflict にする。
		if errors.Is(createError, gorm.ErrDuplicatedKey) {
			return apperror.New(apperror.KindConflict, "同じ名前の ConfigMap がすでにあります")
		}
		if createError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap を保存できませんでした", createError)
		}
		// 版 1 を保存する。
		revision := model.ConfigMapRevision{ConfigMapID: configMap.ID, Revision: 1, Files: encodedFiles}
		if revisionError := transaction.Create(&revision).Error; revisionError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap の版を保存できませんでした", revisionError)
		}
		return nil
	})
}

// itoaForMessage は、整数を、エラーメッセージ用の10進の文字列にする。
//
// 引数:
//   - value: 整数。
//
// 戻り値:
//   - 10進の文字列。
func itoaForMessage(value int) string {
	// JSON の数値の書式が、10進の整数と同じなので、それを使う。
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// summarizeConfigMapFiles は、版のファイルの JSON から、ファイルの数と、保存する大きさの合計を求める。
//
// 引数:
//   - raw: 版の files の JSON。
//
// 戻り値:
//   - ファイルの数、大きさの合計(バイト)。読めなければ internal_error。
func summarizeConfigMapFiles(raw datatypes.JSON) (int, int, error) {
	// JSON を読み込む。
	files, parseError := model.ParseConfigMapFiles(raw)
	if parseError != nil {
		return 0, 0, apperror.Wrap(apperror.KindInternal, "ConfigMap のファイルを読み込めませんでした", parseError)
	}
	return len(files), model.ConfigMapFilesTotalBytes(files), nil
}

// ListByProject は、プロジェクトの ConfigMap を、新しい順に取得する(中身は含めない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - limit: 1ページの件数。
//   - after: 2ページ目以降の、前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - ConfigMap の一覧(ファイルの数と大きさを含む)と、次のページの位置(最後のページなら nil)。
//   - プロジェクトが無い(他人のものを含む)ときは not_found。
func (configMapRepository *ConfigMapRepository) ListByProject(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.ConfigMapSummary, *pagination.Position, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID) {
		return nil, nil, apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// プロジェクトが、持ち主のものかを確かめる。
	var projectCount int64
	if countError := configMapRepository.database.WithContext(requestContext).Model(&model.Project{}).
		Where(&model.Project{ID: projectID, UserID: userID}).Count(&projectCount).Error; countError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", countError)
	}
	if projectCount == 0 {
		return nil, nil, apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// 新しい順に並べる検索を組み立てる(次のページがあるかを知るため、1件多く読む)。
	query := configMapRepository.database.WithContext(requestContext).
		Where(&model.ConfigMap{ProjectID: projectID}).
		Order("created_at DESC, id DESC").
		Limit(limit + 1)
	// 2ページ目以降なら、前のページの最後より後ろだけに絞る。
	if after != nil {
		query = query.Where("(created_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}
	// 検索を実行する。
	var configMaps []model.ConfigMap
	if findError := query.Find(&configMaps).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "ConfigMap の一覧を取得できませんでした", findError)
	}
	// limit 件以下なら、次のページは無い。多く読んだ1件は、外して、次のページの位置にする。
	var nextPosition *pagination.Position
	if len(configMaps) > limit {
		configMaps = configMaps[:limit]
		lastConfigMap := configMaps[limit-1]
		nextPosition = &pagination.Position{CreatedAt: lastConfigMap.CreatedAt, ID: lastConfigMap.ID}
	}
	// 各 ConfigMap の最新の版を、まとめて読んで、ファイルの数と大きさを求める。
	summaries := make([]model.ConfigMapSummary, 0, len(configMaps))
	for _, configMap := range configMaps {
		var revision model.ConfigMapRevision
		revisionError := configMapRepository.database.WithContext(requestContext).
			Where("configmap_id = ? AND revision = ?", configMap.ID, configMap.CurrentRevision).Take(&revision).Error
		if revisionError != nil {
			return nil, nil, apperror.Wrap(apperror.KindInternal, "ConfigMap の版を取得できませんでした", revisionError)
		}
		fileCount, totalBytes, summarizeError := summarizeConfigMapFiles(revision.Files)
		if summarizeError != nil {
			return nil, nil, summarizeError
		}
		usages, usageError := findConfigMapUsages(configMapRepository.database.WithContext(requestContext), configMap.ProjectID, configMap.ID)
		if usageError != nil {
			return nil, nil, usageError
		}
		summaries = append(summaries, model.ConfigMapSummary{ConfigMap: configMap, FileCount: fileCount, TotalBytes: totalBytes, UsedBy: countDistinctDeploys(usages)})
	}
	return summaries, nextPosition, nil
}

// FindByID は、ユーザーの ConfigMap を1件と、指定した版(省略は最新)の中身を取得する。
//
// 他のユーザーのものは、存在しないものとして扱う。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//   - revisionNumber: 取得する版の番号。nil なら、最新の版。
//
// 戻り値:
//   - ConfigMap と、その版の行。
//   - ConfigMap が無い(他人のものを含む)、または、指定した版が無い(消えた版を含む)ときは not_found。
func (configMapRepository *ConfigMapRepository) FindByID(requestContext context.Context, userID string, configMapID string, revisionNumber *int) (model.ConfigMap, model.ConfigMapRevision, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, configMapID) {
		return model.ConfigMap{}, model.ConfigMapRevision{}, apperror.New(apperror.KindNotFound, "ConfigMap が見つかりません")
	}
	// 持ち主の条件つきで探す。
	var configMap model.ConfigMap
	findError := configMapRepository.database.WithContext(requestContext).
		Where(&model.ConfigMap{ID: configMapID, UserID: userID}).Take(&configMap).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.ConfigMap{}, model.ConfigMapRevision{}, apperror.Wrap(apperror.KindNotFound, "ConfigMap が見つかりません", findError)
	}
	if findError != nil {
		return model.ConfigMap{}, model.ConfigMapRevision{}, apperror.Wrap(apperror.KindInternal, "ConfigMap を取得できませんでした", findError)
	}
	// 版の指定が無ければ、最新の版にする。
	wantedRevision := configMap.CurrentRevision
	if revisionNumber != nil {
		wantedRevision = *revisionNumber
	}
	// その版の中身を読む。
	var revision model.ConfigMapRevision
	revisionError := configMapRepository.database.WithContext(requestContext).
		Where("configmap_id = ? AND revision = ?", configMap.ID, wantedRevision).Take(&revision).Error
	if errors.Is(revisionError, gorm.ErrRecordNotFound) {
		return model.ConfigMap{}, model.ConfigMapRevision{}, apperror.Wrap(apperror.KindNotFound, "ConfigMap の版が見つかりません", revisionError)
	}
	if revisionError != nil {
		return model.ConfigMap{}, model.ConfigMapRevision{}, apperror.Wrap(apperror.KindInternal, "ConfigMap の版を取得できませんでした", revisionError)
	}
	return configMap, revision, nil
}

// ReplaceFiles は、ConfigMap のファイルを、全件置き換える。中身が最新の版と同じなら、版を増やさない。
//
// ConfigMap の行を、ロックして読み、最新の版と比べ、違えば新しい版(+1)を足す。
// 古い版は、直近の MaxConfigMapRevisionsKept 版と、どれかの deploy が使っている版(pending を含む)を残して、消す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//   - files: 新しいファイル(検査済みで、key の昇順であること)。
//
// 戻り値:
//   - 新しい版を作ったら true(同じ中身なら false)。
//   - 操作の後の、最新の版の番号。
//   - ConfigMap が無い(他人のものを含む)ときは not_found。
func (configMapRepository *ConfigMapRepository) ReplaceFiles(requestContext context.Context, userID string, configMapID string, files []model.ConfigMapFile) (bool, int, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, configMapID) {
		return false, 0, apperror.New(apperror.KindNotFound, "ConfigMap が見つかりません")
	}
	// 保存する JSON を先に作る(失敗したら、何もしない)。
	encodedFiles, encodeError := encodeConfigMapFiles(files)
	if encodeError != nil {
		return false, 0, encodeError
	}
	// 結果を、トランザクションの外へ取り出すための変数。
	created := false
	currentRevision := 0
	transactionError := configMapRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 同時に編集されないよう、ConfigMap の行を、ロックして読む。
		var configMap model.ConfigMap
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.ConfigMap{ID: configMapID, UserID: userID}).Take(&configMap).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "ConfigMap が見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap を取得できませんでした", findError)
		}
		// 最新の版を読んで、中身を比べる。
		var latest model.ConfigMapRevision
		latestError := transaction.Where("configmap_id = ? AND revision = ?", configMap.ID, configMap.CurrentRevision).Take(&latest).Error
		if latestError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap の版を取得できませんでした", latestError)
		}
		latestFiles, parseError := model.ParseConfigMapFiles(latest.Files)
		if parseError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap のファイルを読み込めませんでした", parseError)
		}
		currentRevision = configMap.CurrentRevision
		// 同じ中身なら、版を増やさない。
		if model.SameConfigMapFiles(latestFiles, files) {
			return nil
		}
		// 新しい版を足す。
		nextRevision := configMap.CurrentRevision + 1
		revision := model.ConfigMapRevision{ConfigMapID: configMap.ID, Revision: nextRevision, Files: encodedFiles}
		if revisionError := transaction.Create(&revision).Error; revisionError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap の版を保存できませんでした", revisionError)
		}
		// 最新の版の番号と、更新日時を変える。
		updateError := transaction.Model(&model.ConfigMap{}).Where("id = ?", configMap.ID).
			Updates(map[string]any{"current_revision": nextRevision, "updated_at": gorm.Expr("now()")}).Error
		if updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap を更新できませんでした", updateError)
		}
		// どれかの deploy が使っている版(pending を含む)は、残す。
		usages, usageError := findConfigMapUsages(transaction, configMap.ProjectID, configMap.ID)
		if usageError != nil {
			return usageError
		}
		keptRevisions := []int{0}
		for _, usage := range usages {
			keptRevisions = append(keptRevisions, usage.Revision)
		}
		// 直近の MaxConfigMapRevisionsKept 版より古い版を消す(使っている版を除く)。
		pruneError := transaction.Where("configmap_id = ? AND revision <= ? AND revision NOT IN ?", configMap.ID, nextRevision-model.MaxConfigMapRevisionsKept, keptRevisions).
			Delete(&model.ConfigMapRevision{}).Error
		if pruneError != nil {
			return apperror.Wrap(apperror.KindInternal, "古い版を消せませんでした", pruneError)
		}
		created = true
		currentRevision = nextRevision
		return nil
	})
	if transactionError != nil {
		return false, 0, transactionError
	}
	return created, currentRevision, nil
}

// Rename は、ConfigMap の名前を変える(即時反映)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//   - name: 新しい名前(書式は、呼び出し側で確かめておく)。
//
// 戻り値:
//   - 無い(他人のものを含む)ときは not_found。同じ名前が、プロジェクトの中にあれば conflict。
func (configMapRepository *ConfigMapRepository) Rename(requestContext context.Context, userID string, configMapID string, name string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, configMapID, name) {
		return newEmptyConditionError()
	}
	// 持ち主の条件つきで、名前と更新日時を変える。
	result := configMapRepository.database.WithContext(requestContext).Model(&model.ConfigMap{}).
		Where("id = ? AND user_id = ?", configMapID, userID).
		Updates(map[string]any{"name": name, "updated_at": gorm.Expr("now()")})
	// 名前が重なったときは、conflict にする。
	if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
		return apperror.New(apperror.KindConflict, "同じ名前の ConfigMap がすでにあります")
	}
	if result.Error != nil {
		return apperror.Wrap(apperror.KindInternal, "ConfigMap の名前を変えられませんでした", result.Error)
	}
	// 1行も変わらなければ、無い(または他人のもの)。
	if result.RowsAffected == 0 {
		return apperror.New(apperror.KindNotFound, "ConfigMap が見つかりません")
	}
	return nil
}

// Delete は、ConfigMap を、全ての版ごと削除する。
//
// ConfigMap の行を、更新用にロックして、どれかの deploy が使っていない(pending のマウントも含む)ことを確かめてから消す。
// deploy がマウントを保存するときは、ConfigMap の行を共有ロックするので、確かめてから消すまでの間に、使われ始めることは無い。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//
// 戻り値:
//   - 無い(他人のものを含む)ときは not_found。
//   - deploy が使っているときは conflict(メッセージに、使っている deploy の ID を入れる)。
func (configMapRepository *ConfigMapRepository) Delete(requestContext context.Context, userID string, configMapID string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, configMapID) {
		return apperror.New(apperror.KindNotFound, "ConfigMap が見つかりません")
	}
	// ロック、使用中の確認、削除を、1つのトランザクションで行う。
	return configMapRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 持ち主の条件つきで、更新用にロックして読む。
		var configMap model.ConfigMap
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.ConfigMap{ID: configMapID, UserID: userID}).Take(&configMap).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "ConfigMap が見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap を取得できませんでした", findError)
		}
		// 使っている deploy があれば、消さない。
		usages, usageError := findConfigMapUsages(transaction, configMap.ProjectID, configMap.ID)
		if usageError != nil {
			return usageError
		}
		if len(usages) > 0 {
			deployIDs := distinctDeployIDs(usages)
			return apperror.New(apperror.KindConflict, "deploy が使っているため、ConfigMap を削除できません。使っている deploy: "+strings.Join(deployIDs, ", "))
		}
		// 消す(版は、外部キーの CASCADE で消える)。
		if deleteError := transaction.Delete(&model.ConfigMap{}, "id = ?", configMap.ID).Error; deleteError != nil {
			return apperror.Wrap(apperror.KindInternal, "ConfigMap を削除できませんでした", deleteError)
		}
		return nil
	})
}

// findConfigMapUsages は、ConfigMap を使っている deploy のマウントを、全て取得する(現在と pending の両方)。
//
// 引数:
//   - database: 使うデータベース(またはトランザクション)。
//   - projectID: ConfigMap のプロジェクトの ID。
//   - configMapID: ConfigMap の ID。
//
// 戻り値:
//   - 使っているマウントの一覧(deploy の ID の順)。無ければ空。
func findConfigMapUsages(database *gorm.DB, projectID string, configMapID string) ([]model.ConfigMapUsage, error) {
	// この ConfigMap の ID を含む要素を持つ配列を、検索に使う(JSON の包含)。
	needle := `[{"configMapId":` + string(mustMarshalString(configMapID)) + `}]`
	var deploys []model.Deploy
	findError := database.Where("project_id = ? AND (configmap_mounts @> ?::jsonb OR pending_configmap_mounts @> ?::jsonb)", projectID, needle, needle).
		Order("id").Find(&deploys).Error
	if findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ConfigMap を使っている deploy を調べられませんでした", findError)
	}
	// deploy ごとに、現在と pending のマウントから、この ConfigMap のものを集める。
	usages := []model.ConfigMapUsage{}
	for _, deploy := range deploys {
		for _, pending := range []bool{false, true} {
			raw := deploy.ConfigMapMounts
			if pending {
				raw = deploy.PendingConfigMapMounts
			}
			mounts, parseError := model.ParseConfigMapMounts(raw)
			if parseError != nil {
				return nil, apperror.Wrap(apperror.KindInternal, "ConfigMap のマウントを読めませんでした", parseError)
			}
			for _, mount := range mounts {
				if mount.ConfigMapID == configMapID {
					usages = append(usages, model.ConfigMapUsage{DeployID: deploy.ID, Revision: mount.Revision, Key: mount.Key, Path: mount.Path, Pending: pending})
				}
			}
		}
	}
	return usages, nil
}

// distinctDeployIDs は、マウントの一覧から、deploy の ID を、重複なく、出てきた順に取り出す。
//
// 引数:
//   - usages: マウントの一覧。
//
// 戻り値:
//   - deploy の ID。
func distinctDeployIDs(usages []model.ConfigMapUsage) []string {
	// 初めて出てきた ID だけを入れる。
	seen := map[string]bool{}
	deployIDs := []string{}
	for _, usage := range usages {
		if !seen[usage.DeployID] {
			seen[usage.DeployID] = true
			deployIDs = append(deployIDs, usage.DeployID)
		}
	}
	return deployIDs
}

// countDistinctDeploys は、マウントの一覧に出てくる deploy の数を返す。
//
// 引数:
//   - usages: マウントの一覧。
//
// 戻り値:
//   - deploy の数。
func countDistinctDeploys(usages []model.ConfigMapUsage) int {
	// 重複を除いた数。
	return len(distinctDeployIDs(usages))
}

// FindUsage は、ConfigMap を使っている deploy のマウントを、全て取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//
// 戻り値:
//   - 使っているマウントの一覧(現在と pending の両方)。
//   - ConfigMap が無い(他人のものを含む)ときは not_found。
func (configMapRepository *ConfigMapRepository) FindUsage(requestContext context.Context, userID string, configMapID string) ([]model.ConfigMapUsage, error) {
	// 持ち主の ConfigMap を読む。
	configMap, _, findError := configMapRepository.FindByID(requestContext, userID, configMapID, nil)
	if findError != nil {
		return nil, findError
	}
	// 使っているマウントを調べる。
	return findConfigMapUsages(configMapRepository.database.WithContext(requestContext), configMap.ProjectID, configMap.ID)
}

// FindRevisionForWorker は、持ち主を問わずに、ConfigMap の版を1件取得する(ワーカー用)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - configMapID: ConfigMap の ID。
//   - revision: 版の番号。
//
// 戻り値:
//   - 版の行。無いときは not_found。
func (configMapRepository *ConfigMapRepository) FindRevisionForWorker(requestContext context.Context, configMapID string, revision int) (model.ConfigMapRevision, error) {
	// 条件の値が空だと、Gorm が条件を無視するので、先に拒否する。
	if hasEmpty(configMapID) {
		return model.ConfigMapRevision{}, apperror.New(apperror.KindNotFound, "ConfigMap の版が見つかりません")
	}
	// ID と版で探す。
	var found model.ConfigMapRevision
	findError := configMapRepository.database.WithContext(requestContext).
		Where("configmap_id = ? AND revision = ?", configMapID, revision).Take(&found).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.ConfigMapRevision{}, apperror.Wrap(apperror.KindNotFound, "ConfigMap の版が見つかりません", findError)
	}
	if findError != nil {
		return model.ConfigMapRevision{}, apperror.Wrap(apperror.KindInternal, "ConfigMap の版を取得できませんでした", findError)
	}
	return found, nil
}

// ListInUseResourceNames は、プロジェクトの deploy が使っている(現在と pending の両方)、k8s の ConfigMap の名前を全て返す(ワーカー用)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - projectID: プロジェクトの ID。
//
// 戻り値:
//   - 使われている名前("<configMapId>-r<revision>")の集合。
func (configMapRepository *ConfigMapRepository) ListInUseResourceNames(requestContext context.Context, projectID string) (map[string]bool, error) {
	// 条件の値が空だと、全ての行が対象になるので、先に拒否する。
	if hasEmpty(projectID) {
		return nil, newEmptyConditionError()
	}
	// プロジェクトの deploy を読む。
	var deploys []model.Deploy
	if findError := configMapRepository.database.WithContext(requestContext).Where("project_id = ?", projectID).Find(&deploys).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "deploy を取得できませんでした", findError)
	}
	// 現在と pending のマウントから、名前を集める。
	names := map[string]bool{}
	for _, deploy := range deploys {
		for _, raw := range []datatypes.JSON{deploy.ConfigMapMounts, deploy.PendingConfigMapMounts} {
			mounts, parseError := model.ParseConfigMapMounts(raw)
			if parseError != nil {
				return nil, apperror.Wrap(apperror.KindInternal, "ConfigMap のマウントを読めませんでした", parseError)
			}
			for _, mount := range mounts {
				names[model.ConfigMapRevisionResourceName(mount.ConfigMapID, mount.Revision)] = true
			}
		}
	}
	return names, nil
}

// ListRevisions は、ConfigMap の版の一覧を、新しい順に取得する(中身は含めない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//
// 戻り値:
//   - 版の一覧(残っている版だけ。多くても MaxConfigMapRevisionsKept 件)。
//   - ConfigMap が無い(他人のものを含む)ときは not_found。
func (configMapRepository *ConfigMapRepository) ListRevisions(requestContext context.Context, userID string, configMapID string) ([]model.ConfigMapRevisionSummary, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, configMapID) {
		return nil, apperror.New(apperror.KindNotFound, "ConfigMap が見つかりません")
	}
	// 持ち主の ConfigMap かを確かめる。
	var configMapCount int64
	if countError := configMapRepository.database.WithContext(requestContext).Model(&model.ConfigMap{}).
		Where(&model.ConfigMap{ID: configMapID, UserID: userID}).Count(&configMapCount).Error; countError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ConfigMap を取得できませんでした", countError)
	}
	if configMapCount == 0 {
		return nil, apperror.New(apperror.KindNotFound, "ConfigMap が見つかりません")
	}
	// 版を、新しい順に読む。
	var revisions []model.ConfigMapRevision
	if findError := configMapRepository.database.WithContext(requestContext).
		Where("configmap_id = ?", configMapID).Order("revision DESC").Find(&revisions).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ConfigMap の版を取得できませんでした", findError)
	}
	// ファイルの数と大きさを求めて、一覧にする。
	summaries := make([]model.ConfigMapRevisionSummary, 0, len(revisions))
	for _, revision := range revisions {
		fileCount, totalBytes, summarizeError := summarizeConfigMapFiles(revision.Files)
		if summarizeError != nil {
			return nil, summarizeError
		}
		summaries = append(summaries, model.ConfigMapRevisionSummary{Revision: revision.Revision, CreatedAt: revision.CreatedAt, FileCount: fileCount, TotalBytes: totalBytes})
	}
	return summaries, nil
}
