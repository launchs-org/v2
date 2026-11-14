package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	// ServiceIDPrefix は、service の ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。k8s の Service の名前にも使う。
	ServiceIDPrefix = "service-"

	// ServiceStatusPending は、作成を受け付けて、まだ k8s に作れていない service。
	ServiceStatusPending = "pending"

	// ServiceStatusActive は、k8s に作成済みの service。
	ServiceStatusActive = "active"

	// ServiceStatusFailed は、作成または削除に失敗した service。削除は、もう一度受け付ける。
	ServiceStatusFailed = "failed"

	// ServiceStatusDeleting は、削除を受け付けて、実体を削除している最中の service。
	ServiceStatusDeleting = "deleting"

	// ServiceProtocolTCP と ServiceProtocolUDP は、公開するポートの protocol。
	ServiceProtocolTCP = "tcp"
	ServiceProtocolUDP = "udp"
)

// ServicePort は、service が公開するポート1つ。services.ports の JSON の配列の要素。
type ServicePort struct {
	// Name は、ポートの名前(k8s のポート名の規則: 小文字の英数字とハイフン、15文字まで)。
	Name string `json:"name"`

	// Port は、ポート番号(1〜65535)。Service のポートと、コンテナのポートに、同じ番号を使う。
	Port int `json:"port"`

	// Protocol は、"tcp" または "udp"。
	Protocol string `json:"protocol"`
}

// Service は、services テーブルの1行。k8s の Service 1つに対応する。
type Service struct {
	// ID は、service の ID("service-" + UUID v4)。k8s の Service の名前にも使う。
	ID string `gorm:"column:id;primaryKey"`

	// DeployID は、属する deploy の ID(1 deploy = 1 service)。
	DeployID string `gorm:"column:deploy_id"`

	// ProjectID は、属するプロジェクトの ID(k8s の namespace の名前でもある)。
	ProjectID string `gorm:"column:project_id"`

	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id"`

	// Ports は、現在反映されているポートの一覧(ServicePort の JSON の配列)。
	Ports datatypes.JSON `gorm:"column:ports"`

	// PendingPorts は、まだ適用していないポートの一覧。変更が無ければ nil。
	PendingPorts datatypes.JSON `gorm:"column:pending_ports"`

	// Status は、ServiceStatus... のどれか。
	Status string `gorm:"column:status"`

	// DNSName は、k8s の Service の DNS 名。active になるまで nil。
	DNSName *string `gorm:"column:dns_name"`

	// ClusterIP は、k8s が払い出した ClusterIP。active になるまで nil。
	ClusterIP *string `gorm:"column:cluster_ip"`

	// CreatedAt は、作成日時。
	CreatedAt time.Time `gorm:"column:created_at"`

	// UpdatedAt は、更新日時。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}
