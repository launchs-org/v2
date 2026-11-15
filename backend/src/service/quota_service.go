package service

import (
	"context"

	"launchs/shared/model"
)

// QuotaStore は、QuotaService が必要とする、上限と使用量の読み取り。
//
// 実体は shared/repository の QuotaRepository。テストでは fake に差し替える。
type QuotaStore interface {
	Get(requestContext context.Context, userID string) (model.QuotaLimits, model.QuotaUsage, error)
}

// QuotaReport は、ユーザーの上限と使用量。
type QuotaReport struct {
	// Limits は、ユーザーに適用される上限。
	Limits model.QuotaLimits

	// Usage は、いまの使用量。
	Usage model.QuotaUsage

	// MaxVolumeSizeMB は、volume 1つの大きさの上限(MB。環境変数 VOLUME_MAX_SIZE_MB)。
	MaxVolumeSizeMB int
}

// QuotaService は、ユーザーの上限と使用量を見せる。作成・拡張の判定は、各リソースの repository が行う。
type QuotaService struct {
	quotaStore      QuotaStore
	maxVolumeSizeMB int
}

// NewQuotaService は、QuotaService を作る。
//
// 引数:
//   - quotaStore: 上限と使用量の読み取り。
//   - maxVolumeSizeMB: volume 1つの大きさの上限(MB)。
//
// 戻り値:
//   - 作成した QuotaService。
func NewQuotaService(quotaStore QuotaStore, maxVolumeSizeMB int) *QuotaService {
	// 依存するものを持たせて返す。
	return &QuotaService{quotaStore: quotaStore, maxVolumeSizeMB: maxVolumeSizeMB}
}

// Get は、ユーザーの上限と使用量を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//
// 戻り値:
//   - 上限と使用量。データベースのエラーは internal_error。
func (quotaService *QuotaService) Get(requestContext context.Context, userID string) (QuotaReport, error) {
	// 上限と使用量を読む。
	limits, usage, getError := quotaService.quotaStore.Get(requestContext, userID)
	if getError != nil {
		return QuotaReport{}, getError
	}
	return QuotaReport{Limits: limits, Usage: usage, MaxVolumeSizeMB: quotaService.maxVolumeSizeMB}, nil
}
