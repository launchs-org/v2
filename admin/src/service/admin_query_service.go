package service

import (
	"context"
	"sort"

	"launchs/admin/src/repository"
	"launchs/shared/apperror"
	"launchs/shared/pagination"
)

// maxQuotaValue は、クォータの値の上限(backend の上書きと同じ範囲)。
const maxQuotaValue = 1000000

// SettingItem は、画面に見せる設定 1 つ。
type SettingItem struct {
	// Name は、環境変数の名前。
	Name string `json:"name"`

	// Value は、値。未設定なら空(既定で動いている)。
	Value string `json:"value"`
}

// AdminQueryService は、管理画面の、全体の一覧・既定のクォータ・設定のユースケース。
type AdminQueryService struct {
	queryRepository *repository.AdminQueryRepository
	quotaRepository *repository.QuotaDefaultRepository
	settings        map[string]string
	settingNames    []string
}

// NewAdminQueryService は、AdminQueryService を作る。
//
// 引数:
//   - queryRepository: deploy とアクセスログの読み出し。
//   - quotaRepository: 上限の既定の読み書き。
//   - settings: 見せる設定(名前 → 値)。
//   - settingNames: 見せる設定の名前(表示の順序)。
//
// 戻り値:
//   - 作成した AdminQueryService。
func NewAdminQueryService(queryRepository *repository.AdminQueryRepository, quotaRepository *repository.QuotaDefaultRepository, settings map[string]string, settingNames []string) *AdminQueryService {
	// 依存するものを持たせて返す。
	return &AdminQueryService{queryRepository: queryRepository, quotaRepository: quotaRepository, settings: settings, settingNames: settingNames}
}

// ListDeploys は、全ユーザーの deploy の一覧を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 件数の上限。
//   - after: 前のページの最後の位置。
//   - filter: 絞り込み。
//
// 戻り値:
//   - 一覧と、次のページの位置。
func (adminQueryService *AdminQueryService) ListDeploys(requestContext context.Context, limit int, after *pagination.Position, filter repository.AdminDeployFilter) ([]repository.AdminDeploy, *pagination.Position, error) {
	// 読み出しに任せる。
	return adminQueryService.queryRepository.ListDeploys(requestContext, limit, after, filter)
}

// ListAccessLogs は、全 ingress のアクセスログを返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 件数の上限。
//   - after: 前のページの最後の位置。
//   - filter: 絞り込み。
//
// 戻り値:
//   - 一覧と、次のページの位置。
func (adminQueryService *AdminQueryService) ListAccessLogs(requestContext context.Context, limit int, after *pagination.Position, filter repository.AdminAccessLogFilter) ([]repository.AdminAccessLog, *pagination.Position, error) {
	// 読み出しに任せる。
	return adminQueryService.queryRepository.ListAccessLogs(requestContext, limit, after, filter)
}

// GetQuotaDefaults は、全体のクォータ(既定)を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - 既定。
func (adminQueryService *AdminQueryService) GetQuotaDefaults(requestContext context.Context) (repository.QuotaDefaults, error) {
	// 読み出しに任せる。
	return adminQueryService.quotaRepository.Get(requestContext)
}

// SetQuotaDefaults は、全体のクォータ(既定)を置き換える。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - defaults: 新しい既定(値は 0〜1000000)。
//
// 戻り値:
//   - 置き換えた後の既定。値が範囲外なら invalid_request。
func (adminQueryService *AdminQueryService) SetQuotaDefaults(requestContext context.Context, defaults repository.QuotaDefaults) (repository.QuotaDefaults, error) {
	// 全ての値が範囲に入っているかを確かめる。
	values := []int{defaults.Projects, defaults.Deploys, defaults.Volumes, defaults.VolumeTotalMB}
	for _, limit := range defaults.DeploysBySpec {
		values = append(values, limit)
	}
	for _, value := range values {
		if value < 0 || value > maxQuotaValue {
			return repository.QuotaDefaults{}, apperror.New(apperror.KindInvalidRequest, "上限は 0〜1000000 の整数で指定してください")
		}
	}
	// 保存に任せる(既存の上限を超えたデータは、消さない)。
	return adminQueryService.quotaRepository.Set(requestContext, defaults)
}

// Settings は、画面に見せる、システムの設定の一覧を返す(秘密を含まない固定の名前だけ)。
//
// 戻り値:
//   - 設定の一覧(決まった順序)。
func (adminQueryService *AdminQueryService) Settings() []SettingItem {
	// 名前の順序を、複製してから並べる(元の一覧は変えない)。
	names := append([]string(nil), adminQueryService.settingNames...)
	sort.Strings(names)
	items := make([]SettingItem, 0, len(names))
	for _, name := range names {
		items = append(items, SettingItem{Name: name, Value: adminQueryService.settings[name]})
	}
	return items
}
