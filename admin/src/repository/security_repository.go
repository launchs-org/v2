package repository

import (
	"context"
	"net/netip"
	"strconv"
	"time"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/pagination"
)

// BlockedIP は、ブロックしている IP(範囲)1 件。
type BlockedIP struct {
	// ID は、ブロックの ID。
	ID string `gorm:"column:id" json:"blockId"`

	// Network は、ブロックする範囲(CIDR)。
	Network string `gorm:"column:network" json:"network"`

	// Reason は、理由。
	Reason string `gorm:"column:reason" json:"reason"`

	// CreatedBy と CreatedByName は、ブロックした管理者。
	CreatedBy     string `gorm:"column:created_by" json:"createdBy"`
	CreatedByName string `gorm:"column:created_by_name" json:"createdByName"`

	// CreatedAt は、ブロックした日時。
	CreatedAt time.Time `gorm:"column:created_at" json:"-"`

	// ExpiresAt は、期限。無期限なら nil。
	ExpiresAt *time.Time `gorm:"column:expires_at" json:"-"`
}

// SuspiciousIP は、ログインの失敗が多い IP の集計。
type SuspiciousIP struct {
	// IP は、クライアントの IP。
	IP string `gorm:"column:ip" json:"ip"`

	// Failures と Successes は、集計の期間の、失敗と成功の数。
	Failures  int `gorm:"column:failures" json:"failures"`
	Successes int `gorm:"column:successes" json:"successes"`

	// UserNames は、失敗で試された、ユーザー名の種類の数(多いほど、総当たりの疑いが強い)。
	UserNames int `gorm:"column:user_names" json:"userNames"`

	// LastSeen は、最後の試行の日時。
	LastSeen time.Time `gorm:"column:last_seen" json:"-"`

	// Blocked は、いま、ブロックされているか。
	Blocked bool `gorm:"-" json:"blocked"`
}

// AuditLog は、管理操作の監査ログ 1 件。
type AuditLog struct {
	// ID は、行の連番。
	ID int64 `gorm:"column:id" json:"-"`

	// OccurredAt は、操作の日時。
	OccurredAt time.Time `gorm:"column:occurred_at" json:"-"`

	// ActorID と ActorName は、操作した管理者。
	ActorID   string `gorm:"column:actor_id" json:"actorId"`
	ActorName string `gorm:"column:actor_name" json:"actorName"`

	// Action、Target、Detail、IP は、操作の内容。
	Action string `gorm:"column:action" json:"action"`
	Target string `gorm:"column:target" json:"target"`
	Detail string `gorm:"column:detail" json:"detail"`
	IP     string `gorm:"column:ip" json:"ip"`
}

// SecurityRepository は、auth データベースの、セキュリティの表(ブロック、監査ログ、ログイン履歴の集計)を読み書きする。
type SecurityRepository struct {
	database *gorm.DB
}

// NewSecurityRepository は、SecurityRepository を作る。
//
// 引数:
//   - database: auth データベースの接続。
//
// 戻り値:
//   - 作成した SecurityRepository。
func NewSecurityRepository(database *gorm.DB) *SecurityRepository {
	// 接続を持たせて返す。
	return &SecurityRepository{database: database}
}

// ListBlocked は、有効なブロック(期限が切れていないもの)を、新しい順に返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - ブロックの一覧。データベースのエラーは internal_error。
func (securityRepository *SecurityRepository) ListBlocked(requestContext context.Context) ([]BlockedIP, error) {
	// ネットワークは、文字列にして読む。
	blocks := []BlockedIP{}
	queryError := securityRepository.database.WithContext(requestContext).Table("blocked_ips").
		Select("id, network::text AS network, reason, created_by, created_by_name, created_at, expires_at").
		Where("expires_at IS NULL OR expires_at > now()").Order("created_at DESC, id DESC").Scan(&blocks).Error
	if queryError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ブロックの一覧を取得できませんでした", queryError)
	}
	return blocks, nil
}

// CreateBlock は、ブロックを 1 件追加する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - block: 追加するブロック(ID、範囲、理由、管理者。期限は nil で無期限)。
//
// 戻り値:
//   - データベースのエラー(internal_error)。
func (securityRepository *SecurityRepository) CreateBlock(requestContext context.Context, block BlockedIP) error {
	// 追加する(日時は、データベースが決める)。
	execError := securityRepository.database.WithContext(requestContext).Exec(
		"INSERT INTO blocked_ips (id, network, reason, created_by, created_by_name, expires_at) VALUES (?, ?::cidr, ?, ?, ?, ?)",
		block.ID, block.Network, block.Reason, block.CreatedBy, block.CreatedByName, block.ExpiresAt).Error
	if execError != nil {
		return apperror.Wrap(apperror.KindInternal, "ブロックを追加できませんでした", execError)
	}
	return nil
}

// DeleteBlock は、ブロックを解除する(行を消す。記録は、監査ログに残る)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - blockID: ブロックの ID。
//
// 戻り値:
//   - 解除したブロックの範囲。無ければ not_found。
func (securityRepository *SecurityRepository) DeleteBlock(requestContext context.Context, blockID string) (string, error) {
	// 消した行の範囲を受け取る。
	var networks []string
	deleteError := securityRepository.database.WithContext(requestContext).Raw("DELETE FROM blocked_ips WHERE id = ? RETURNING network::text", blockID).Scan(&networks).Error
	if deleteError != nil {
		return "", apperror.Wrap(apperror.KindInternal, "ブロックを解除できませんでした", deleteError)
	}
	if len(networks) == 0 {
		return "", apperror.New(apperror.KindNotFound, "ブロックが見つかりません")
	}
	return networks[0], nil
}

// SuspiciousIPs は、期間内に、ログインの失敗が一定以上ある IP を、失敗の多い順に返す(ブロック中かも付ける)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - hours: 集計する期間(時間)。
//   - minFailures: 一覧に出す、失敗の最小の数。
//
// 戻り値:
//   - 一覧(最大 100 件)。データベースのエラーは internal_error。
func (securityRepository *SecurityRepository) SuspiciousIPs(requestContext context.Context, hours int, minFailures int) ([]SuspiciousIP, error) {
	// IP ごとに、失敗と成功と、試されたユーザー名の数を集める。
	ips := []SuspiciousIP{}
	queryError := securityRepository.database.WithContext(requestContext).Raw(`
		SELECT ip,
		       count(*) FILTER (WHERE result = 'failure') AS failures,
		       count(*) FILTER (WHERE result = 'success') AS successes,
		       count(DISTINCT user_name_lower) FILTER (WHERE result = 'failure') AS user_names,
		       max(occurred_at) AS last_seen
		FROM login_events
		WHERE occurred_at > now() - make_interval(hours => ?)
		GROUP BY ip
		HAVING count(*) FILTER (WHERE result = 'failure') >= ?
		ORDER BY failures DESC, last_seen DESC
		LIMIT 100`, hours, minFailures).Scan(&ips).Error
	if queryError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "不審な IP を集計できませんでした", queryError)
	}
	// ブロック中かを付ける(範囲に入っているかは、Go で調べる)。
	blocks, listError := securityRepository.ListBlocked(requestContext)
	if listError != nil {
		return nil, listError
	}
	prefixes := make([]netip.Prefix, 0, len(blocks))
	for _, block := range blocks {
		if prefix, parseError := netip.ParsePrefix(block.Network); parseError == nil {
			prefixes = append(prefixes, prefix)
		}
	}
	for index := range ips {
		if address, parseError := netip.ParseAddr(ips[index].IP); parseError == nil {
			for _, prefix := range prefixes {
				if prefix.Contains(address) {
					ips[index].Blocked = true
				}
			}
		}
	}
	return ips, nil
}

// CreateAudit は、監査ログを 1 件追記する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - entry: 追記する内容(ID と日時は、データベースが決める)。
//
// 戻り値:
//   - データベースのエラー(internal_error)。
func (securityRepository *SecurityRepository) CreateAudit(requestContext context.Context, entry AuditLog) error {
	// 追記する。
	execError := securityRepository.database.WithContext(requestContext).Exec(
		"INSERT INTO admin_audit_logs (actor_id, actor_name, action, target, detail, ip) VALUES (?, ?, ?, ?, ?, ?)",
		entry.ActorID, entry.ActorName, entry.Action, entry.Target, entry.Detail, entry.IP).Error
	if execError != nil {
		return apperror.Wrap(apperror.KindInternal, "監査ログを保存できませんでした", execError)
	}
	return nil
}

// ListAudit は、監査ログを、新しい順に取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 件数の上限。
//   - after: 前のページの最後の位置(ID は、行の連番を文字列にしたもの)。最初は nil。
//   - action: 操作の名前で絞り込む。空なら絞り込まない。
//
// 戻り値:
//   - 監査ログ(新しい順)と、次のページの位置。不正なカーソルは invalid_request。
func (securityRepository *SecurityRepository) ListAudit(requestContext context.Context, limit int, after *pagination.Position, action string) ([]AuditLog, *pagination.Position, error) {
	// 新しい順に、確認のため 1 件多く読む。
	query := securityRepository.database.WithContext(requestContext).Table("admin_audit_logs").Order("occurred_at DESC, id DESC").Limit(limit + 1)
	if action != "" {
		query = query.Where("action = ?", action)
	}
	if after != nil {
		lastID, parseError := strconv.ParseInt(after.ID, 10, 64)
		if parseError != nil {
			return nil, nil, apperror.New(apperror.KindInvalidRequest, "cursor が正しくありません")
		}
		query = query.Where("(occurred_at, id) < (?, ?)", after.CreatedAt, lastID)
	}
	logs := []AuditLog{}
	if findError := query.Scan(&logs).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "監査ログを取得できませんでした", findError)
	}
	// 次のページが無ければ、位置は nil。
	if len(logs) <= limit {
		return logs, nil, nil
	}
	logs = logs[:limit]
	last := logs[limit-1]
	return logs, &pagination.Position{CreatedAt: last.OccurredAt, ID: strconv.FormatInt(last.ID, 10)}, nil
}
