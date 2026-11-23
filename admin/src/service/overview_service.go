// Package service は、管理コンテナのユースケースを置く。
package service

import (
	"context"

	"launchs/admin/src/repository"
)

// OverviewReader は、全体の概要の読み出し先(repository.OverviewRepository が満たす)。
type OverviewReader interface {
	// Get は、全体の概要を返す。
	Get(requestContext context.Context) (repository.Overview, error)
}

// OverviewService は、全体の概要のユースケース。
type OverviewService struct {
	overviewReader OverviewReader
}

// NewOverviewService は、OverviewService を作る。
//
// 引数:
//   - overviewReader: 概要の読み出し先。
//
// 戻り値:
//   - 作成した OverviewService。
func NewOverviewService(overviewReader OverviewReader) *OverviewService {
	// 依存するものを持たせて返す。
	return &OverviewService{overviewReader: overviewReader}
}

// Get は、全体の概要を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - 概要。エラーは、読み出し先のもの。
func (overviewService *OverviewService) Get(requestContext context.Context) (repository.Overview, error) {
	// 読み出し先に任せる。
	return overviewService.overviewReader.Get(requestContext)
}
