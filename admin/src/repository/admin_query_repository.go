package repository

import (
	"context"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/pagination"
)

// AdminDeploy は、全体の一覧に出す deploy の 1 行(持ち主とプロジェクトの名前つき)。
type AdminDeploy struct {
	// ID は、deploy の ID。
	ID string `gorm:"column:id" json:"deployId"`

	// ProjectID と ProjectName は、属するプロジェクト。
	ProjectID   string `gorm:"column:project_id" json:"projectId"`
	ProjectName string `gorm:"column:project_name" json:"projectName"`

	// UserID は、持ち主のユーザーの ID。
	UserID string `gorm:"column:user_id" json:"userId"`

	// ImageRef は、イメージ。
	ImageRef string `gorm:"column:image_ref" json:"imageRef"`

	// Spec は、スペックの名前。
	Spec string `gorm:"column:spec" json:"spec"`

	// Replicas は、希望するレプリカ数。
	Replicas int `gorm:"column:replicas" json:"replicas"`

	// Status は、状態。
	Status string `gorm:"column:status" json:"status"`

	// CreatedAt は、作成の日時。
	CreatedAt time.Time `gorm:"column:created_at" json:"-"`
}

// AdminDeployFilter は、deploy の一覧の絞り込み(空の項目は、絞り込まない)。
type AdminDeployFilter struct {
	// Status は、状態。
	Status string

	// UserID は、持ち主のユーザーの ID。
	UserID string

	// Keyword は、イメージ・プロジェクト名・deploy の ID に含まれる文字列。
	Keyword string
}

// AdminAccessLog は、全体のアクセスログの 1 行(ingress のホスト名つき)。
type AdminAccessLog struct {
	// ID は、行の ID。
	ID int64 `gorm:"column:id" json:"-"`

	// Host は、ingress のホスト名(ingress が消えていれば空)。
	Host string `gorm:"column:host" json:"host"`

	// IngressID は、ingress の ID。
	IngressID string `gorm:"column:ingress_id" json:"ingressId"`

	// RequestedAt は、リクエストを受けた時刻。
	RequestedAt time.Time `gorm:"column:requested_at" json:"-"`

	// Method、URL、Status、DurationMs、ClientIP、Bytes は、リクエストの内容。
	Method     string  `gorm:"column:method" json:"method"`
	URL        string  `gorm:"column:url" json:"url"`
	Status     int     `gorm:"column:status" json:"status"`
	DurationMs float64 `gorm:"column:duration_ms" json:"durationMs"`
	ClientIP   string  `gorm:"column:client_ip" json:"clientIp"`
	Bytes      int64   `gorm:"column:bytes" json:"bytes"`
}

// AdminAccessLogFilter は、アクセスログの絞り込み(空の項目は、絞り込まない)。
type AdminAccessLogFilter struct {
	// Status は、「2xx」「3xx」「4xx」「5xx」の分類か、「404」のような数字。
	Status string

	// ClientIP は、クライアントの IP(完全一致)。
	ClientIP string

	// Keyword は、ホスト名か URL に含まれる文字列。
	Keyword string
}

// AdminQueryRepository は、管理画面の、全体の一覧(deploy、アクセスログ)を読む。
type AdminQueryRepository struct {
	database *gorm.DB
}

// NewAdminQueryRepository は、AdminQueryRepository を作る。
//
// 引数:
//   - database: app データベースの接続。
//
// 戻り値:
//   - 作成した AdminQueryRepository。
func NewAdminQueryRepository(database *gorm.DB) *AdminQueryRepository {
	// 接続を持たせて返す。
	return &AdminQueryRepository{database: database}
}

// escapeLike は、LIKE の記号(%、_、\)を、そのままの文字として扱えるようにする。
//
// 引数:
//   - text: 検索の文字列。
//
// 戻り値:
//   - エスケープした文字列。
func escapeLike(text string) string {
	// 記号の前に、エスケープの文字を足す。
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(text)
}

// ListDeploys は、全ユーザーの deploy を、新しい順に取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 件数の上限。
//   - after: 前のページの最後の位置。最初は nil。
//   - filter: 絞り込み。
//
// 戻り値:
//   - deploy(新しい順)と、次のページの位置(最後なら nil)。データベースのエラーは internal_error。
func (adminQueryRepository *AdminQueryRepository) ListDeploys(requestContext context.Context, limit int, after *pagination.Position, filter AdminDeployFilter) ([]AdminDeploy, *pagination.Position, error) {
	// プロジェクトの名前を付けて、新しい順に、確認のため 1 件多く読む。
	query := adminQueryRepository.database.WithContext(requestContext).Table("deploys AS d").
		Select("d.id, d.project_id, COALESCE(p.name, '') AS project_name, d.user_id, d.image_ref, d.spec, d.replicas, d.status, d.created_at").
		Joins("LEFT JOIN projects p ON p.id = d.project_id").Order("d.created_at DESC, d.id DESC").Limit(limit + 1)
	// 絞り込みを足す。
	if filter.Status != "" {
		query = query.Where("d.status = ?", filter.Status)
	}
	if filter.UserID != "" {
		query = query.Where("d.user_id = ?", filter.UserID)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		pattern := "%" + escapeLike(strings.ToLower(keyword)) + "%"
		query = query.Where(`(lower(d.image_ref) LIKE ? ESCAPE '\' OR lower(p.name) LIKE ? ESCAPE '\' OR lower(d.id) LIKE ? ESCAPE '\')`, pattern, pattern, pattern)
	}
	// 2 ページ目以降は、前のページの最後より後ろから読む。
	if after != nil {
		query = query.Where("(d.created_at, d.id) < (?, ?)", after.CreatedAt, after.ID)
	}
	deploys := []AdminDeploy{}
	if findError := query.Scan(&deploys).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "deploy の一覧を取得できませんでした", findError)
	}
	// 次のページが無ければ、位置は nil。
	if len(deploys) <= limit {
		return deploys, nil, nil
	}
	deploys = deploys[:limit]
	last := deploys[limit-1]
	return deploys, &pagination.Position{CreatedAt: last.CreatedAt, ID: last.ID}, nil
}

// ListAccessLogs は、全 ingress のアクセスログを、新しい順に取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limit: 件数の上限。
//   - after: 前のページの最後の位置(ID は、行の連番を文字列にしたもの)。最初は nil。
//   - filter: 絞り込み。
//
// 戻り値:
//   - ログ(新しい順)と、次のページの位置(最後なら nil)。不正な指定は invalid_request、データベースのエラーは internal_error。
func (adminQueryRepository *AdminQueryRepository) ListAccessLogs(requestContext context.Context, limit int, after *pagination.Position, filter AdminAccessLogFilter) ([]AdminAccessLog, *pagination.Position, error) {
	// ingress のホスト名を付けて、新しい順に、確認のため 1 件多く読む。
	query := adminQueryRepository.database.WithContext(requestContext).Table("ingress_access_logs AS l").
		Select("l.id, COALESCE(i.host, '') AS host, l.ingress_id, l.requested_at, l.method, l.url, l.status, l.duration_ms, l.client_ip, l.bytes").
		Joins("LEFT JOIN ingresses i ON i.id = l.ingress_id").Order("l.requested_at DESC, l.id DESC").Limit(limit + 1)
	// ステータスの絞り込み(分類か、数字)。
	switch status := strings.ToLower(strings.TrimSpace(filter.Status)); {
	case status == "":
	case len(status) == 3 && status[1:] == "xx" && status[0] >= '2' && status[0] <= '5':
		base := int(status[0]-'0') * 100
		query = query.Where("l.status >= ? AND l.status < ?", base, base+100)
	default:
		code, parseError := strconv.Atoi(status)
		if parseError != nil {
			return nil, nil, apperror.New(apperror.KindInvalidRequest, "status は 2xx〜5xx か、数字で指定してください")
		}
		query = query.Where("l.status = ?", code)
	}
	if filter.ClientIP != "" {
		query = query.Where("l.client_ip = ?", filter.ClientIP)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		pattern := "%" + escapeLike(strings.ToLower(keyword)) + "%"
		query = query.Where(`(lower(l.url) LIKE ? ESCAPE '\' OR lower(i.host) LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	// 2 ページ目以降は、前のページの最後より後ろから読む。
	if after != nil {
		lastID, parseError := strconv.ParseInt(after.ID, 10, 64)
		if parseError != nil {
			return nil, nil, apperror.New(apperror.KindInvalidRequest, "cursor が正しくありません")
		}
		query = query.Where("(l.requested_at, l.id) < (?, ?)", after.CreatedAt, lastID)
	}
	logs := []AdminAccessLog{}
	if findError := query.Scan(&logs).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "アクセスログを取得できませんでした", findError)
	}
	// 次のページが無ければ、位置は nil。
	if len(logs) <= limit {
		return logs, nil, nil
	}
	logs = logs[:limit]
	last := logs[limit-1]
	return logs, &pagination.Position{CreatedAt: last.RequestedAt, ID: strconv.FormatInt(last.ID, 10)}, nil
}
