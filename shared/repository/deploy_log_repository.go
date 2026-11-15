package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// ErrLogStreamGone は、ログの保存先のストリームが無い(deploy が削除された)ときのエラー。収集を止める合図になる。
var ErrLogStreamGone = errors.New("ログのストリームがありません(deploy が削除されました)")

// logInsertBatchSize は、1回の INSERT にまとめる行数。
const logInsertBatchSize = 500

// DeployLogRepository は、ログの収集(書き込み、読み取り位置、削除)に使う、deploy_log_streams と deploy_logs の操作。
// 書き手は logcollector だけ。
type DeployLogRepository struct {
	database *gorm.DB
}

// NewDeployLogRepository は、DeployLogRepository を作る。
//
// 引数:
//   - database: 使うデータベース。
//
// 戻り値:
//   - 作成した DeployLogRepository。
func NewDeployLogRepository(database *gorm.DB) *DeployLogRepository {
	// データベースを持たせて返す。
	return &DeployLogRepository{database: database}
}

// ListLivePods は、watcher が観測している Pod(deploy_pods の全行)を返す。logcollector が、どの Pod を追うかの元になる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - Pod の一覧。データベースのエラーは internal_error。
func (deployLogRepository *DeployLogRepository) ListLivePods(requestContext context.Context) ([]model.DeployPod, error) {
	// 全件を読む。
	var pods []model.DeployPod
	if findError := deployLogRepository.database.WithContext(requestContext).Find(&pods).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "Pod の一覧を取得できませんでした", findError)
	}
	return pods, nil
}

// FindLivePod は、Pod 1つを、deploy_pods から探す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - podUID: Pod の UID。
//
// 戻り値:
//   - Pod と、あったか。データベースのエラーは internal_error。
func (deployLogRepository *DeployLogRepository) FindLivePod(requestContext context.Context, podUID string) (model.DeployPod, bool, error) {
	// 空の値は、Gorm が条件を無視しないよう、先に拒否する。
	if hasEmpty(podUID) {
		return model.DeployPod{}, false, nil
	}
	// UID で探す。
	var pod model.DeployPod
	findError := deployLogRepository.database.WithContext(requestContext).Where("pod_uid = ?", podUID).Take(&pod).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.DeployPod{}, false, nil
	}
	if findError != nil {
		return model.DeployPod{}, false, apperror.Wrap(apperror.KindInternal, "Pod を取得できませんでした", findError)
	}
	return pod, true, nil
}

// EnsureStream は、Pod のストリームが無ければ作り、あるものを返す。
//
// deploy が無い(削除された)ときは、作れないので、ErrLogStreamGone を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - pod: 追う Pod。
//
// 戻り値:
//   - ストリーム。データベースのエラーは internal_error。
func (deployLogRepository *DeployLogRepository) EnsureStream(requestContext context.Context, pod model.DeployPod) (model.DeployLogStream, error) {
	// 空の値は、先に拒否する。
	if hasEmpty(pod.PodUID, pod.DeployID) {
		return model.DeployLogStream{}, newEmptyConditionError()
	}
	// 無ければ作る(あれば、何もしない)。
	stream := model.DeployLogStream{PodUID: pod.PodUID, DeployID: pod.DeployID, ProjectID: pod.ProjectID, PodName: pod.PodName, StartedAt: pod.PodCreatedAt}
	database := deployLogRepository.database.WithContext(requestContext)
	createError := database.Clauses(clause.OnConflict{DoNothing: true}).Create(&stream).Error
	if errors.Is(createError, gorm.ErrForeignKeyViolated) {
		return model.DeployLogStream{}, ErrLogStreamGone
	}
	if createError != nil {
		return model.DeployLogStream{}, apperror.Wrap(apperror.KindInternal, "ログのストリームを作れませんでした", createError)
	}
	// 保存されているものを読む。
	var stored model.DeployLogStream
	if findError := database.Where("pod_uid = ?", pod.PodUID).Take(&stored).Error; findError != nil {
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return model.DeployLogStream{}, ErrLogStreamGone
		}
		return model.DeployLogStream{}, apperror.Wrap(apperror.KindInternal, "ログのストリームを取得できませんでした", findError)
	}
	return stored, nil
}

// ListOpenStreams は、収集中(終了していない)のストリームを返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - ストリームの一覧。データベースのエラーは internal_error。
func (deployLogRepository *DeployLogRepository) ListOpenStreams(requestContext context.Context) ([]model.DeployLogStream, error) {
	// 終了していないものを読む。
	var streams []model.DeployLogStream
	if findError := deployLogRepository.database.WithContext(requestContext).Where("finished_at IS NULL").Find(&streams).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ログのストリームを取得できませんでした", findError)
	}
	return streams, nil
}

// AppendLines は、ログの行を保存し、読み取り位置と大きさを、同じトランザクションで更新する。
//
// 行が無くても、位置は更新する(捨てた行や、書き込みの速さの上限で捨てた行の分も、位置を進めるため)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - podUID: Pod の UID。
//   - lines: 保存する行(PodUID は、ここで設定する)。
//   - position: 保存した最後の行までの、新しい読み取り位置。
//
// 戻り値:
//   - ストリームが無ければ ErrLogStreamGone。データベースのエラーは internal_error。
func (deployLogRepository *DeployLogRepository) AppendLines(requestContext context.Context, podUID string, lines []model.DeployLog, position model.ResumePosition) error {
	// 空の値は、先に拒否する。
	if hasEmpty(podUID) {
		return newEmptyConditionError()
	}
	// 保存と位置の更新を、1つのトランザクションで行う。
	return deployLogRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行を、まとめて保存し、大きさの合計を求める。
		var addedBytes int64
		for index := range lines {
			lines[index].PodUID = podUID
			addedBytes += int64(len(lines[index].Line))
		}
		if len(lines) > 0 {
			createError := transaction.CreateInBatches(&lines, logInsertBatchSize).Error
			if errors.Is(createError, gorm.ErrForeignKeyViolated) {
				return ErrLogStreamGone
			}
			if createError != nil {
				return apperror.Wrap(apperror.KindInternal, "ログを保存できませんでした", createError)
			}
		}
		// 読み取り位置と、大きさを更新する(ストリームが無ければ、0行)。
		result := transaction.Model(&model.DeployLogStream{}).Where("pod_uid = ?", podUID).Updates(map[string]any{
			"resume_ns": position.NS, "resume_count": position.Count, "bytes": gorm.Expr("bytes + ?", addedBytes),
		})
		if result.Error != nil {
			return apperror.Wrap(apperror.KindInternal, "読み取り位置を保存できませんでした", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrLogStreamGone
		}
		return nil
	})
}

// SetRestartCount は、ストリームが最後に処理した、コンテナの再起動の回数を保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - podUID: Pod の UID。
//   - restartCount: 処理した再起動の回数。
//
// 戻り値:
//   - データベースのエラーは internal_error。ストリームが無くても、エラーにしない。
func (deployLogRepository *DeployLogRepository) SetRestartCount(requestContext context.Context, podUID string, restartCount int) error {
	// 空の値は、先に拒否する。
	if hasEmpty(podUID) {
		return newEmptyConditionError()
	}
	// 回数を更新する。
	if updateError := deployLogRepository.database.WithContext(requestContext).Model(&model.DeployLogStream{}).Where("pod_uid = ?", podUID).Update("restart_count", restartCount).Error; updateError != nil {
		return apperror.Wrap(apperror.KindInternal, "再起動の回数を保存できませんでした", updateError)
	}
	return nil
}

// FinishStream は、ストリームを終了にする(すでに終了していれば、時刻は変えない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - podUID: Pod の UID。
//
// 戻り値:
//   - データベースのエラーは internal_error。
func (deployLogRepository *DeployLogRepository) FinishStream(requestContext context.Context, podUID string) error {
	// 空の値は、先に拒否する。
	if hasEmpty(podUID) {
		return newEmptyConditionError()
	}
	// 終了していないものだけ、DB の時計で終了にする。
	if updateError := deployLogRepository.database.WithContext(requestContext).Model(&model.DeployLogStream{}).Where("pod_uid = ? AND finished_at IS NULL", podUID).Update("finished_at", gorm.Expr("now()")).Error; updateError != nil {
		return apperror.Wrap(apperror.KindInternal, "ログのストリームを終了にできませんでした", updateError)
	}
	return nil
}

// LogPruneResult は、保存期間と上限の削除の結果。
type LogPruneResult struct {
	// ExpiredLines は、保存期間を過ぎて削除した行の数。
	ExpiredLines int64

	// OverflowLines は、Pod ごとの上限を超えて削除した行の数。
	OverflowLines int64

	// DeletedStreams は、終了して保存期間を過ぎたため、行ごと削除したストリームの数。
	DeletedStreams int64
}

// PruneLogs は、保存期間と、Pod ごとの上限を超えたログを削除する。
//
//  1. 行の時刻が cutoff より古い行を削除する。
//  2. 保存済みの大きさが maxBytesPerPod を超えたストリームは、新しい行から数えて上限を超えた分の、古い行を削除する。
//  3. 終了して cutoff を過ぎたストリームを、行ごと削除する(その podId は、取得できなくなる)。
//
// 削除した行の大きさは、ストリームの bytes から引く(書き込みと同時でも、相対の更新なので、ずれない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - cutoff: この時刻より古いものを削除する。
//   - maxBytesPerPod: Pod ごとの大きさの上限(バイト)。
//
// 戻り値:
//   - 削除した数。データベースのエラーは internal_error。
func (deployLogRepository *DeployLogRepository) PruneLogs(requestContext context.Context, cutoff time.Time, maxBytesPerPod int64) (LogPruneResult, error) {
	// 結果を集める。
	var result LogPruneResult
	database := deployLogRepository.database.WithContext(requestContext)
	// 1. 保存期間を過ぎた行を削除して、大きさを引く。
	expiredCount, expiredError := deleteLogs(database, "WHERE logged_ns < ?", cutoff.UnixNano())
	if expiredError != nil {
		return result, apperror.Wrap(apperror.KindInternal, "古いログを削除できませんでした", expiredError)
	}
	result.ExpiredLines = expiredCount
	// 2. 上限を超えたストリームごとに、古い行を削除する。
	var overflowing []string
	if findError := database.Model(&model.DeployLogStream{}).Where("bytes > ?", maxBytesPerPod).Pluck("pod_uid", &overflowing).Error; findError != nil {
		return result, apperror.Wrap(apperror.KindInternal, "上限を超えたログを探せませんでした", findError)
	}
	for _, podUID := range overflowing {
		trimmedCount, trimmedError := deleteLogs(database, `WHERE pod_uid = ? AND id <= (
			SELECT id FROM (
				SELECT id, sum(octet_length(line)) OVER (ORDER BY id DESC) AS running FROM deploy_logs WHERE pod_uid = ?
			) AS counted WHERE running > ? ORDER BY id DESC LIMIT 1)`, podUID, podUID, maxBytesPerPod)
		if trimmedError != nil {
			return result, apperror.Wrap(apperror.KindInternal, "上限を超えたログを削除できませんでした", trimmedError)
		}
		result.OverflowLines += trimmedCount
	}
	// 3. 終了して保存期間を過ぎたストリームを、行ごと削除する(行は CASCADE で消える)。
	streams := database.Where("finished_at IS NOT NULL AND finished_at < ?", cutoff).Delete(&model.DeployLogStream{})
	if streams.Error != nil {
		return result, apperror.Wrap(apperror.KindInternal, "古いログのストリームを削除できませんでした", streams.Error)
	}
	result.DeletedStreams = streams.RowsAffected
	return result, nil
}

// deleteAndSubtractSQL は、条件に合う deploy_logs の行を削除し、削除した大きさを、ストリームの bytes から引いて、削除した行数を返す SQL を作る。
//
// 更新の CTE は、結果を使わなくても、最後まで実行される(PostgreSQL の決まり)。
//
// 引数:
//   - whereClause: "WHERE ..." の条件(deploy_logs に対するもの)。
//
// 戻り値:
//   - SQL(削除した行数を、1列で返す)。
func deleteAndSubtractSQL(whereClause string) string {
	// 削除した行の大きさと数を、ストリームごとに集めて、大きさを引く。
	return `WITH deleted AS (DELETE FROM deploy_logs ` + whereClause + ` RETURNING pod_uid, octet_length(line) AS size),
		removed AS (SELECT pod_uid, sum(size) AS total, count(*) AS lines FROM deleted GROUP BY pod_uid),
		updated AS (UPDATE deploy_log_streams streams SET bytes = GREATEST(streams.bytes - removed.total, 0)
			FROM removed WHERE streams.pod_uid = removed.pod_uid RETURNING streams.pod_uid)
		SELECT COALESCE(sum(lines), 0)::bigint FROM removed`
}

// deleteLogs は、条件に合う行を削除して、大きさを引き、削除した行数を返す。
//
// 引数:
//   - database: 使うデータベース。
//   - whereClause: "WHERE ..." の条件。
//   - arguments: 条件の引数。
//
// 戻り値:
//   - 削除した行数。データベースのエラー。
func deleteLogs(database *gorm.DB, whereClause string, arguments ...any) (int64, error) {
	// SQL を実行して、削除した行数を受け取る。
	var deleted int64
	if scanError := database.Raw(deleteAndSubtractSQL(whereClause), arguments...).Scan(&deleted).Error; scanError != nil {
		return 0, scanError
	}
	return deleted, nil
}
