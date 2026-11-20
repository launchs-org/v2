package repository

import (
	"context"
	"encoding/json"

	"launchs/shared/apperror"

	"gorm.io/gorm"
)

// DeployTagRepository は、deploy、volume、service、ingress のタグ(各テーブルの tags 列)の読み書きを担当する。
type DeployTagRepository struct {
	database *gorm.DB
}

// NewDeployTagRepository は、DeployTagRepository を作る。
//
// 引数:
//   - database: アプリのデータベースの接続。
func NewDeployTagRepository(database *gorm.DB) *DeployTagRepository {
	// 接続を持たせて返す。
	return &DeployTagRepository{database: database}
}

// タグを付けられる資源の種類。
const (
	TagKindDeploy  = "deploy"
	TagKindVolume  = "volume"
	TagKindService = "service"
	TagKindIngress = "ingress"
)

// tagTables は、資源の種類から、テーブル名と、利用者が指定する ID の列を引く表(service は、deploy の ID で指す)。
var tagTables = map[string]struct{ table, keyColumn string }{
	TagKindDeploy:  {"deploys", "id"},
	TagKindVolume:  {"volumes", "id"},
	TagKindService: {"services", "deploy_id"},
	TagKindIngress: {"ingresses", "id"},
}

// DeployTagCount は、タグと、そのタグが付いた資源の数(種類ごと)。型の名前は、deploy だけだった頃のまま。
type DeployTagCount struct {
	// Tag は、タグ。
	Tag string `gorm:"column:tag"`

	// DeployCount は、そのタグが付いた deploy の数。
	DeployCount int `gorm:"column:deploy_count"`

	// VolumeCount は、そのタグが付いた volume の数。
	VolumeCount int `gorm:"column:volume_count"`

	// ServiceCount は、そのタグが付いた service の数。
	ServiceCount int `gorm:"column:service_count"`

	// IngressCount は、そのタグが付いた ingress の数。
	IngressCount int `gorm:"column:ingress_count"`
}

// TaggedResource は、タグが付いた資源 1 つ。
type TaggedResource struct {
	// Kind は、資源の種類(TagKind...)。
	Kind string `gorm:"column:kind"`

	// ID は、資源の ID。
	ID string `gorm:"column:id"`

	// DeployID は、service のときの、持ち主の deploy の ID(service の削除は、deploy の ID で指す)。他の種類では空。
	DeployID string `gorm:"column:deploy_id"`
}

// SetTags は、資源のタグを、渡した組に置き換える。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - kind: 資源の種類(TagKind...)。
//   - userID: 持ち主のユーザー ID。
//   - key: 資源の ID(service は、持ち主の deploy の ID)。
//   - tags: 整えたタグ(model.NormalizeDeployTags の結果)。
//
// 戻り値:
//   - 資源が無い(他人のものを含む)、または種類が不正なら not_found。データベースのエラーは internal_error。
func (repository *DeployTagRepository) SetTags(requestContext context.Context, kind string, userID string, key string, tags []string) error {
	// 種類を、テーブルに引く。
	target, known := tagTables[kind]
	// 条件の値が空、または種類が不明なら、見つからない扱いにする(Gorm は、空の条件を無視するため)。
	if !known || hasEmpty(userID, key) {
		return apperror.New(apperror.KindNotFound, "対象が見つかりません")
	}
	// タグを JSON にする。
	encoded, encodeError := json.Marshal(tags)
	if encodeError != nil {
		return apperror.Wrap(apperror.KindInternal, "タグを保存できませんでした", encodeError)
	}
	// 持ち主の資源の、タグの列だけを書き換える(テーブルと列の名前は、上の固定の表から取る)。
	result := repository.database.WithContext(requestContext).Exec(
		"UPDATE "+target.table+" SET tags = ?::jsonb WHERE "+target.keyColumn+" = ? AND user_id = ?", string(encoded), key, userID)
	if result.Error != nil {
		return apperror.Wrap(apperror.KindInternal, "タグを保存できませんでした", result.Error)
	}
	// 1 行も変わらなければ、資源が無い。
	if result.RowsAffected == 0 {
		return apperror.New(apperror.KindNotFound, "対象が見つかりません")
	}
	return nil
}

// ListTags は、プロジェクトの資源に付いているタグを、種類ごとの数と一緒に、タグの昇順で返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//
// 戻り値:
//   - タグと数の一覧(無ければ空)。データベースのエラーは internal_error。
func (repository *DeployTagRepository) ListTags(requestContext context.Context, userID string, projectID string) ([]DeployTagCount, error) {
	// 条件の値が空なら、空の一覧を返す。
	if hasEmpty(userID, projectID) {
		return []DeployTagCount{}, nil
	}
	// 4 つのテーブルのタグを、行に展開して、1 つにつなぎ、タグごと、種類ごとに数える。
	counts := []DeployTagCount{}
	queryError := repository.database.WithContext(requestContext).Raw(
		`SELECT tag,
			COUNT(*) FILTER (WHERE kind = 'deploy') AS deploy_count, COUNT(*) FILTER (WHERE kind = 'volume') AS volume_count,
			COUNT(*) FILTER (WHERE kind = 'service') AS service_count, COUNT(*) FILTER (WHERE kind = 'ingress') AS ingress_count
		 FROM (
			SELECT 'deploy' AS kind, jsonb_array_elements_text(tags) AS tag FROM deploys WHERE project_id = @project AND user_id = @user
			UNION ALL SELECT 'volume', jsonb_array_elements_text(tags) FROM volumes WHERE project_id = @project AND user_id = @user
			UNION ALL SELECT 'service', jsonb_array_elements_text(tags) FROM services WHERE project_id = @project AND user_id = @user
			UNION ALL SELECT 'ingress', jsonb_array_elements_text(tags) FROM ingresses WHERE project_id = @project AND user_id = @user
		 ) AS tagged GROUP BY tag ORDER BY tag`, map[string]any{"project": projectID, "user": userID}).Scan(&counts).Error
	if queryError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "タグの一覧を取得できませんでした", queryError)
	}
	return counts, nil
}

// ListByTag は、プロジェクトの中で、タグが付いた資源(deploy、volume、service、ingress)を、種類ごとに、作成の古い順で返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - tag: タグ。
//
// 戻り値:
//   - 資源の一覧(無ければ空)。データベースのエラーは internal_error。
func (repository *DeployTagRepository) ListByTag(requestContext context.Context, userID string, projectID string, tag string) ([]TaggedResource, error) {
	// 条件の値が空なら、空の一覧を返す。
	if hasEmpty(userID, projectID, tag) {
		return []TaggedResource{}, nil
	}
	// タグ 1 つの JSON の配列を作る(包含の検索 @> に使う)。
	encoded, _ := json.Marshal([]string{tag})
	// 4 つのテーブルから、タグを含む資源を集める。
	resources := []TaggedResource{}
	queryError := repository.database.WithContext(requestContext).Raw(
		`SELECT 'deploy' AS kind, id, '' AS deploy_id, created_at FROM deploys WHERE project_id = @project AND user_id = @user AND tags @> CAST(@tag AS jsonb)
		 UNION ALL SELECT 'volume', id, '', created_at FROM volumes WHERE project_id = @project AND user_id = @user AND tags @> CAST(@tag AS jsonb)
		 UNION ALL SELECT 'service', id, deploy_id, created_at FROM services WHERE project_id = @project AND user_id = @user AND tags @> CAST(@tag AS jsonb)
		 UNION ALL SELECT 'ingress', id, '', created_at FROM ingresses WHERE project_id = @project AND user_id = @user AND tags @> CAST(@tag AS jsonb)
		 ORDER BY created_at ASC, id ASC`, map[string]any{"project": projectID, "user": userID, "tag": string(encoded)}).Scan(&resources).Error
	if queryError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "タグの資源を取得できませんでした", queryError)
	}
	return resources, nil
}
