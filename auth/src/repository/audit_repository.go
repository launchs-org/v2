package repository

import (
	"context"

	"gorm.io/gorm"

	"launchs/shared/apperror"
)

// AuditRepository は、admin_audit_logs テーブル(管理操作の監査ログ)に、追記する。
type AuditRepository struct {
	database *gorm.DB
}

// NewAuditRepository は、AuditRepository を作る。
//
// 引数:
//   - database: auth データベースの接続。
//
// 戻り値:
//   - 作成した AuditRepository。
func NewAuditRepository(database *gorm.DB) *AuditRepository {
	// 接続を持たせて返す。
	return &AuditRepository{database: database}
}

// Create は、監査ログを 1 件追記する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - actorID: 操作した管理者の ID(コマンドなら "command")。
//   - action: 操作の名前。
//   - target: 操作の対象。
//
// 戻り値:
//   - データベースのエラー(internal_error)。
func (auditRepository *AuditRepository) Create(requestContext context.Context, actorID string, action string, target string) error {
	// 追記する(日時は、データベースが決める)。
	execError := auditRepository.database.WithContext(requestContext).
		Exec("INSERT INTO admin_audit_logs (actor_id, action, target) VALUES (?, ?, ?)", actorID, action, target).Error
	if execError != nil {
		return apperror.Wrap(apperror.KindInternal, "監査ログを保存できませんでした", execError)
	}
	return nil
}
