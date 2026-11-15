package service

import (
	"context"
	"regexp"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// QuotaStore は、QuotaService が必要とする、上限と使用量の読み取り。
//
// 実体は shared/repository の QuotaRepository。テストでは fake に差し替える。
type QuotaStore interface {
	Get(requestContext context.Context, userID string) (model.QuotaLimits, model.QuotaUsage, error)

	// GetOverrides は、管理者が設定した、ユーザーごとの上書きを返す。
	GetOverrides(requestContext context.Context, userID string) (model.QuotaOverrides, error)

	// SetOverrides は、ユーザーごとの上書きを、一式で置き換える。
	SetOverrides(requestContext context.Context, userID string, overrides model.QuotaOverrides) error
}

// targetUserIDPattern は、管理者が指定できる、対象のユーザー ID の形(英数字とハイフン。長さの上限つき)。
// backend は、ユーザーが実在するかを知らない(auth が持っている)ので、形だけを確かめる。
var targetUserIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,100}$`)

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

// AdminQuotaReport は、管理者が見る、あるユーザーの上限・使用量と、設定されている上書き。
type AdminQuotaReport struct {
	// Report は、そのユーザーに適用される上限と、使用量。
	Report QuotaReport

	// Overrides は、管理者が設定した上書き(設定が無い項目は、既定)。
	Overrides model.QuotaOverrides
}

// GetForUser は、指定したユーザーの上限と使用量と、上書きを返す(管理者用)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - targetUserID: 対象のユーザー ID。
//
// 戻り値:
//   - 上限・使用量・上書き。ID の形が不正なら invalid_request。
func (quotaService *QuotaService) GetForUser(requestContext context.Context, targetUserID string) (AdminQuotaReport, error) {
	// ID の形を確かめる。
	if !targetUserIDPattern.MatchString(targetUserID) {
		return AdminQuotaReport{}, apperror.New(apperror.KindInvalidRequest, "userId の形が正しくありません")
	}
	// 上限と使用量、上書きを読む。
	report, reportError := quotaService.Get(requestContext, targetUserID)
	if reportError != nil {
		return AdminQuotaReport{}, reportError
	}
	overrides, overridesError := quotaService.quotaStore.GetOverrides(requestContext, targetUserID)
	if overridesError != nil {
		return AdminQuotaReport{}, overridesError
	}
	return AdminQuotaReport{Report: report, Overrides: overrides}, nil
}

// SetOverrides は、指定したユーザーの上限の上書きを、一式で置き換えて、結果を返す(管理者用)。
//
// 指定しなかった項目は、以前の上書きが消えて、既定に戻る。既にある使用量が、新しい上限を超えていても、消さない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - targetUserID: 対象のユーザー ID。
//   - overrides: 新しい上書き一式。
//
// 戻り値:
//   - 置き換えた後の上限・使用量・上書き。ID の形や値が不正なら invalid_request。
func (quotaService *QuotaService) SetOverrides(requestContext context.Context, targetUserID string, overrides model.QuotaOverrides) (AdminQuotaReport, error) {
	// ID の形を確かめる(値の確認は、保存と同じトランザクションで行う)。
	if !targetUserIDPattern.MatchString(targetUserID) {
		return AdminQuotaReport{}, apperror.New(apperror.KindInvalidRequest, "userId の形が正しくありません")
	}
	// 置き換える。
	if setError := quotaService.quotaStore.SetOverrides(requestContext, targetUserID, overrides); setError != nil {
		return AdminQuotaReport{}, setError
	}
	// 置き換えた後の状態を返す。
	return quotaService.GetForUser(requestContext, targetUserID)
}
