package service

import (
	"context"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"launchs/admin/src/repository"
	"launchs/shared/apperror"
	"launchs/shared/pagination"
)

// maxBlockHours は、ブロックの期限の上限(時間。1 年)。
const maxBlockHours = 8760

// SecurityStore は、セキュリティの表の読み書き(repository.SecurityRepository が満たす)。
type SecurityStore interface {
	ListBlocked(requestContext context.Context) ([]repository.BlockedIP, error)
	CreateBlock(requestContext context.Context, block repository.BlockedIP) error
	DeleteBlock(requestContext context.Context, blockID string) (string, error)
	SuspiciousIPs(requestContext context.Context, hours int, minFailures int) ([]repository.SuspiciousIP, error)
	CreateAudit(requestContext context.Context, entry repository.AuditLog) error
	ListAudit(requestContext context.Context, limit int, after *pagination.Position, action string) ([]repository.AuditLog, *pagination.Position, error)
}

// Actor は、操作した管理者(監査ログに残す)。
type Actor struct {
	// ID と Name は、管理者の ID と名前。
	ID   string
	Name string

	// IP は、操作した接続元の IP。
	IP string
}

// SecurityService は、不審な IP の確認、IP のブロック、監査ログのユースケース。
type SecurityService struct {
	securityStore SecurityStore
}

// NewSecurityService は、SecurityService を作る。
//
// 引数:
//   - securityStore: セキュリティの表の読み書き。
//
// 戻り値:
//   - 作成した SecurityService。
func NewSecurityService(securityStore SecurityStore) *SecurityService {
	// 依存するものを持たせて返す。
	return &SecurityService{securityStore: securityStore}
}

// Audit は、管理操作を、監査ログに追記する(他のユースケースからも使う)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - actor: 操作した管理者。
//   - action: 操作の名前。
//   - target: 操作の対象。
//   - detail: 補足(秘密は入れない)。
//
// 戻り値:
//   - 保存できなかったときのエラー。
func (securityService *SecurityService) Audit(requestContext context.Context, actor Actor, action string, target string, detail string) error {
	// 追記する。
	return securityService.securityStore.CreateAudit(requestContext, repository.AuditLog{ActorID: actor.ID, ActorName: actor.Name, Action: action, Target: target, Detail: detail, IP: actor.IP})
}

// ListBlocked は、有効なブロックの一覧を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - ブロックの一覧。
func (securityService *SecurityService) ListBlocked(requestContext context.Context) ([]repository.BlockedIP, error) {
	// 読み出しに任せる。
	return securityService.securityStore.ListBlocked(requestContext)
}

// Block は、IP(または範囲)をブロックして、監査ログに残す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - actor: 操作した管理者。
//   - network: ブロックする IP か CIDR(例: 203.0.113.5、203.0.113.0/24)。
//   - reason: 理由(200 バイトまで)。
//   - expiresInHours: 期限(時間)。0 なら、解除するまで。
//
// 戻り値:
//   - 追加したブロック。形が不正、期限が範囲外、理由が長い、**操作している自分の IP を含む**ときは invalid_request。
func (securityService *SecurityService) Block(requestContext context.Context, actor Actor, network string, reason string, expiresInHours int) (repository.BlockedIP, error) {
	// 範囲を読む(IP だけなら、その 1 つだけの範囲にする。ホスト部は、落とす)。
	prefix, parseError := parseNetwork(strings.TrimSpace(network))
	if parseError != nil {
		return repository.BlockedIP{}, parseError
	}
	if len(reason) > 200 {
		return repository.BlockedIP{}, apperror.New(apperror.KindInvalidRequest, "理由は 200 バイトまでです")
	}
	if expiresInHours < 0 || expiresInHours > maxBlockHours {
		return repository.BlockedIP{}, apperror.New(apperror.KindInvalidRequest, "期限は 0〜8760 時間で指定してください(0 は、解除するまで)")
	}
	// 自分を締め出さないよう、操作している接続元を含む範囲は、拒否する。
	if address, selfError := netip.ParseAddr(actor.IP); selfError == nil && prefix.Contains(address) {
		return repository.BlockedIP{}, apperror.New(apperror.KindInvalidRequest, "あなたの接続元の IP が含まれるため、ブロックできません")
	}
	block := repository.BlockedIP{ID: "block-" + uuid.NewString(), Network: prefix.String(), Reason: reason, CreatedBy: actor.ID, CreatedByName: actor.Name, CreatedAt: time.Now()}
	if expiresInHours > 0 {
		expiresAt := time.Now().Add(time.Duration(expiresInHours) * time.Hour)
		block.ExpiresAt = &expiresAt
	}
	// 保存して、監査ログに残す。
	if createError := securityService.securityStore.CreateBlock(requestContext, block); createError != nil {
		return repository.BlockedIP{}, createError
	}
	if auditError := securityService.Audit(requestContext, actor, "ip_block", block.Network, reason); auditError != nil {
		return repository.BlockedIP{}, auditError
	}
	return block, nil
}

// Unblock は、ブロックを解除して、監査ログに残す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - actor: 操作した管理者。
//   - blockID: ブロックの ID。
//
// 戻り値:
//   - ブロックが無ければ not_found。
func (securityService *SecurityService) Unblock(requestContext context.Context, actor Actor, blockID string) error {
	// 消して、監査ログに残す。
	network, deleteError := securityService.securityStore.DeleteBlock(requestContext, blockID)
	if deleteError != nil {
		return deleteError
	}
	return securityService.Audit(requestContext, actor, "ip_unblock", network, "")
}

// SuspiciousIPs は、失敗が多い IP を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - hours: 集計する期間(1〜720 時間)。
//   - minFailures: 一覧に出す、失敗の最小の数(1〜10000)。
//
// 戻り値:
//   - 一覧。範囲外は invalid_request。
func (securityService *SecurityService) SuspiciousIPs(requestContext context.Context, hours int, minFailures int) ([]repository.SuspiciousIP, error) {
	// 範囲を確かめる。
	if hours < 1 || hours > 720 || minFailures < 1 || minFailures > 10000 {
		return nil, apperror.New(apperror.KindInvalidRequest, "hours は 1〜720、minFailures は 1〜10000 で指定してください")
	}
	// 集計に任せる。
	return securityService.securityStore.SuspiciousIPs(requestContext, hours, minFailures)
}

// ListAudit は、監査ログを返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 件数の上限。
//   - after: 前のページの最後の位置。
//   - action: 操作の名前での絞り込み。
//
// 戻り値:
//   - 監査ログと、次のページの位置。
func (securityService *SecurityService) ListAudit(requestContext context.Context, limit int, after *pagination.Position, action string) ([]repository.AuditLog, *pagination.Position, error) {
	// 読み出しに任せる。
	return securityService.securityStore.ListAudit(requestContext, limit, after, action)
}

// parseNetwork は、IP か CIDR の文字列を、範囲にする(ホスト部は落とす)。
//
// 引数:
//   - text: IP か CIDR。
//
// 戻り値:
//   - 範囲。読めなければ invalid_request。
func parseNetwork(text string) (netip.Prefix, error) {
	// CIDR として読めればそれを、IP として読めれば 1 つだけの範囲にする。
	if prefix, err := netip.ParsePrefix(text); err == nil {
		return prefix.Masked(), nil
	}
	if address, err := netip.ParseAddr(text); err == nil {
		return netip.PrefixFrom(address.Unmap(), address.Unmap().BitLen()), nil
	}
	return netip.Prefix{}, apperror.New(apperror.KindInvalidRequest, "IP か CIDR(例: 203.0.113.0/24)で指定してください")
}
