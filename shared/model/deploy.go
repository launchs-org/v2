package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	// DeployIDPrefix は、deploy の ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。
	DeployIDPrefix = "deploy-"

	// DeployStatusPending は、作成を受け付けて、まだ起動が確認できていない deploy。
	DeployStatusPending = "pending"

	// DeployStatusActive は、稼働中の deploy。
	DeployStatusActive = "active"

	// DeployStatusApplying は、設定の反映(コンテナの再起動)が進んでいる deploy。
	DeployStatusApplying = "applying"

	// DeployStatusFailed は、起動または反映に失敗した deploy。
	DeployStatusFailed = "failed"

	// DeployStatusScaledZero は、replicas が 0 の deploy。
	DeployStatusScaledZero = "scaled_zero"

	// DeployStatusDeleting は、削除を受け付けて、実体を削除している最中の deploy。
	DeployStatusDeleting = "deleting"

	// DeployStatusDeleteFailed は、削除に失敗して止まっている deploy。もう一度の削除を受け付ける。
	DeployStatusDeleteFailed = "delete_failed"
)

// Deploy は、deploys テーブルの1行。k8s の Deployment 1つに対応する。
type Deploy struct {
	// ID は、deploy の ID("deploy-" + UUID v4)。k8s の Deployment の名前にも使う。
	ID string `gorm:"column:id;primaryKey"`

	// ProjectID は、属するプロジェクトの ID。k8s の namespace の名前でもある。
	ProjectID string `gorm:"column:project_id"`

	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id"`

	// ImageRef は、起動するイメージ。
	ImageRef string `gorm:"column:image_ref"`

	// Spec は、スペックの名前(deploy_specs.name)。作成後は変えられない。
	Spec string `gorm:"column:spec"`

	// Replicas は、希望するレプリカ数。
	Replicas int `gorm:"column:replicas"`

	// Status は、deploy の状態(DeployStatus... のどれか)。
	Status string `gorm:"column:status"`

	// Env は、現在コンテナに反映されている環境変数(JSON のオブジェクト)。
	Env datatypes.JSON `gorm:"column:env"`

	// Command は、現在の起動コマンド。nil ならイメージのデフォルト。
	Command *string `gorm:"column:command"`

	// Args は、現在の起動引数(JSON の配列)。nil ならイメージのデフォルト。
	Args datatypes.JSON `gorm:"column:args"`

	// PendingEnv は、まだ適用していない環境変数。変更が無ければ nil。
	PendingEnv datatypes.JSON `gorm:"column:pending_env"`

	// PendingCommand は、まだ適用していない起動コマンド。
	PendingCommand *string `gorm:"column:pending_command"`

	// PendingArgs は、まだ適用していない起動引数。
	PendingArgs datatypes.JSON `gorm:"column:pending_args"`

	// Mounts は、現在コンテナに反映されているボリュームのマウント(MountSpec の JSON の配列)。nil はマウント無し。
	Mounts datatypes.JSON `gorm:"column:mounts"`

	// PendingMounts は、まだ適用していないマウントの構成。変更が無ければ nil。空の配列は、全て外す変更。
	PendingMounts datatypes.JSON `gorm:"column:pending_mounts"`

	// CreatedAt は、作成日時。
	CreatedAt time.Time `gorm:"column:created_at"`

	// UpdatedAt は、更新日時。
	UpdatedAt time.Time `gorm:"column:updated_at"`

	// Pods は、deploy が持つ Pod(watcher が観測した現在の状態)。読み込んだときだけ入り、deploy の保存では書かない(読み取り専用)。
	Pods []DeployPod `gorm:"foreignKey:DeployID;references:ID;->"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("deploys")。
func (Deploy) TableName() string {
	return "deploys"
}

// DeploySpec は、deploy_specs テーブルの1行。スペック(small など)の CPU とメモリの大きさ。
type DeploySpec struct {
	// Name は、スペックの名前(small、medium、large)。
	Name string `gorm:"column:name;primaryKey"`

	// CPUMillicores は、CPU の大きさ(ミリコア。250 は 0.25 CPU)。
	CPUMillicores int `gorm:"column:cpu_millicores"`

	// MemoryMiB は、メモリの大きさ(MiB)。
	MemoryMiB int `gorm:"column:memory_mib"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("deploy_specs")。
func (DeploySpec) TableName() string {
	return "deploy_specs"
}
