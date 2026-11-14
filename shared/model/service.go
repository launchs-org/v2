package model

import (
	"time"

	"launchs/shared/apperror"

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

// CanChangePorts は、service のポートを更新・適用・破棄できるかどうかを返す。
//
// 戻り値:
//   - active の service なら true。
func (service Service) CanChangePorts() bool {
	// active でない service は、ポートを変えられない。
	return service.Status == ServiceStatusActive
}

// CanDelete は、service の削除を受け付けられるかどうかを返す。
//
// 作成中(pending)と削除中(deleting)は受け付けない。active と failed は受け付ける。
//
// 戻り値:
//   - 受け付けられるなら true。
func (service Service) CanDelete() bool {
	// 作成中と削除中のどちらでもなければ受け付ける。
	return service.Status != ServiceStatusPending && service.Status != ServiceStatusDeleting
}

// BeginPortsApply は、ポートの反映を始められるかを確かめる。
//
// 戻り値:
//   - active でない、または適用する値が無いときは conflict。
func (service Service) BeginPortsApply() error {
	// active でない service は、ポートを変えられない。
	if !service.CanChangePorts() {
		return apperror.New(apperror.KindConflict, "service は "+service.Status+" のため、ポートを操作できません")
	}
	// 適用する値が無いときは、反映する意味が無い。
	if len(service.PendingPorts) == 0 {
		return apperror.New(apperror.KindConflict, "適用するポートの変更がありません")
	}
	return nil
}

// PromotePendingPorts は、pending のポートを、現在のポートにして、pending を空に戻す。
//
// pending が無いときは、何もしない(現在のポートを壊さない)。
func (service *Service) PromotePendingPorts() {
	// pending が無ければ何もしない。
	if len(service.PendingPorts) == 0 {
		return
	}
	// 現在のポートを pending の内容にして、pending を空に戻す。
	service.Ports = service.PendingPorts
	service.PendingPorts = nil
}

// CompleteCreation は、service の作成の結果を確定する。pending の service だけが対象。
//
//   - 成功: active にして、DNS 名と ClusterIP を記録する(ClusterIP が空なら NULL のまま)。
//   - 失敗: failed にする。
//
// 引数:
//   - succeeded: 作成に成功したか。
//   - dnsName: 払い出された DNS 名(成功のときだけ使う)。
//   - clusterIP: 払い出された ClusterIP(成功のときだけ使う。空なら記録しない)。
//
// 戻り値:
//   - 確定したら true。pending でないときは、何も変えず false。
func (service *Service) CompleteCreation(succeeded bool, dnsName string, clusterIP string) bool {
	// 作成中でなければ、確定するものが無い。
	if service.Status != ServiceStatusPending {
		return false
	}
	// 失敗のときは failed にするだけ。
	if !succeeded {
		service.Status = ServiceStatusFailed
		return true
	}
	// 成功のときは、active にして、払い出された値を記録する。
	service.Status = ServiceStatusActive
	service.DNSName = &dnsName
	service.ClusterIP = nil
	if clusterIP != "" {
		service.ClusterIP = &clusterIP
	}
	return true
}

// BeginDelete は、削除を受け付ける(状態を deleting にする)。
//
// 戻り値:
//   - 受け付ける前の状態(削除を始められなかったとき、元に戻すために使う)。
//   - 作成中と削除中なら conflict。
func (service *Service) BeginDelete() (string, error) {
	// 作成中と削除中は、受け付けられない。
	if !service.CanDelete() {
		return "", apperror.New(apperror.KindConflict, "service は "+service.Status+" のため、削除できません")
	}
	// 前の状態を覚えて、削除中にする。
	previousStatus := service.Status
	service.Status = ServiceStatusDeleting
	return previousStatus, nil
}

// RevertDeleting は、削除中の service を、削除を受け付ける前の状態に戻す。
//
// 引数:
//   - previousStatus: 戻す先の状態(BeginDelete が返したもの)。
//
// 戻り値:
//   - 戻したら true。削除中でないときは、何も変えず false。
func (service *Service) RevertDeleting(previousStatus string) bool {
	// 削除中でなければ、戻すものが無い。
	if service.Status != ServiceStatusDeleting {
		return false
	}
	// 前の状態に戻す。
	service.Status = previousStatus
	return true
}

// MarkDeleteFailed は、削除中の service を、失敗(failed)にする。
//
// 戻り値:
//   - 変えたら true。削除中でないときは、何も変えず false。
func (service *Service) MarkDeleteFailed() bool {
	// 削除中でなければ、変えるものが無い。
	if service.Status != ServiceStatusDeleting {
		return false
	}
	// 失敗の状態にする。
	service.Status = ServiceStatusFailed
	return true
}
