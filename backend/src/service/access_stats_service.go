package service

import (
	"context"
	"time"

	"launchs/shared/accessstats"
	"launchs/shared/apperror"
	"launchs/shared/model"
)

// AccessStatsIngressFinder は、AccessStatsService が使う、ingress の取得(持ち主の確認を含む)。
type AccessStatsIngressFinder interface {
	// FindByID は、ingress を1件取得する。他のユーザーのものは、not_found。
	FindByID(requestContext context.Context, userID string, projectID string, ingressID string) (model.Ingress, error)
}

// AccessStatsReader は、AccessStatsService が使う、集計の読み出し。
type AccessStatsReader interface {
	// Query は、ingress の集計を、期間 [from, to) で読む。
	Query(requestContext context.Context, hours bool, ingressID string, from time.Time, to time.Time) ([]accessstats.Record, error)
}

// AccessStatsService は、ingress のアクセスの集計を返すユースケース。
type AccessStatsService struct {
	ingressFinder AccessStatsIngressFinder
	reader        AccessStatsReader
	// now は、現在の時刻を返す関数(試験で差し替える)。
	now func() time.Time
}

// NewAccessStatsService は、AccessStatsService を作る。
//
// 引数:
//   - ingressFinder: ingress の取得。
//   - reader: 集計の読み出し。
//
// 戻り値:
//   - 作成した AccessStatsService。
func NewAccessStatsService(ingressFinder AccessStatsIngressFinder, reader AccessStatsReader) *AccessStatsService {
	// 依存するものと、実際の時計を持たせて返す。
	return &AccessStatsService{ingressFinder: ingressFinder, reader: reader, now: time.Now}
}

// Get は、ingress のアクセスの集計を、期間を指定して返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - rangeName: 期間の名前(1h、24h、7d、30d)。空なら 1h。
//
// 戻り値:
//   - レポートと、ingress の集計の設定(有効か)。ingress が無ければ not_found、期間が不正なら invalid_request。
func (accessStatsService *AccessStatsService) Get(requestContext context.Context, userID string, projectID string, ingressID string, rangeName string) (accessstats.Report, bool, error) {
	// 期間を決める(空は 1h)。
	if rangeName == "" {
		rangeName = "1h"
	}
	spec, found := accessstats.FindRange(rangeName)
	if !found {
		return accessstats.Report{}, false, apperror.New(apperror.KindInvalidRequest, "range は 1h、24h、7d、30d のどれかにしてください")
	}
	// ingress が、このユーザーのものかを確かめる。
	ingress, findError := accessStatsService.ingressFinder.FindByID(requestContext, userID, projectID, ingressID)
	if findError != nil {
		return accessstats.Report{}, false, findError
	}
	// 期間の分の行を読んで、レポートにする。
	current := accessStatsService.now().UTC()
	from := current.Add(-spec.Length).Truncate(spec.Bucket)
	records, queryError := accessStatsService.reader.Query(requestContext, spec.Hours, ingressID, from, current.Add(time.Second))
	if queryError != nil {
		return accessstats.Report{}, false, queryError
	}
	return accessstats.BuildReport(spec, current, records), ingress.AccessStats, nil
}
