package service

import (
	"context"

	"github.com/google/uuid"

	"launchs/shared/model"
	"launchs/shared/pagination"
)

// ConfigMapStore は、ConfigMap のユースケースが使う、データベースの操作。
type ConfigMapStore interface {
	// Create は、ConfigMap を、版 1 と一緒に保存する。
	Create(requestContext context.Context, configMap *model.ConfigMap, files []model.ConfigMapFile) error

	// ListByProject は、プロジェクトの ConfigMap を、新しい順に取得する。
	ListByProject(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.ConfigMapSummary, *pagination.Position, error)

	// FindByID は、ConfigMap と、指定した版(nil は最新)の中身を取得する。
	FindByID(requestContext context.Context, userID string, configMapID string, revisionNumber *int) (model.ConfigMap, model.ConfigMapRevision, error)

	// ReplaceFiles は、ファイルを全件置き換える(同じ中身なら、版を増やさない)。
	ReplaceFiles(requestContext context.Context, userID string, configMapID string, files []model.ConfigMapFile) (bool, int, error)

	// Rename は、名前を変える。
	Rename(requestContext context.Context, userID string, configMapID string, name string) error

	// Delete は、ConfigMap を、全ての版ごと削除する。
	Delete(requestContext context.Context, userID string, configMapID string) error

	// FindUsage は、ConfigMap を使っている deploy のマウントを取得する。
	FindUsage(requestContext context.Context, userID string, configMapID string) ([]model.ConfigMapUsage, error)

	// ListRevisions は、版の一覧を取得する。
	ListRevisions(requestContext context.Context, userID string, configMapID string) ([]model.ConfigMapRevisionSummary, error)
}

// ConfigMapService は、ConfigMap に関するユースケース。
// ConfigMap の編集は、版を足すだけで、k8s にも Temporal にも触れない(docs/configmap.md §3)。
type ConfigMapService struct {
	configMapStore ConfigMapStore
}

// NewConfigMapService は、ConfigMapService を作る。
//
// 引数:
//   - configMapStore: データベースの操作。
//
// 戻り値:
//   - 作成した ConfigMapService。
func NewConfigMapService(configMapStore ConfigMapStore) *ConfigMapService {
	// 依存するものを持たせて返す。
	return &ConfigMapService{configMapStore: configMapStore}
}

// Create は、プロジェクトに ConfigMap を作る(版 1 ができる)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 属するプロジェクトの ID。
//   - name: ConfigMap の名前(英数字とハイフンだけの1〜64文字)。
//   - files: 最初のファイル(空でもよい)。
//
// 戻り値:
//   - 作成した ConfigMap。
//   - 名前やファイルが不正なら invalid_request。プロジェクトが無ければ not_found。
//     active でない、同じ名前がある、または数の上限に達していれば conflict。
func (configMapService *ConfigMapService) Create(requestContext context.Context, userID string, projectID string, name string, files []model.ConfigMapFile) (model.ConfigMap, error) {
	// 名前の形を確かめる(volume の名前と同じ規則)。
	if nameError := model.ValidateVolumeName(name); nameError != nil {
		return model.ConfigMap{}, nameError
	}
	// ファイルを検査して、key の昇順にする。
	sortedFiles, filesError := model.ValidateConfigMapFiles(files)
	if filesError != nil {
		return model.ConfigMap{}, filesError
	}
	// ID を作って、保存する。
	configMap := model.ConfigMap{ID: model.ConfigMapIDPrefix + uuid.NewString(), ProjectID: projectID, UserID: userID, Name: name}
	if createError := configMapService.configMapStore.Create(requestContext, &configMap, sortedFiles); createError != nil {
		return model.ConfigMap{}, createError
	}
	return configMap, nil
}

// List は、プロジェクトの ConfigMap を、新しい順に返す(中身は含めない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - limit: 1ページの件数。
//   - after: 前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - 一覧と、次のページの位置。プロジェクトが無ければ not_found。
func (configMapService *ConfigMapService) List(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.ConfigMapSummary, *pagination.Position, error) {
	// 保存先に任せる。
	return configMapService.configMapStore.ListByProject(requestContext, userID, projectID, limit, after)
}

// Get は、ConfigMap と、指定した版(nil は最新)の中身を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//   - revisionNumber: 版の番号。nil なら最新。
//
// 戻り値:
//   - ConfigMap と、版の中身(ファイルの配列)。ConfigMap や版が無ければ not_found。
func (configMapService *ConfigMapService) Get(requestContext context.Context, userID string, configMapID string, revisionNumber *int) (model.ConfigMap, model.ConfigMapRevision, []model.ConfigMapFile, error) {
	// 保存先から読む(他のユーザーのものは、見つからない扱いになる)。
	configMap, revision, findError := configMapService.configMapStore.FindByID(requestContext, userID, configMapID, revisionNumber)
	if findError != nil {
		return model.ConfigMap{}, model.ConfigMapRevision{}, nil, findError
	}
	// 版のファイルを読み込む。
	files, parseError := model.ParseConfigMapFiles(revision.Files)
	if parseError != nil {
		return model.ConfigMap{}, model.ConfigMapRevision{}, nil, parseError
	}
	return configMap, revision, files, nil
}

// ReplaceFiles は、ConfigMap のファイルを、全件置き換える(同じ中身なら、版を増やさない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//   - files: 新しいファイル。
//
// 戻り値:
//   - 新しい版を作ったか、最新の版の番号。
//   - ファイルが不正なら invalid_request。ConfigMap が無ければ not_found。
func (configMapService *ConfigMapService) ReplaceFiles(requestContext context.Context, userID string, configMapID string, files []model.ConfigMapFile) (bool, int, error) {
	// ファイルを検査して、key の昇順にする。
	sortedFiles, filesError := model.ValidateConfigMapFiles(files)
	if filesError != nil {
		return false, 0, filesError
	}
	// 保存先に任せる。
	return configMapService.configMapStore.ReplaceFiles(requestContext, userID, configMapID, sortedFiles)
}

// Rename は、ConfigMap の名前を変える(即時反映)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//   - name: 新しい名前(形は、確かめてから変える)。
//
// 戻り値:
//   - 名前が不正なら invalid_request。無ければ not_found。同じ名前があれば conflict。
func (configMapService *ConfigMapService) Rename(requestContext context.Context, userID string, configMapID string, name string) error {
	// 名前の形を確かめる。
	if nameError := model.ValidateVolumeName(name); nameError != nil {
		return nameError
	}
	// 保存先に任せる。
	return configMapService.configMapStore.Rename(requestContext, userID, configMapID, name)
}

// Delete は、ConfigMap を、全ての版ごと削除する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//
// 戻り値:
//   - 無ければ not_found。
func (configMapService *ConfigMapService) Delete(requestContext context.Context, userID string, configMapID string) error {
	// 保存先に任せる。
	return configMapService.configMapStore.Delete(requestContext, userID, configMapID)
}

// ListRevisions は、ConfigMap の版の一覧を、新しい順に返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//
// 戻り値:
//   - 版の一覧。ConfigMap が無ければ not_found。
func (configMapService *ConfigMapService) ListRevisions(requestContext context.Context, userID string, configMapID string) ([]model.ConfigMapRevisionSummary, error) {
	// 保存先に任せる。
	return configMapService.configMapStore.ListRevisions(requestContext, userID, configMapID)
}

// Usage は、ConfigMap を使っている deploy のマウントを返す(現在と pending の両方)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - configMapID: ConfigMap の ID。
//
// 戻り値:
//   - 使っているマウントの一覧。ConfigMap が無ければ not_found。
func (configMapService *ConfigMapService) Usage(requestContext context.Context, userID string, configMapID string) ([]model.ConfigMapUsage, error) {
	// 保存先に任せる。
	return configMapService.configMapStore.FindUsage(requestContext, userID, configMapID)
}
