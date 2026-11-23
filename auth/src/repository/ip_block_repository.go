package repository

import (
	"context"
	"net/netip"

	"gorm.io/gorm"

	"launchs/shared/apperror"
)

// IPBlockRepository は、blocked_ips テーブル(ブロックする IP)を、読む。書くのは、管理コンテナ。
type IPBlockRepository struct {
	database *gorm.DB
}

// NewIPBlockRepository は、IPBlockRepository を作る。
//
// 引数:
//   - database: auth データベースの接続。
//
// 戻り値:
//   - 作成した IPBlockRepository。
func NewIPBlockRepository(database *gorm.DB) *IPBlockRepository {
	// 接続を持たせて返す。
	return &IPBlockRepository{database: database}
}

// IsBlocked は、IP が、期限内のブロックの範囲に入っているかを返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - clientIP: 調べる IP(読めない文字列は、ブロックされていない扱い)。
//
// 戻り値:
//   - ブロックされていれば true。データベースのエラーは internal_error。
func (ipBlockRepository *IPBlockRepository) IsBlocked(requestContext context.Context, clientIP string) (bool, error) {
	// IP として読めないものは、範囲に入れようがない。
	address, parseError := netip.ParseAddr(clientIP)
	if parseError != nil {
		return false, nil
	}
	// 範囲に含まれ、期限が切れていないブロックがあるかを調べる。
	var blocked bool
	queryError := ipBlockRepository.database.WithContext(requestContext).
		Raw("SELECT EXISTS (SELECT 1 FROM blocked_ips WHERE network >>= ?::inet AND (expires_at IS NULL OR expires_at > now()))", address.String()).Scan(&blocked).Error
	if queryError != nil {
		return false, apperror.Wrap(apperror.KindInternal, "ブロックの確認ができませんでした", queryError)
	}
	return blocked, nil
}
