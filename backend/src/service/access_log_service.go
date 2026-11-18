package service

import (
	"context"

	"launchs/shared/pagination"
	"launchs/shared/repository"
)

// AccessLogReader は、AccessLogService が使う、アクセスログの読み出し。
type AccessLogReader interface {
	// ListLogs は、ingress のアクセスログを、新しい順に返す。
	ListLogs(requestContext context.Context, ingressID string, filter repository.AccessLogFilter, limit int, after *pagination.Position) ([]repository.AccessLog, *pagination.Position, error)
}

// AccessLogService は、ingress のアクセスログ(1 リクエストごとの記録)を返すユースケース。
type AccessLogService struct {
	ingressFinder AccessStatsIngressFinder
	reader        AccessLogReader
}

// NewAccessLogService は、AccessLogService を作る。
//
// 引数:
//   - ingressFinder: ingress の取得(持ち主の確認を含む)。
//   - reader: アクセスログの読み出し。
//
// 戻り値:
//   - 作成した AccessLogService。
func NewAccessLogService(ingressFinder AccessStatsIngressFinder, reader AccessLogReader) *AccessLogService {
	// 依存するものを持たせて返す。
	return &AccessLogService{ingressFinder: ingressFinder, reader: reader}
}

// List は、ingress のアクセスログを、新しい順に返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - filter: 絞り込み。
//   - limit: 1 ページの件数。
//   - after: 前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - ログと、次のページの位置。ingress が無ければ not_found、絞り込みが不正なら invalid_request。
func (accessLogService *AccessLogService) List(requestContext context.Context, userID string, projectID string, ingressID string, filter repository.AccessLogFilter, limit int, after *pagination.Position) ([]repository.AccessLog, *pagination.Position, error) {
	// ingress が、このユーザーのものかを確かめる。
	if _, findError := accessLogService.ingressFinder.FindByID(requestContext, userID, projectID, ingressID); findError != nil {
		return nil, nil, findError
	}
	// 保存先に任せる。
	return accessLogService.reader.ListLogs(requestContext, ingressID, filter, limit, after)
}
