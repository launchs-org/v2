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

	// MaxCPUMillicores は、deploy の CPU の合計の上限(ミリコア)。
	MaxCPUMillicores int `gorm:"column:max_cpu_millicores"`

	// MaxMemoryMiB は、deploy のメモリの合計の上限(MiB)。
	MaxMemoryMiB int `gorm:"column:max_memory_mib"`

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
	MaxCPUMillicores *int `gorm:"column:max_cpu_millicores"`
	MaxMemoryMiB     *int `gorm:"column:max_memory_mib"`
	MaxVolumes       *int `gorm:"column:max_volumes"`
	MaxVolumeTotalMB *int `gorm:"column:max_volume_total_mb"`
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
	MaxCPUMillicores int
	MaxMemoryMiB     int
	MaxVolumes       int
	MaxVolumeTotalMB int
}

// QuotaUsage は、ユーザーの使用量。数え方は docs/auth.md §8.2。
type QuotaUsage struct {
	// Projects は、プロジェクトの数(deleting 以外)。
	Projects int `gorm:"column:projects"`

	// Deploys は、deploy の数(deleting 以外)。
	Deploys int `gorm:"column:deploys"`

	// CPUMillicores は、replicas × スペックの CPU の合計(deleting 以外)。
	CPUMillicores int `gorm:"column:cpu_millicores"`

	// MemoryMiB は、replicas × スペックのメモリの合計(deleting 以外)。
	MemoryMiB int `gorm:"column:memory_mib"`

	// Volumes は、volume の数。
	Volumes int `gorm:"column:volumes"`

	// VolumeTotalMB は、volume の大きさの合計(size と pending の大きい方。MB)。
	VolumeTotalMB int `gorm:"column:volume_total_mb"`
}

// LimitsFor は、既定に、ユーザーごとの上書きを重ねて、そのユーザーの上限にする。
//
// 引数:
//   - override: ユーザーごとの上書き(行が無ければ、ゼロ値でよい)。
//
// 戻り値:
//   - そのユーザーに適用される上限。
func (defaults QuotaDefaults) LimitsFor(override AccountQuota) QuotaLimits {
	// 上書きがあればそれを、無ければ既定を使う。
	pick := func(overridden *int, fallback int) int {
		if overridden != nil {
			return *overridden
		}
		return fallback
	}
	return QuotaLimits{
		MaxProjects:      pick(override.MaxProjects, defaults.MaxProjects),
		MaxDeploys:       pick(override.MaxDeploys, defaults.MaxDeploys),
		MaxCPUMillicores: pick(override.MaxCPUMillicores, defaults.MaxCPUMillicores),
		MaxMemoryMiB:     pick(override.MaxMemoryMiB, defaults.MaxMemoryMiB),
		MaxVolumes:       pick(override.MaxVolumes, defaults.MaxVolumes),
		MaxVolumeTotalMB: pick(override.MaxVolumeTotalMB, defaults.MaxVolumeTotalMB),
	}
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

// checkCompute は、CPU とメモリの合計が、増える分を足しても、上限に収まるかを確かめる。
//
// 引数:
//   - usage: 使用量。
//   - addedReplicas: 増えるレプリカ数。
//   - spec: スペック。
//
// 戻り値:
//   - 超えるなら conflict。
func (limits QuotaLimits) checkCompute(usage QuotaUsage, spec DeploySpec, addedReplicas int) error {
	// 増える分を求めて、CPU、メモリの順に確かめる。
	addedCPU, addedMemory := addedReplicas*spec.CPUMillicores, addedReplicas*spec.MemoryMiB
	if usage.CPUMillicores+addedCPU > limits.MaxCPUMillicores {
		return exceededTotal("CPU ", "ミリコア", limits.MaxCPUMillicores, usage.CPUMillicores, addedCPU)
	}
	if usage.MemoryMiB+addedMemory > limits.MaxMemoryMiB {
		return exceededTotal("メモリ", "MiB", limits.MaxMemoryMiB, usage.MemoryMiB, addedMemory)
	}
	return nil
}

// CheckDeployCreate は、deploy を1つ作れるか(数と、CPU・メモリの合計)を確かめる。
//
// 引数:
//   - usage: 使用量。
//   - spec: 作る deploy のスペック。
//   - replicas: 作る deploy のレプリカ数。
//
// 戻り値:
//   - 超えるなら conflict。
func (limits QuotaLimits) CheckDeployCreate(usage QuotaUsage, spec DeploySpec, replicas int) error {
	// 数が上限に達していたら、作れない。
	if usage.Deploys >= limits.MaxDeploys {
		return exceededCount("deploy の数", limits.MaxDeploys, usage.Deploys)
	}
	// CPU とメモリの合計を確かめる。
	return limits.checkCompute(usage, spec, replicas)
}

// CheckScaleUp は、レプリカ数を増やせるか(CPU・メモリの合計)を確かめる。減らすときは、呼ばない。
//
// 引数:
//   - usage: 使用量(いまのレプリカ数で数えたもの)。
//   - spec: deploy のスペック。
//   - addedReplicas: 増えるレプリカ数(新しい数 - いまの数)。
//
// 戻り値:
//   - 超えるなら conflict。
func (limits QuotaLimits) CheckScaleUp(usage QuotaUsage, spec DeploySpec, addedReplicas int) error {
	// 増える分で、合計を確かめる。
	return limits.checkCompute(usage, spec, addedReplicas)
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
