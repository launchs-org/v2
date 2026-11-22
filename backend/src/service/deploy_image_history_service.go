package service

import (
	"context"

	"launchs/shared/model"
)

// MaxImageHistoryItems は、イメージの更新履歴を、1回に返す最大の件数。
const MaxImageHistoryItems = 50

// DeployImageHistoryStore は、DeployImageHistoryService が必要とする、履歴の読み取り。
//
// 実体は shared/repository の DeployImageHistoryRepository。テストでは fake に差し替える。
type DeployImageHistoryStore interface {
	// List は、deploy のイメージの更新履歴を、新しい順に、最大 limit 件返す。
	List(requestContext context.Context, deployID string, limit int) ([]model.DeployImageHistory, error)
}

// DeployImageHistoryService は、deploy のイメージの更新履歴の取得のビジネスルール。
type DeployImageHistoryService struct {
	deployStore  DeployStore
	historyStore DeployImageHistoryStore
}

// NewDeployImageHistoryService は、DeployImageHistoryService を作る。
//
// 引数:
//   - deployStore: deploy の取得(持ち主の確認に使う)。
//   - historyStore: 履歴の読み取り。
//
// 戻り値:
//   - 作成した DeployImageHistoryService。
func NewDeployImageHistoryService(deployStore DeployStore, historyStore DeployImageHistoryStore) *DeployImageHistoryService {
	// 依存するものを持たせて返す。
	return &DeployImageHistoryService{deployStore: deployStore, historyStore: historyStore}
}

// List は、ユーザーの deploy のイメージの更新履歴を、新しい順に、最大 MaxImageHistoryItems 件返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 操作するユーザーの ID。
//   - deployID: 対象の deploy の ID。
//
// 戻り値:
//   - 履歴(新しい順)。
//   - deploy が無い、または他のユーザーのものなら not_found。
func (deployImageHistoryService *DeployImageHistoryService) List(requestContext context.Context, userID string, deployID string) ([]model.DeployImageHistory, error) {
	// 先に、ユーザーの deploy であることを確かめる。
	if _, findError := deployImageHistoryService.deployStore.FindByID(requestContext, userID, deployID); findError != nil {
		return nil, findError
	}
	// 履歴を、上限の件数まで読んで返す。
	return deployImageHistoryService.historyStore.List(requestContext, deployID, MaxImageHistoryItems)
}
