package controller

import (
	"context"
	"net/http"
	"net/url"

	"launchs/backend/src/middlewares"
	"launchs/backend/src/service"
	"launchs/shared/repository"

	"github.com/labstack/echo/v5"
)

// DeployTagUseCase は、タグのコントローラーが必要とする、ビジネスルールの操作。
type DeployTagUseCase interface {
	SetTags(requestContext context.Context, kind string, userID string, key string, tags []string) ([]string, error)
	ListTags(requestContext context.Context, userID string, projectID string) ([]repository.DeployTagCount, error)
	DeleteByTag(requestContext context.Context, userID string, projectID string, tag string) (service.TagDeleteResult, error)
}

// MetricUseCase は、メトリクスのコントローラーが必要とする、ビジネスルールの操作。
type MetricUseCase interface {
	Series(requestContext context.Context, userID string, scope repository.MetricScope, rangeName string) ([]repository.MetricPoint, error)
}

// DeployTagController は、タグとメトリクスの HTTP のハンドラー。
type DeployTagController struct {
	tagUseCase    DeployTagUseCase
	metricUseCase MetricUseCase
}

// NewDeployTagController は、DeployTagController を作る。
//
// 引数:
//   - tagUseCase: タグのビジネスルール。
//   - metricUseCase: メトリクスのビジネスルール。
func NewDeployTagController(tagUseCase DeployTagUseCase, metricUseCase MetricUseCase) *DeployTagController {
	// 部品を持たせて返す。
	return &DeployTagController{tagUseCase: tagUseCase, metricUseCase: metricUseCase}
}

// SetTagsRequest は、タグの置き換えのリクエストのボディ。
type SetTagsRequest struct {
	// Tags は、新しいタグの全て(空の配列は、全て外す)。nil(未指定)は許さない。
	Tags []string `json:"tags" validate:"required"`
}

// setTags は、資源のタグを置き換える(4 つのエンドポイントの共通の処理)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//   - kind: 資源の種類(repository.TagKind...)。
//   - key: 資源の ID(service は、持ち主の deploy の ID)。
//
// 戻り値:
//   - ボディやタグが不正なら invalid_request、資源が無ければ not_found、未認証なら unauthorized。
func (tagController *DeployTagController) setTags(echoContext *echo.Context, kind string, key string) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ボディを読み込み、検証する。
	var request SetTagsRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 置き換える。
	tags, setError := tagController.tagUseCase.SetTags(echoContext.Request().Context(), kind, userID, key, request.Tags)
	if setError != nil {
		return setError
	}
	// 資源の種類と、保存したタグを返す。
	return echoContext.JSON(http.StatusOK, map[string]any{"kind": kind, "id": key, "tags": tags})
}

// SetTags は、deploy のタグを置き換える(PUT /deploy/:deployId/tags)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
func (tagController *DeployTagController) SetTags(echoContext *echo.Context) error {
	// deploy のタグを置き換える。
	return tagController.setTags(echoContext, repository.TagKindDeploy, echoContext.Param("deployId"))
}

// SetServiceTags は、service のタグを置き換える(PUT /deploy/:deployId/service/tags)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
func (tagController *DeployTagController) SetServiceTags(echoContext *echo.Context) error {
	// service のタグを置き換える(service は、deploy の ID で指す)。
	return tagController.setTags(echoContext, repository.TagKindService, echoContext.Param("deployId"))
}

// SetVolumeTags は、volume のタグを置き換える(PUT /projects/:projectId/volumes/:volumeId/tags)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
func (tagController *DeployTagController) SetVolumeTags(echoContext *echo.Context) error {
	// volume のタグを置き換える。
	return tagController.setTags(echoContext, repository.TagKindVolume, echoContext.Param("volumeId"))
}

// SetIngressTags は、ingress のタグを置き換える(PUT /projects/:projectId/ingresses/:ingressId/tags)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
func (tagController *DeployTagController) SetIngressTags(echoContext *echo.Context) error {
	// ingress のタグを置き換える。
	return tagController.setTags(echoContext, repository.TagKindIngress, echoContext.Param("ingressId"))
}

// ListTags は、プロジェクトのタグの一覧を返す(GET /projects/:projectId/tags)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 未認証なら unauthorized。
func (tagController *DeployTagController) ListTags(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 一覧を読む。
	counts, listError := tagController.tagUseCase.ListTags(echoContext.Request().Context(), userID, echoContext.Param("projectId"))
	if listError != nil {
		return listError
	}
	// 応答の形にする。
	items := make([]map[string]any, 0, len(counts))
	for _, count := range counts {
		items = append(items, map[string]any{"tag": count.Tag, "deployCount": count.DeployCount, "volumeCount": count.VolumeCount, "serviceCount": count.ServiceCount, "ingressCount": count.IngressCount})
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"tags": items})
}

// DeleteByTag は、タグが付いた資源(ingress、service、deploy、volume)の削除を、一斉に受け付ける(DELETE /projects/:projectId/tags/:tag/deploys)。
//
// 削除の完了は待たない。受け付けた deploy と、受け付けられなかった deploy(と、理由)を返す。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// volume は、データを守るため、削除しない。
//
// 戻り値:
//   - タグが不正なら invalid_request、そのタグの deploy が無ければ not_found、未認証なら unauthorized。
func (tagController *DeployTagController) DeleteByTag(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 一斉に削除を受け付ける。
	result, deleteError := tagController.tagUseCase.DeleteByTag(echoContext.Request().Context(), userID, echoContext.Param("projectId"), unescapedTagParam(echoContext))
	if deleteError != nil {
		return deleteError
	}
	// 受け付けた分と、失敗した分を、応答の形にする。
	toItems := func(items []service.TagDeleteItem) []map[string]string {
		responses := make([]map[string]string, 0, len(items))
		for _, item := range items {
			responses = append(responses, map[string]string{"kind": item.Kind, "id": item.ID, "message": item.Message})
		}
		return responses
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"accepted": toItems(result.Accepted), "failed": toItems(result.Failed)})
}

// metricPointResponse は、メトリクスの 1 点(ある時刻の合計)。測れなかった値は null。
func metricPointResponse(point repository.MetricPoint) map[string]any {
	// 時刻は、JST ではなく UTC の RFC3339 で返す(画面が、自分の時間帯に直す)。
	return map[string]any{
		"at": point.At.UTC().Format("2006-01-02T15:04:05Z"), "cpuMillicores": point.CPUMillicores, "memoryBytes": point.MemoryBytes,
		"pods": point.Pods, "readyPods": point.ReadyPods, "restarts": point.Restarts,
	}
}

// metrics は、範囲の合計のメトリクスを返す(3 つのエンドポイントの共通の処理)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//   - scope: 集計する範囲。
//
// 戻り値:
//   - range が不正なら invalid_request、deploy が無ければ not_found、未認証なら unauthorized。
func (tagController *DeployTagController) metrics(echoContext *echo.Context, scope repository.MetricScope) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 範囲の合計を読む。
	rangeName := echoContext.QueryParam("range")
	points, seriesError := tagController.metricUseCase.Series(echoContext.Request().Context(), userID, scope, rangeName)
	if seriesError != nil {
		return seriesError
	}
	// 応答の形にする。
	items := make([]map[string]any, 0, len(points))
	for _, point := range points {
		items = append(items, metricPointResponse(point))
	}
	if rangeName == "" {
		rangeName = "1h"
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"range": rangeName, "points": items})
}

// DeployMetrics は、deploy 1 つのメトリクスを返す(GET /deploy/:deployId/metrics?range=)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
func (tagController *DeployTagController) DeployMetrics(echoContext *echo.Context) error {
	// deploy の範囲で集計する。
	return tagController.metrics(echoContext, repository.MetricScope{DeployID: echoContext.Param("deployId")})
}

// ProjectMetrics は、プロジェクト全体のメトリクスを返す(GET /projects/:projectId/metrics?range=)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
func (tagController *DeployTagController) ProjectMetrics(echoContext *echo.Context) error {
	// プロジェクトの範囲で集計する。
	return tagController.metrics(echoContext, repository.MetricScope{ProjectID: echoContext.Param("projectId")})
}

// TagMetrics は、プロジェクトの中のタグのメトリクスを返す(GET /projects/:projectId/tags/:tag/metrics?range=)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
func (tagController *DeployTagController) TagMetrics(echoContext *echo.Context) error {
	// タグの範囲で集計する。
	return tagController.metrics(echoContext, repository.MetricScope{ProjectID: echoContext.Param("projectId"), Tag: unescapedTagParam(echoContext)})
}

// unescapedTagParam は、パスの :tag を、URL のエスケープ(例: %3A)を戻した文字列で返す。
//
// Echo は、エスケープされたパスで経路を探すと、パラメーターもエスケープのまま渡す。
// 画面は、タグを encodeURIComponent で送る(template:xxx の : が %3A になる)ので、ここで戻す。
//
// 引数:
//   - echoContext: リクエストの文脈。
//
// 戻り値:
//   - 戻したタグ。戻せない形のときは、元の文字列(検査で、不正として断られる)。
func unescapedTagParam(echoContext *echo.Context) string {
	// パラメーターの生の値を取る。
	rawTag := echoContext.Param("tag")
	// エスケープを戻す。
	decodedTag, decodeError := url.PathUnescape(rawTag)
	// 戻せなければ、そのまま返す。
	if decodeError != nil {
		return rawTag
	}
	return decodedTag
}
