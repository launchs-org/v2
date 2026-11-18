package service

import (
	"context"

	"launchs/shared/model"
)

const (
	// MaxLogLinesPerResponse は、1回のレスポンスで返すログの行数の上限。
	MaxLogLinesPerResponse = 1000

	// MaxLogBytesPerResponse は、1回のレスポンスで返すログの大きさの上限(バイト。行の本文の合計)。
	MaxLogBytesPerResponse = 1 << 20

	// MaxLogPods は、deploy の詳細に載せる、ログのある Pod の数の上限。
	MaxLogPods = 50
)

// DeployLogStore は、DeployLogService が必要とする、ログの読み取り。
//
// 実体は shared/repository の DeployLogQueryRepository。テストでは fake に差し替える。
type DeployLogStore interface {
	// ListLogStreams は、deploy のストリームのうち、ログの行が残っているものを、新しい Pod の順に返す。
	ListLogStreams(requestContext context.Context, deployID string, limit int) ([]model.DeployLogStream, error)

	// ReadLogs は、Pod のログの行を、since より後のものから、最大 maxLines 行返す。行が無いときは、Pod の最大の id も返す。
	ReadLogs(requestContext context.Context, deployID string, podUID string, since int64, maxLines int) ([]model.DeployLog, int64, error)
}

// LogChunk は、ログの取得の結果(1回分)。
type LogChunk struct {
	// Lines は、返す行(id の順)。
	Lines []model.DeployLog

	// NextSince は、次に since に渡す値。
	NextSince int64
}

// DeployLogService は、deploy のログの取得のビジネスルール。
type DeployLogService struct {
	deployStore DeployStore
	logStore    DeployLogStore
}

// NewDeployLogService は、DeployLogService を作る。
//
// 引数:
//   - deployStore: deploy の取得(持ち主の確認に使う)。
//   - logStore: ログの読み取り。
//
// 戻り値:
//   - 作成した DeployLogService。
func NewDeployLogService(deployStore DeployStore, logStore DeployLogStore) *DeployLogService {
	// 依存するものを持たせて返す。
	return &DeployLogService{deployStore: deployStore, logStore: logStore}
}

// ListLogPods は、ユーザーの deploy の、ログのある Pod(終了した Pod も含む)を、新しい Pod の順に、最大 MaxLogPods 件返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//
// 戻り値:
//   - ストリームの一覧。無ければ空。
//   - deploy が無い、または他のユーザーのものなら not_found。
func (deployLogService *DeployLogService) ListLogPods(requestContext context.Context, userID string, deployID string) ([]model.DeployLogStream, error) {
	// 先に、ユーザーの deploy であることを確かめる(他のユーザーの deploy のログは、見せない)。
	if _, findError := deployLogService.deployStore.FindByID(requestContext, userID, deployID); findError != nil {
		return nil, findError
	}
	// 持ち主の確認ができたので、読む。
	return deployLogService.logStore.ListLogStreams(requestContext, deployID, MaxLogPods)
}

// GetLogs は、Pod のログを、since より後のものから、1回の上限(MaxLogLinesPerResponse 行、MaxLogBytesPerResponse バイトの、先に達した方)まで返す。
//
// 続きは、返した NextSince を since に渡して取る。行が返らないときの NextSince は、since のまま(since が末尾を超えているときは、末尾の id)。
// 大きさの上限を超える場合も、少なくとも1行は返す(1行が上限より大きくても、進めなくならない。1行は 16KiB までに、収集のときに切り詰めてある)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: deploy の ID。
//   - podUID: Pod の UID(API の podId から、接頭辞を除いたもの)。
//   - since: この id より後の行を返す(0 なら、最初から)。
//
// 戻り値:
//   - 行と、NextSince。
//   - deploy が無い、他のユーザーのもの、または Pod がその deploy に属したことが無い(保持期間を過ぎて削除済みを含む)なら not_found。
func (deployLogService *DeployLogService) GetLogs(requestContext context.Context, userID string, deployID string, podUID string, since int64) (LogChunk, error) {
	// 先に、ユーザーの deploy であることを確かめる。
	if _, findError := deployLogService.deployStore.FindByID(requestContext, userID, deployID); findError != nil {
		return LogChunk{}, findError
	}
	// 行を、上限の行数まで読む。
	lines, tail, readError := deployLogService.logStore.ReadLogs(requestContext, deployID, podUID, since, MaxLogLinesPerResponse)
	if readError != nil {
		return LogChunk{}, readError
	}
	// 行が無いときは、since のまま。ただし、since が末尾を超えているときは、末尾(クライアントが、次回もそのまま使い続けられるように)。
	if len(lines) == 0 {
		return LogChunk{Lines: nil, NextSince: min(since, tail)}, nil
	}
	// 大きさの上限に達するところまでで切る(少なくとも1行は返す)。
	totalBytes := 0
	for index, line := range lines {
		totalBytes += len(line.Line)
		if totalBytes > MaxLogBytesPerResponse && index > 0 {
			lines = lines[:index]
			break
		}
	}
	return LogChunk{Lines: lines, NextSince: lines[len(lines)-1].ID}, nil
}
