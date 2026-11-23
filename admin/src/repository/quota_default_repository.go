package repository

import (
	"context"
	"sort"

	"gorm.io/gorm"

	"launchs/shared/apperror"
)

// QuotaDefaults は、全ユーザーの上限の既定(全体のクォータ)。
type QuotaDefaults struct {
	// Projects、Deploys、Volumes、VolumeTotalMB は、既定の上限。
	Projects      int `json:"projects"`
	Deploys       int `json:"deploys"`
	Volumes       int `json:"volumes"`
	VolumeTotalMB int `json:"volumeTotalMB"`

	// DeploysBySpec は、スペックの名前ごとの、deploy の数の既定の上限。
	DeploysBySpec map[string]int `json:"deploysBySpec"`
}

// QuotaDefaultRepository は、上限の既定(quota_defaults と deploy_specs.default_max_deploys)を読み書きする。
type QuotaDefaultRepository struct {
	database *gorm.DB
}

// NewQuotaDefaultRepository は、QuotaDefaultRepository を作る。
//
// 引数:
//   - database: app データベースの接続。
//
// 戻り値:
//   - 作成した QuotaDefaultRepository。
func NewQuotaDefaultRepository(database *gorm.DB) *QuotaDefaultRepository {
	// 接続を持たせて返す。
	return &QuotaDefaultRepository{database: database}
}

// read は、既定を読む(トランザクションの中でも使う)。
//
// 引数:
//   - database: 読む接続(トランザクションでもよい)。
//
// 戻り値:
//   - 既定。データベースのエラーは internal_error。
func (quotaDefaultRepository *QuotaDefaultRepository) read(database *gorm.DB) (QuotaDefaults, error) {
	// 全体の既定(1 行)を読む。
	var row struct {
		MaxProjects      int `gorm:"column:max_projects"`
		MaxDeploys       int `gorm:"column:max_deploys"`
		MaxVolumes       int `gorm:"column:max_volumes"`
		MaxVolumeTotalMB int `gorm:"column:max_volume_total_mb"`
	}
	if findError := database.Table("quota_defaults").Take(&row).Error; findError != nil {
		return QuotaDefaults{}, apperror.Wrap(apperror.KindInternal, "クォータの既定を取得できませんでした", findError)
	}
	// スペックごとの既定を読む。
	var specs []struct {
		Name string `gorm:"column:name"`
		Max  int    `gorm:"column:default_max_deploys"`
	}
	if findError := database.Table("deploy_specs").Select("name, default_max_deploys").Find(&specs).Error; findError != nil {
		return QuotaDefaults{}, apperror.Wrap(apperror.KindInternal, "スペックを取得できませんでした", findError)
	}
	defaults := QuotaDefaults{Projects: row.MaxProjects, Deploys: row.MaxDeploys, Volumes: row.MaxVolumes, VolumeTotalMB: row.MaxVolumeTotalMB, DeploysBySpec: map[string]int{}}
	for _, spec := range specs {
		defaults.DeploysBySpec[spec.Name] = spec.Max
	}
	return defaults, nil
}

// Get は、上限の既定を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - 既定。データベースのエラーは internal_error。
func (quotaDefaultRepository *QuotaDefaultRepository) Get(requestContext context.Context) (QuotaDefaults, error) {
	// context を付けた接続で読む。
	return quotaDefaultRepository.read(quotaDefaultRepository.database.WithContext(requestContext))
}

// Set は、上限の既定を置き換える(1 つのトランザクションで、全体の既定と、スペックごとの既定を更新する)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - defaults: 新しい既定。スペックは、存在する名前だけ(無い名前は invalid_request)。含まないスペックは、変えない。
//
// 戻り値:
//   - 置き換えた後の既定。データベースのエラーは internal_error。
func (quotaDefaultRepository *QuotaDefaultRepository) Set(requestContext context.Context, defaults QuotaDefaults) (QuotaDefaults, error) {
	var result QuotaDefaults
	transactionError := quotaDefaultRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 存在するスペックの名前を確かめる(順序を決めて、更新する)。
		existing, readError := quotaDefaultRepository.read(transaction)
		if readError != nil {
			return readError
		}
		names := make([]string, 0, len(defaults.DeploysBySpec))
		for name := range defaults.DeploysBySpec {
			if _, found := existing.DeploysBySpec[name]; !found {
				return apperror.New(apperror.KindInvalidRequest, "存在しないスペックです: "+name)
			}
			names = append(names, name)
		}
		sort.Strings(names)
		// 全体の既定を更新する。
		updateError := transaction.Exec("UPDATE quota_defaults SET max_projects = ?, max_deploys = ?, max_volumes = ?, max_volume_total_mb = ?",
			defaults.Projects, defaults.Deploys, defaults.Volumes, defaults.VolumeTotalMB).Error
		if updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "クォータの既定を更新できませんでした", updateError)
		}
		// スペックごとの既定を更新する。
		for _, name := range names {
			if specError := transaction.Exec("UPDATE deploy_specs SET default_max_deploys = ? WHERE name = ?", defaults.DeploysBySpec[name], name).Error; specError != nil {
				return apperror.Wrap(apperror.KindInternal, "スペックの既定を更新できませんでした", specError)
			}
		}
		// 置き換えた後の値を読む。
		var finalError error
		result, finalError = quotaDefaultRepository.read(transaction)
		return finalError
	})
	if transactionError != nil {
		return QuotaDefaults{}, transactionError
	}
	return result, nil
}
