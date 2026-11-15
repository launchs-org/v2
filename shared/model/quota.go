package model

import (
	"fmt"

	"launchs/shared/apperror"
)

// QuotaDefaults は、quota_defaults テーブルの1行。全ユーザーの、既定の上限。
type QuotaDefaults struct {
	// ID は、常に true(1行だけにするための主キー)。
	ID bool `gorm:"column:id;primaryKey"`

	// MaxProjects は、プロジェクトの数の上限。
	MaxProjects int `gorm:"column:max_projects"`

	// MaxDeploys は、deploy の数の上限。
	MaxDeploys int `gorm:"column:max_deploys"`

	// MaxVolumes は、volume の数の上限。
	MaxVolumes int `gorm:"column:max_volumes"`

	// MaxVolumeTotalMB は、volume の大きさの合計の上限(MB)。
	MaxVolumeTotalMB int `gorm:"column:max_volume_total_mb"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("quota_defaults")。
func (QuotaDefaults) TableName() string {
	return "quota_defaults"
}

// AccountQuota は、account_quotas テーブルの1行。ユーザーごとの上書き。nil の項目は、既定を使う。
type AccountQuota struct {
	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id;primaryKey"`

	// MaxProjects 以下は、QuotaDefaults と同じ意味の、上書きの値(nil なら既定)。
	MaxProjects      *int `gorm:"column:max_projects"`
	MaxDeploys       *int `gorm:"column:max_deploys"`
	MaxVolumes       *int `gorm:"column:max_volumes"`
	MaxVolumeTotalMB *int `gorm:"column:max_volume_total_mb"`
}

// AccountSpecQuota は、account_spec_quotas テーブルの1行。ユーザーごとの、スペックごとの deploy の数の上書き。
type AccountSpecQuota struct {
	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id;primaryKey"`

	// Spec は、スペックの名前。
	Spec string `gorm:"column:spec;primaryKey"`

	// MaxDeploys は、そのスペックの deploy の数の上限。
	MaxDeploys int `gorm:"column:max_deploys"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("account_spec_quotas")。
func (AccountSpecQuota) TableName() string {
	return "account_spec_quotas"
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("account_quotas")。
func (AccountQuota) TableName() string {
	return "account_quotas"
}

// QuotaLimits は、ユーザーに適用される上限(既定に、ユーザーごとの上書きを重ねたもの)。
type QuotaLimits struct {
	MaxProjects      int
	MaxDeploys       int
	MaxVolumes       int
	MaxVolumeTotalMB int

	// MaxDeploysBySpec は、スペックの名前ごとの、deploy の数の上限。
	MaxDeploysBySpec map[string]int
}

// QuotaUsage は、ユーザーの使用量。数え方は docs/auth.md §8.2。
type QuotaUsage struct {
	// Projects は、プロジェクトの数(deleting 以外)。
	Projects int

	// Deploys は、deploy の数(deleting 以外)。
	Deploys int

	// DeploysBySpec は、スペックの名前ごとの、deploy(deleting 以外)の数。replicas は数えない。
	DeploysBySpec map[string]int

	// Volumes は、volume の数。
	Volumes int

	// VolumeTotalMB は、volume の大きさの合計(size と pending の大きい方。MB)。
	VolumeTotalMB int
}

// LimitsFor は、既定に、ユーザーごとの上書きを重ねて、そのユーザーの上限にする。
//
// 引数:
//   - override: ユーザーごとの上書き(行が無ければ、ゼロ値でよい)。
//   - specs: スペックの一覧(スペックごとの既定の上限を持つ)。
//   - specOverrides: ユーザーごとの、スペックごとの deploy の数の上書き。
//
// 戻り値:
//   - そのユーザーに適用される上限。
func (defaults QuotaDefaults) LimitsFor(override AccountQuota, specs []DeploySpec, specOverrides []AccountSpecQuota) QuotaLimits {
	// 上書きがあればそれを、無ければ既定を使う。
	pick := func(overridden *int, fallback int) int {
		if overridden != nil {
			return *overridden
		}
		return fallback
	}
	// スペックごとに、既定の上限を入れてから、ユーザーの上書きで置き換える。
	maxDeploys := make(map[string]int, len(specs))
	for _, spec := range specs {
		maxDeploys[spec.Name] = spec.DefaultMaxDeploys
	}
	for _, specOverride := range specOverrides {
		maxDeploys[specOverride.Spec] = specOverride.MaxDeploys
	}
	return QuotaLimits{
		MaxProjects:      pick(override.MaxProjects, defaults.MaxProjects),
		MaxDeploys:       pick(override.MaxDeploys, defaults.MaxDeploys),
		MaxVolumes:       pick(override.MaxVolumes, defaults.MaxVolumes),
		MaxVolumeTotalMB: pick(override.MaxVolumeTotalMB, defaults.MaxVolumeTotalMB),
		MaxDeploysBySpec: maxDeploys,
	}
}

// AddDeploy は、deploy 1つ分を、使用量に足す(deploy の数と、スペックごとの deploy の数)。replicas は数えない。
//
// 引数:
//   - specName: deploy のスペックの名前。
func (usage *QuotaUsage) AddDeploy(specName string) {
	// 数を足し、スペックごとの数を、無ければ作って足す。
	usage.Deploys++
	if usage.DeploysBySpec == nil {
		usage.DeploysBySpec = map[string]int{}
	}
	usage.DeploysBySpec[specName]++
}

// AddVolume は、volume 1つ分を、使用量に足す(volume の数と、予約する大きさ)。
//
// 引数:
//   - volume: volume(拡張の pending があれば、大きい方が数えられる)。
func (usage *QuotaUsage) AddVolume(volume *Volume) {
	// 数と、予約する大きさを足す。
	usage.Volumes++
	usage.VolumeTotalMB += volume.ReservedMB()
}

// exceededCount は、数の上限に達したときの conflict のエラーを作る。
//
// 引数:
//   - label: 項目の名前。
//   - limit: 上限。
//   - used: 使用量。
//
// 戻り値:
//   - conflict のエラー。
func exceededCount(label string, limit int, used int) error {
	// どの項目が、いくつ中いくつかを伝える。
	return apperror.New(apperror.KindConflict, fmt.Sprintf("%sが上限(%d)に達しています(使用中 %d)", label, limit, used))
}

// exceededTotal は、合計の上限を超えるときの conflict のエラーを作る。
//
// 引数:
//   - label: 項目の名前。
//   - unit: 単位。
//   - limit: 上限。
//   - used: 使用量。
//   - adding: 追加する量。
//
// 戻り値:
//   - conflict のエラー。
func exceededTotal(label string, unit string, limit int, used int, adding int) error {
	// 上限、使用量、追加する量を伝える。
	return apperror.New(apperror.KindConflict, fmt.Sprintf("%sの合計が上限(%d%s)を超えます(使用中 %d、追加 %d)", label, limit, unit, used, adding))
}

// CheckProjectCreate は、プロジェクトを1つ作れるかを確かめる。
//
// 引数:
//   - usage: 使用量。
//
// 戻り値:
//   - 上限に達しているなら conflict。
func (limits QuotaLimits) CheckProjectCreate(usage QuotaUsage) error {
	// 数が上限に達していたら、作れない。
	if usage.Projects >= limits.MaxProjects {
		return exceededCount("プロジェクトの数", limits.MaxProjects, usage.Projects)
	}
	return nil
}

// CheckDeployCreate は、deploy を1つ作れるか(deploy の合計の数と、そのスペックの deploy の数)を確かめる。
//
// 引数:
//   - usage: 使用量。
//   - specName: 作る deploy のスペックの名前。
//
// 戻り値:
//   - 上限に達しているなら conflict(上限が無いスペックは、0 として扱う)。
func (limits QuotaLimits) CheckDeployCreate(usage QuotaUsage, specName string) error {
	// 合計の数が上限に達していたら、作れない。
	if usage.Deploys >= limits.MaxDeploys {
		return exceededCount("deploy の数", limits.MaxDeploys, usage.Deploys)
	}
	// そのスペックの数が上限に達していたら、作れない。
	if usage.DeploysBySpec[specName] >= limits.MaxDeploysBySpec[specName] {
		return exceededCount(specName+" の deploy の数", limits.MaxDeploysBySpec[specName], usage.DeploysBySpec[specName])
	}
	return nil
}

// CheckVolumeCreate は、volume を1つ作れるか(数と、大きさの合計)を確かめる。
//
// 引数:
//   - usage: 使用量。
//   - sizeMB: 作る volume の大きさ(MB)。
//
// 戻り値:
//   - 超えるなら conflict。
func (limits QuotaLimits) CheckVolumeCreate(usage QuotaUsage, sizeMB int) error {
	// 数が上限に達していたら、作れない。
	if usage.Volumes >= limits.MaxVolumes {
		return exceededCount("volume の数", limits.MaxVolumes, usage.Volumes)
	}
	// 大きさの合計を確かめる。
	return limits.CheckVolumeGrow(usage, sizeMB)
}

// CheckVolumeGrow は、volume の大きさの合計が、増える分を足しても、上限に収まるかを確かめる。
//
// 引数:
//   - usage: 使用量。
//   - addedMB: 増える大きさ(MB)。
//
// 戻り値:
//   - 超えるなら conflict。
func (limits QuotaLimits) CheckVolumeGrow(usage QuotaUsage, addedMB int) error {
	// 合計が上限を超えるなら、増やせない。
	if usage.VolumeTotalMB+addedMB > limits.MaxVolumeTotalMB {
		return exceededTotal("volume の大きさ", "MB", limits.MaxVolumeTotalMB, usage.VolumeTotalMB, addedMB)
	}
	return nil
}

// MaxQuotaValue は、管理者が上書きできる上限の値の最大(桁の間違いを防ぐための、大きな上限)。
const MaxQuotaValue = 1_000_000

// QuotaOverrides は、管理者が設定する、ユーザーごとの上限の上書き一式。nil(またはキーが無い)の項目は、既定を使う。
//
// 設定は、一式の置き換え: 指定しなかった項目は、以前の上書きが消えて、既定に戻る。
type QuotaOverrides struct {
	// MaxProjects 以下は、QuotaDefaults と同じ意味の、上書きの値(nil なら既定)。
	MaxProjects      *int
	MaxDeploys       *int
	MaxVolumes       *int
	MaxVolumeTotalMB *int

	// MaxDeploysBySpec は、スペックの名前ごとの、deploy の数の上書き(キーが無いスペックは、スペックの既定)。
	MaxDeploysBySpec map[string]int
}

// Validate は、上書きの値が正しいか(0〜MaxQuotaValue、存在するスペックの名前)を確かめる。
//
// 引数:
//   - knownSpecs: 存在するスペックの名前。
//
// 戻り値:
//   - 範囲外の値、存在しないスペックがあれば invalid_request(どの項目かを、メッセージに入れる)。
func (overrides QuotaOverrides) Validate(knownSpecs []string) error {
	// 項目ごとの値を、名前と一緒に確かめる。
	for name, value := range map[string]*int{"projects": overrides.MaxProjects, "deploys": overrides.MaxDeploys, "volumes": overrides.MaxVolumes, "volumeTotalMB": overrides.MaxVolumeTotalMB} {
		if value != nil && (*value < 0 || *value > MaxQuotaValue) {
			return apperror.New(apperror.KindInvalidRequest, fmt.Sprintf("%s は 0〜%d でなければなりません", name, MaxQuotaValue))
		}
	}
	// スペックごとの値と、スペックの名前を確かめる。
	known := make(map[string]bool, len(knownSpecs))
	for _, specName := range knownSpecs {
		known[specName] = true
	}
	for specName, value := range overrides.MaxDeploysBySpec {
		if !known[specName] {
			return apperror.New(apperror.KindInvalidRequest, fmt.Sprintf("deploysBySpec の %q は、存在しないスペックです", specName))
		}
		if value < 0 || value > MaxQuotaValue {
			return apperror.New(apperror.KindInvalidRequest, fmt.Sprintf("deploysBySpec の %q は 0〜%d でなければなりません", specName, MaxQuotaValue))
		}
	}
	return nil
}

// AccountQuota は、上書きのうち、account_quotas の1行にあたる部分を返す。
//
// 引数:
//   - userID: 持ち主のユーザー ID。
//
// 戻り値:
//   - account_quotas の行(スペックごとの上書きは、含まない)。
func (overrides QuotaOverrides) AccountQuota(userID string) AccountQuota {
	// 項目をそのまま写す。
	return AccountQuota{UserID: userID, MaxProjects: overrides.MaxProjects, MaxDeploys: overrides.MaxDeploys, MaxVolumes: overrides.MaxVolumes, MaxVolumeTotalMB: overrides.MaxVolumeTotalMB}
}

// QuotaOverridesOf は、保存されている行から、上書き一式を作る。
//
// 引数:
//   - row: account_quotas の行(無ければ、ゼロ値)。
//   - specRows: account_spec_quotas の行。
//
// 戻り値:
//   - 上書き一式。
func QuotaOverridesOf(row AccountQuota, specRows []AccountSpecQuota) QuotaOverrides {
	// 行の項目を写し、スペックごとの行を、名前をキーにした表にする。
	overrides := QuotaOverrides{MaxProjects: row.MaxProjects, MaxDeploys: row.MaxDeploys, MaxVolumes: row.MaxVolumes, MaxVolumeTotalMB: row.MaxVolumeTotalMB, MaxDeploysBySpec: map[string]int{}}
	for _, specRow := range specRows {
		overrides.MaxDeploysBySpec[specRow.Spec] = specRow.MaxDeploys
	}
	return overrides
}
