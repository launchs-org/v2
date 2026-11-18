package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// DeployLogQueryRepository は、API が使う、ログの読み取り(deploy_log_streams と deploy_logs)。読むだけで、書かない(書き手は logcollector)。
type DeployLogQueryRepository struct {
	database *gorm.DB
}

// NewDeployLogQueryRepository は、DeployLogQueryRepository を作る。
//
// 引数:
//   - database: 使うデータベース。
//
// 戻り値:
//   - 作成した DeployLogQueryRepository。
func NewDeployLogQueryRepository(database *gorm.DB) *DeployLogQueryRepository {
	// データベースを持たせて返す。
	return &DeployLogQueryRepository{database: database}
}

// ListLogStreams は、deploy のストリームのうち、ログの行が1行以上残っているものを、新しい Pod の順に返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: deploy の ID(持ち主の確認は、呼び出し側が済ませる)。
//   - limit: 最大の件数。
//
// 戻り値:
//   - ストリームの一覧(Pod の作成時刻の新しい順)。データベースのエラーは internal_error。
func (deployLogQueryRepository *DeployLogQueryRepository) ListLogStreams(requestContext context.Context, deployID string, limit int) ([]model.DeployLogStream, error) {
	// 空の値は、Gorm が条件を無視しないよう、先に拒否する。
	if hasEmpty(deployID) {
		return nil, newEmptyConditionError()
	}
	// 行が残っているストリームだけを、新しい順に読む。
	var streams []model.DeployLogStream
	findError := deployLogQueryRepository.database.WithContext(requestContext).
		Where("deploy_id = ? AND EXISTS (SELECT 1 FROM deploy_logs WHERE deploy_logs.pod_uid = deploy_log_streams.pod_uid)", deployID).
		Order("started_at DESC, pod_uid DESC").Limit(limit).Find(&streams).Error
	if findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ログのある Pod の一覧を取得できませんでした", findError)
	}
	return streams, nil
}

// ReadLogs は、Pod のログの行を、since より後のものから、id の順に、最大 maxLines 行返す。
//
// Pod が、その deploy のストリームでなければ(属したことが無い、または保持期間を過ぎて削除済み)、見つからない扱いにする。
// ストリームはあるが、行が全て削除されているときは、空の一覧を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - deployID: deploy の ID(持ち主の確認は、呼び出し側が済ませる)。
//   - podUID: Pod の UID。
//   - since: この id より後の行を返す(0 なら、最初から)。
//   - maxLines: 最大の行数。
//
// 戻り値:
//   - 行(id の順)。
//   - その Pod の、いまの最大の id(行が無ければ 0)。行が返らなかったときの、nextSince の計算に使う。
//   - Pod が、その deploy のストリームでなければ not_found。データベースのエラーは internal_error。
func (deployLogQueryRepository *DeployLogQueryRepository) ReadLogs(requestContext context.Context, deployID string, podUID string, since int64, maxLines int) ([]model.DeployLog, int64, error) {
	// 空の値は、Gorm が条件を無視しないよう、先に拒否する(見つからない扱い)。
	if hasEmpty(deployID, podUID) {
		return nil, 0, apperror.New(apperror.KindNotFound, "ログが見つかりません")
	}
	// ストリームが、その deploy のものであることを確かめる。
	database := deployLogQueryRepository.database.WithContext(requestContext)
	var stream model.DeployLogStream
	findError := database.Select("pod_uid").Where("pod_uid = ? AND deploy_id = ?", podUID, deployID).Take(&stream).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return nil, 0, apperror.Wrap(apperror.KindNotFound, "ログが見つかりません", findError)
	}
	if findError != nil {
		return nil, 0, apperror.Wrap(apperror.KindInternal, "ログを取得できませんでした", findError)
	}
	// since より後の行を、id の順に読む。
	var lines []model.DeployLog
	if readError := database.Where("pod_uid = ? AND id > ?", podUID, since).Order("id").Limit(maxLines).Find(&lines).Error; readError != nil {
		return nil, 0, apperror.Wrap(apperror.KindInternal, "ログを取得できませんでした", readError)
	}
	// 行が返らなかったときのために、いまの最大の id を読む(行があるときは、使わない)。
	var tail int64
	if len(lines) == 0 {
		if tailError := database.Model(&model.DeployLog{}).Where("pod_uid = ?", podUID).Select("COALESCE(max(id), 0)").Scan(&tail).Error; tailError != nil {
			return nil, 0, apperror.Wrap(apperror.KindInternal, "ログを取得できませんでした", tailError)
		}
	}
	return lines, tail, nil
}
