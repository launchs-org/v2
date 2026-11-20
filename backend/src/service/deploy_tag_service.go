package service

import (
	"context"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/repository"
)

// DeployTagStore は、DeployTagService が必要とする、タグの保存先の操作(実体は repository.DeployTagRepository)。
type DeployTagStore interface {
	SetTags(requestContext context.Context, kind string, userID string, key string, tags []string) error
	ListTags(requestContext context.Context, userID string, projectID string) ([]repository.DeployTagCount, error)
	ListByTag(requestContext context.Context, userID string, projectID string, tag string) ([]repository.TaggedResource, error)
}

// DeployDeleter は、deploy の削除を受け付ける操作(実体は DeployService)。
type DeployDeleter interface {
	Delete(requestContext context.Context, userID string, deployID string) error
}

// ServiceDeleter は、service の削除を受け付ける操作(実体は ServiceService。deploy の ID で指す)。
type ServiceDeleter interface {
	Delete(requestContext context.Context, userID string, deployID string) error
}

// ProjectResourceDeleter は、プロジェクトの資源(volume、ingress)の削除を受け付ける操作(実体は VolumeService、IngressService)。
type ProjectResourceDeleter interface {
	Delete(requestContext context.Context, userID string, projectID string, resourceID string) error
}

// TagDeleters は、タグの一斉削除が、種類ごとに呼ぶ、削除の部品。
type TagDeleters struct {
	Deploy  DeployDeleter
	Service ServiceDeleter
	Volume  ProjectResourceDeleter
	Ingress ProjectResourceDeleter
}

// MetricStore は、DeployMetricService が必要とする、メトリクスの読み取り(実体は repository.DeployMetricRepository)。
type MetricStore interface {
	Series(requestContext context.Context, userID string, scope repository.MetricScope, since time.Time) ([]repository.MetricPoint, error)
}

// DeployTagService は、deploy のタグの設定、一覧、タグ単位の一斉削除を担当する。
type DeployTagService struct {
	tagStore DeployTagStore
	deleters TagDeleters
}

// NewDeployTagService は、DeployTagService を作る。
//
// 引数:
//   - tagStore: タグの保存先。
//   - deleters: 種類ごとの、削除を受け付ける部品。
func NewDeployTagService(tagStore DeployTagStore, deleters TagDeleters) *DeployTagService {
	// 部品を持たせて返す。
	return &DeployTagService{tagStore: tagStore, deleters: deleters}
}

// SetTags は、資源(deploy、volume、service、ingress)のタグを置き換える。空の配列なら、全て外す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - kind: 資源の種類(repository.TagKind...)。
//   - key: 資源の ID(service は、持ち主の deploy の ID)。
//   - tags: 新しいタグ。
//
// 戻り値:
//   - 整えて保存したタグ。タグが不正なら invalid_request、deploy が無ければ not_found。
func (tagService *DeployTagService) SetTags(requestContext context.Context, kind string, userID string, key string, tags []string) ([]string, error) {
	// タグを検査して、整える。
	normalized, normalizeError := model.NormalizeDeployTags(tags)
	if normalizeError != nil {
		return nil, normalizeError
	}
	// 保存する。
	if saveError := tagService.tagStore.SetTags(requestContext, kind, userID, key, normalized); saveError != nil {
		return nil, saveError
	}
	return normalized, nil
}

// ListTags は、プロジェクトのタグの一覧(deploy の数つき)を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//
// 戻り値:
//   - タグと数の一覧。
func (tagService *DeployTagService) ListTags(requestContext context.Context, userID string, projectID string) ([]repository.DeployTagCount, error) {
	// 保存先から読む。
	return tagService.tagStore.ListTags(requestContext, userID, projectID)
}

// TagDeleteItem は、一斉削除で、受け付けた(または受け付けられなかった)資源 1 つ。
type TagDeleteItem struct {
	// Kind は、資源の種類(deploy、volume、service、ingress)。
	Kind string

	// ID は、資源の ID。
	ID string

	// Message は、受け付けられなかった理由(受け付けたときは空)。
	Message string
}

// TagDeleteResult は、タグ単位の一斉削除の結果。
type TagDeleteResult struct {
	// Accepted は、削除を受け付けた資源(完了は非同期)。
	Accepted []TagDeleteItem

	// Failed は、受け付けられなかった資源(理由つき)。
	Failed []TagDeleteItem
}

// tagDeleteOrder は、一斉削除で、削除を受け付ける順(外から内へ。volume は、データを守るため、一斉削除の対象にしない)。
var tagDeleteOrder = []string{repository.TagKindIngress, repository.TagKindService, repository.TagKindDeploy}

// DeleteByTag は、プロジェクトの中で、タグが付いた資源(ingress、service、deploy)の削除を、全て受け付ける。volume は、データを守るため、削除しない(個別に消す)。
//
// 1 つの削除が失敗しても、他は続ける(失敗は、結果に入れる)。1 つの削除の規則(反映中は conflict、deploy が使っている volume は conflict など)は、そのまま適用される。
// 削除の完了は非同期。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - tag: 削除する資源のタグ。
//
// 戻り値:
//   - 受け付けた資源と、失敗の一覧。タグが不正なら invalid_request、そのタグの資源が 1 つも無ければ not_found。
func (tagService *DeployTagService) DeleteByTag(requestContext context.Context, userID string, projectID string, tag string) (TagDeleteResult, error) {
	// タグを検査する。
	normalized, normalizeError := model.NormalizeDeployTags([]string{tag})
	if normalizeError != nil || len(normalized) != 1 {
		return TagDeleteResult{}, apperror.New(apperror.KindInvalidRequest, "タグが正しくありません")
	}
	// タグが付いた資源を探す。
	resources, listError := tagService.tagStore.ListByTag(requestContext, userID, projectID, normalized[0])
	if listError != nil {
		return TagDeleteResult{}, listError
	}
	// 1 つも無ければ not_found。
	if len(resources) == 0 {
		return TagDeleteResult{}, apperror.New(apperror.KindNotFound, "そのタグの資源が見つかりません")
	}
	// 決めた順(ingress、service、deploy)に、1 つずつ、削除を受け付ける。
	result := TagDeleteResult{Accepted: []TagDeleteItem{}, Failed: []TagDeleteItem{}}
	for _, kind := range tagDeleteOrder {
		for _, resource := range resources {
			if resource.Kind != kind {
				continue
			}
			// 種類ごとの削除を呼ぶ。
			var deleteError error
			switch kind {
			case repository.TagKindIngress:
				deleteError = tagService.deleters.Ingress.Delete(requestContext, userID, projectID, resource.ID)
			case repository.TagKindService:
				deleteError = tagService.deleters.Service.Delete(requestContext, userID, resource.DeployID)
			case repository.TagKindDeploy:
				deleteError = tagService.deleters.Deploy.Delete(requestContext, userID, resource.ID)
			default:
				deleteError = tagService.deleters.Volume.Delete(requestContext, userID, projectID, resource.ID)
			}
			// 失敗したら、理由を残して、次へ進む。
			if deleteError != nil {
				result.Failed = append(result.Failed, TagDeleteItem{Kind: kind, ID: resource.ID, Message: deleteError.Error()})
				continue
			}
			// 受け付けた。
			result.Accepted = append(result.Accepted, TagDeleteItem{Kind: kind, ID: resource.ID})
		}
	}
	return result, nil
}

// DeployMetricService は、CPU・メモリなどのメトリクスを、deploy、プロジェクト、タグの単位で返す。
type DeployMetricService struct {
	metricStore MetricStore
	deployStore DeployStore
	now         func() time.Time
}

// NewDeployMetricService は、DeployMetricService を作る。
//
// 引数:
//   - metricStore: メトリクスの読み取り元。
//   - deployStore: deploy の持ち主の確認に使う(FindByID)。
func NewDeployMetricService(metricStore MetricStore, deployStore DeployStore) *DeployMetricService {
	// 部品と、現在時刻の関数を持たせて返す。
	return &DeployMetricService{metricStore: metricStore, deployStore: deployStore, now: time.Now}
}

// MetricRanges は、指定できる範囲の名前と、その長さ。
var MetricRanges = map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour}

// Series は、範囲の合計のメトリクスを、時刻の昇順で返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - scope: deploy 1 つ、プロジェクト、プロジェクトのタグのどれか。deploy の指定は、持ち主かを確かめる。
//   - rangeName: 範囲("1h"、"6h"、"24h"。空なら 1h)。
//
// 戻り値:
//   - 時刻ごとの合計。範囲の名前が不正なら invalid_request、deploy が無ければ not_found。
func (metricService *DeployMetricService) Series(requestContext context.Context, userID string, scope repository.MetricScope, rangeName string) ([]repository.MetricPoint, error) {
	// 範囲の名前を、長さにする(空は 1h)。
	if rangeName == "" {
		rangeName = "1h"
	}
	duration, known := MetricRanges[rangeName]
	if !known {
		return nil, apperror.New(apperror.KindInvalidRequest, "range は 1h、6h、24h のどれかです")
	}
	// deploy を指定したときは、持ち主のものかを確かめる(他人のものは not_found)。
	if scope.DeployID != "" {
		if _, findError := metricService.deployStore.FindByID(requestContext, userID, scope.DeployID); findError != nil {
			return nil, findError
		}
	}
	// 集計を読む。
	return metricService.metricStore.Series(requestContext, userID, scope, metricService.now().Add(-duration))
}
