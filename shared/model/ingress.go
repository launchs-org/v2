package model

import (
	"encoding/json"
	"regexp"
	"time"

	"launchs/shared/apperror"

	"gorm.io/datatypes"
)

const (
	// IngressIDPrefix は、ingress の ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。IngressRoute の名前にも使う。
	IngressIDPrefix = "ingress-"

	// IngressStatusActive は、反映済みの ingress。
	IngressStatusActive = "active"

	// IngressStatusApplying は、ルールの反映が進んでいる ingress。
	IngressStatusApplying = "applying"

	// IngressStatusFailed は、反映または削除に失敗した ingress。
	IngressStatusFailed = "failed"

	// IngressStatusDeleting は、削除を受け付けて、実体を削除している最中の ingress。
	IngressStatusDeleting = "deleting"

	// MaxIngressRules は、1つの ingress に置けるルールの数の上限。
	MaxIngressRules = 50

	// MaxIngressPathLength は、ルールの path の長さの上限。
	MaxIngressPathLength = 200
)

// ingressPathPattern は、ルールの path に使える形(/ で始まり、英数字と . _ ~ - % / だけ)。
// Traefik のルールの式に埋め込むので、引用符やバッククォートや空白は、受け付けない。
var ingressPathPattern = regexp.MustCompile(`^/[A-Za-z0-9._~%/-]*$`)

// IngressRule は、ingress のルール1つ。path に一致した通信を、service のポートへ流す。
// ingresses.rules と ingresses.pending_rules の JSON の配列の要素。
type IngressRule struct {
	// Path は、前方一致させる path("/" や "/api")。
	Path string `json:"path"`

	// ServiceID は、流す先の service の ID(同じプロジェクトの service)。
	ServiceID string `json:"serviceId"`

	// Port は、流す先の service の、tcp のポート番号。
	Port int `json:"port"`
}

// Ingress は、ingresses テーブルの1行。Traefik の IngressRoute 1つに対応する。
type Ingress struct {
	// ID は、ingress の ID("ingress-" + UUID v4)。
	ID string `gorm:"column:id;primaryKey"`

	// ProjectID は、属するプロジェクトの ID(k8s の namespace の名前でもある)。
	ProjectID string `gorm:"column:project_id"`

	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id"`

	// Host は、払い出したホスト名。
	Host string `gorm:"column:host"`

	// Rules は、現在反映されているルールの一覧(IngressRule の JSON の配列)。
	Rules datatypes.JSON `gorm:"column:rules"`

	// PendingRules は、まだ適用していないルールの一覧。変更が無ければ nil。空の配列は、ルールを全て消す変更。
	PendingRules datatypes.JSON `gorm:"column:pending_rules"`

	// Status は、ingress の状態(IngressStatus... のどれか)。
	Status string `gorm:"column:status"`

	// CreatedAt は、作成日時。
	CreatedAt time.Time `gorm:"column:created_at"`

	// UpdatedAt は、更新日時。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("ingresses")。
func (Ingress) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "ingresses"
}

// ValidateIngressRules は、ルールの一覧の形(path の書式、重複、ポート番号の範囲、個数)を確かめる。
// 流す先の service が存在するか、などは、データベースを見る必要があるので、ここでは確かめない。
//
// 引数:
//   - rules: 確かめるルールの一覧(空でもよい)。
//
// 戻り値:
//   - 不正なら invalid_request。
func ValidateIngressRules(rules []IngressRule) error {
	// 個数の上限を確かめる。
	if len(rules) > MaxIngressRules {
		return apperror.New(apperror.KindInvalidRequest, "rules は50件までです")
	}
	// path の重複を調べるための入れ物。
	usedPaths := map[string]bool{}
	for _, rule := range rules {
		// path の書式と長さを確かめる。
		if len(rule.Path) > MaxIngressPathLength || !ingressPathPattern.MatchString(rule.Path) {
			return apperror.New(apperror.KindInvalidRequest, "path は、/ で始まり、英数字と . _ ~ - % / だけの200文字以内でなければなりません: "+rule.Path)
		}
		// 同じ path は2つ置けない。
		if usedPaths[rule.Path] {
			return apperror.New(apperror.KindInvalidRequest, "rules の path が重複しています: "+rule.Path)
		}
		usedPaths[rule.Path] = true
		// service の ID の形と、ポート番号の範囲を確かめる。
		if len(rule.ServiceID) <= len(ServiceIDPrefix) || rule.ServiceID[:len(ServiceIDPrefix)] != ServiceIDPrefix {
			return apperror.New(apperror.KindInvalidRequest, "serviceId の形が正しくありません: "+rule.ServiceID)
		}
		if rule.Port < 1 || rule.Port > 65535 {
			return apperror.New(apperror.KindInvalidRequest, "port は1〜65535でなければなりません")
		}
	}
	return nil
}

// ParseIngressRules は、JSON のルールの一覧を、読み込む。
//
// 引数:
//   - raw: JSON の配列(空なら、ルール無し)。
//
// 戻り値:
//   - ルールの一覧。読めなければエラー。
func ParseIngressRules(raw []byte) ([]IngressRule, error) {
	// 空は、ルール無し。
	if len(raw) == 0 {
		return []IngressRule{}, nil
	}
	// JSON を読み込む。
	rules := []IngressRule{}
	if parseError := json.Unmarshal(raw, &rules); parseError != nil {
		return nil, parseError
	}
	return rules, nil
}

// --- 状態による操作の可否 ---

// IsApplying は、ingress が反映中かを返す。
//
// 戻り値:
//   - 反映中なら true。
func (ingress Ingress) IsApplying() bool {
	// applying が、反映中。
	return ingress.Status == IngressStatusApplying
}

// CanChangeRules は、ルールを更新・適用・破棄できるかを返す。
//
// 戻り値:
//   - active、または failed(直して再度適用できる)なら true。反映中と削除中は false。
func (ingress Ingress) CanChangeRules() bool {
	// 反映中と削除中以外なら、操作できる。
	return ingress.Status == IngressStatusActive || ingress.Status == IngressStatusFailed
}

// CanDelete は、削除を受け付けられるかを返す。
//
// 戻り値:
//   - 反映中と削除中以外なら true(failed は、もう一度削除できる)。
func (ingress Ingress) CanDelete() bool {
	// 反映中と削除中は、受け付けない。
	return !ingress.IsApplying() && ingress.Status != IngressStatusDeleting
}

// --- 状態を変える操作 ---

// SettledStatus は、反映が終わったあとの、落ち着いた状態を返す。
//
// 戻り値:
//   - 常に active。
func (ingress Ingress) SettledStatus() string {
	// ingress は、反映が成功すれば active。
	return IngressStatusActive
}

// BeginRulesApply は、ルールの反映を始める(状態を applying にする)。
//
// 戻り値:
//   - 受け付ける前の状態(開始に失敗したとき、元に戻すために使う)。
//   - 操作できない状態(反映中、削除中)、または適用する値が無いときは conflict。
func (ingress *Ingress) BeginRulesApply() (string, error) {
	// 反映中や削除中は、操作できない。
	if !ingress.CanChangeRules() {
		return "", apperror.New(apperror.KindConflict, "ingress は "+ingress.Status+" のため、ルールを操作できません")
	}
	// 適用する値が無いときは、反映する意味が無い。
	if len(ingress.PendingRules) == 0 {
		return "", apperror.New(apperror.KindConflict, "適用するルールの変更がありません")
	}
	// 前の状態を覚えて、反映中にする。
	previousStatus := ingress.Status
	ingress.Status = IngressStatusApplying
	return previousStatus, nil
}

// CommitRules は、ルールの反映の結果を確定する。
//
//   - 成功: ルールを pending の内容にし、pending を空に戻し、active にする。
//   - 失敗: ルールと pending は、そのまま(動いている値を保つ)。failed にする。
//
// 引数:
//   - succeeded: 反映に成功したか。
//
// 戻り値:
//   - 確定したら true。applying でないときは、何も変えず false。
func (ingress *Ingress) CommitRules(succeeded bool) bool {
	// 反映中でなければ、確定するものが無い。
	if !ingress.IsApplying() {
		return false
	}
	// 失敗のときは、failed にするだけ。
	if !succeeded {
		ingress.Status = IngressStatusFailed
		return true
	}
	// 成功のときは、pending があるときだけ、ルールにして pending を空に戻す。
	if len(ingress.PendingRules) > 0 {
		ingress.Rules = ingress.PendingRules
		ingress.PendingRules = nil
	}
	ingress.Status = ingress.SettledStatus()
	return true
}

// RevertApply は、反映の開始に失敗したとき、状態を元に戻す。
//
// 引数:
//   - status: 戻す先の状態。
//
// 戻り値:
//   - 戻したら true。applying でないときは、何も変えず false。
func (ingress *Ingress) RevertApply(status string) bool {
	// 反映中でなければ、戻すものが無い。
	if !ingress.IsApplying() {
		return false
	}
	// 前の状態に戻す。
	ingress.Status = status
	return true
}

// BeginDelete は、削除を受け付ける(状態を deleting にする)。
//
// 戻り値:
//   - 受け付ける前の状態。
//   - 反映中、または削除中なら conflict。
func (ingress *Ingress) BeginDelete() (string, error) {
	// 反映中と削除中は、受け付けられない。
	if !ingress.CanDelete() {
		return "", apperror.New(apperror.KindConflict, "ingress は "+ingress.Status+" のため、削除できません")
	}
	// 前の状態を覚えて、削除中にする。
	previousStatus := ingress.Status
	ingress.Status = IngressStatusDeleting
	return previousStatus, nil
}

// RevertDeleting は、削除中の ingress を、削除を受け付ける前の状態に戻す。
//
// 引数:
//   - previousStatus: 戻す先の状態。
//
// 戻り値:
//   - 戻したら true。削除中でないときは、何も変えず false。
func (ingress *Ingress) RevertDeleting(previousStatus string) bool {
	// 削除中でなければ、戻すものが無い。
	if ingress.Status != IngressStatusDeleting {
		return false
	}
	// 前の状態に戻す。
	ingress.Status = previousStatus
	return true
}

// MarkDeleteFailed は、削除中の ingress を、failed にする(削除は、もう一度受け付ける)。
//
// 戻り値:
//   - 変えたら true。削除中でないときは、何も変えず false。
func (ingress *Ingress) MarkDeleteFailed() bool {
	// 削除中でなければ、変えるものが無い。
	if ingress.Status != IngressStatusDeleting {
		return false
	}
	// 失敗の状態にする。
	ingress.Status = IngressStatusFailed
	return true
}
