package service

import (
	"context"
	"strings"
	"testing"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// fakeDeployLogStore は、テスト用の DeployLogStore。決めた行を返し、呼び出された引数を記録する。
type fakeDeployLogStore struct {
	lines       []model.DeployLog
	tail        int64
	readError   error
	lastSince   int64
	lastMaxRead int
	streams     []model.DeployLogStream
	lastLimit   int
}

// ListLogStreams は、決めたストリームを返し、上限を記録する。
//
// 引数:
//   - requestContext: 使わない。
//   - deployID: 使わない。
//   - limit: 最大の件数。
//
// 戻り値:
//   - ストリームの一覧。
func (store *fakeDeployLogStore) ListLogStreams(requestContext context.Context, deployID string, limit int) ([]model.DeployLogStream, error) {
	// 上限を記録して、返す。
	store.lastLimit = limit
	return store.streams, nil
}

// ReadLogs は、決めた行(since より後)を返し、引数を記録する。
//
// 引数:
//   - requestContext: 使わない。
//   - deployID: 使わない。
//   - podUID: 使わない。
//   - since: この id より後の行を返す。
//   - maxLines: 最大の行数。
//
// 戻り値:
//   - 行、最大の id、エラー。
func (store *fakeDeployLogStore) ReadLogs(requestContext context.Context, deployID string, podUID string, since int64, maxLines int) ([]model.DeployLog, int64, error) {
	// 引数を記録して、決めたエラーか、since より後の行(上限まで)を返す。
	store.lastSince, store.lastMaxRead = since, maxLines
	if store.readError != nil {
		return nil, 0, store.readError
	}
	var result []model.DeployLog
	for _, line := range store.lines {
		if line.ID > since && len(result) < maxLines {
			result = append(result, line)
		}
	}
	return result, store.tail, nil
}

// TestDeployLogServiceGetLogs は、持ち主の確認、nextSince(返した最後の行、行が無いときは since のまま、
// since が末尾を超えたときは末尾)、大きさの上限での切り詰め(少なくとも1行は返す)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployLogServiceGetLogs(testingT *testing.T) {
	// 他のユーザーの deploy は、見つからない。ログの取得にも進まない。
	logStore := &fakeDeployLogStore{lines: []model.DeployLog{{ID: 5, Line: "a"}, {ID: 7, Line: "b"}}, tail: 7}
	foreignStore := &fakeDeployStore{findError: apperror.New(apperror.KindNotFound, "deploy が見つかりません")}
	if _, getError := NewDeployLogService(foreignStore, logStore).GetLogs(context.Background(), "user-a", "deploy-1", "uid-1", 0); apperror.KindOf(getError) != apperror.KindNotFound || logStore.lastMaxRead != 0 {
		testingT.Fatalf("deploy が無ければ not_found で、ログは読まないはず: %v", getError)
	}
	// 持ち主の deploy。最初から読むと、2行と、最後の id。
	ownedStore := &fakeDeployStore{}
	logService := NewDeployLogService(ownedStore, logStore)
	chunk, getError := logService.GetLogs(context.Background(), "user-a", "deploy-1", "uid-1", 0)
	if getError != nil || len(chunk.Lines) != 2 || chunk.NextSince != 7 || logStore.lastMaxRead != MaxLogLinesPerResponse {
		testingT.Fatalf("2行と、最後の id のはず: %+v %v", chunk, getError)
	}
	// 末尾まで読んだ後(since=7)は、行が無く、since のまま。
	if caughtUp, _ := logService.GetLogs(context.Background(), "user-a", "deploy-1", "uid-1", 7); len(caughtUp.Lines) != 0 || caughtUp.NextSince != 7 {
		testingT.Fatalf("行が無く、since のままのはず: %+v", caughtUp)
	}
	// since が末尾を超えているときは、末尾(7)。
	if beyond, _ := logService.GetLogs(context.Background(), "user-a", "deploy-1", "uid-1", 1000); len(beyond.Lines) != 0 || beyond.NextSince != 7 {
		testingT.Fatalf("末尾を超えたら、末尾のはず: %+v", beyond)
	}
	// ログが1行も無い Pod は、最大の id が 0 なので、since が 0 でも、0 のまま。
	empty := NewDeployLogService(ownedStore, &fakeDeployLogStore{})
	if emptyChunk, _ := empty.GetLogs(context.Background(), "user-a", "deploy-1", "uid-1", 0); emptyChunk.NextSince != 0 || len(emptyChunk.Lines) != 0 {
		testingT.Fatalf("空のはず: %+v", emptyChunk)
	}
	// Pod が見つからないエラーは、そのまま返る。
	missing := NewDeployLogService(ownedStore, &fakeDeployLogStore{readError: apperror.New(apperror.KindNotFound, "ログが見つかりません")})
	if _, missingError := missing.GetLogs(context.Background(), "user-a", "deploy-1", "uid-x", 0); apperror.KindOf(missingError) != apperror.KindNotFound {
		testingT.Fatalf("not_found のはず: %v", missingError)
	}
	// 大きさの上限(1MiB): 400KiB の行が3つあると、2つ(800KiB)で収まる…が、3つ目で超えるので、2行で切る。nextSince は、2行目の id。
	big := strings.Repeat("x", 400*1024)
	bigStore := &fakeDeployLogStore{lines: []model.DeployLog{{ID: 1, Line: big}, {ID: 2, Line: big}, {ID: 3, Line: big}}, tail: 3}
	bigChunk, _ := NewDeployLogService(ownedStore, bigStore).GetLogs(context.Background(), "user-a", "deploy-1", "uid-1", 0)
	if len(bigChunk.Lines) != 2 || bigChunk.NextSince != 2 {
		testingT.Fatalf("上限で、2行で切るはず: %d 行 nextSince=%d", len(bigChunk.Lines), bigChunk.NextSince)
	}
	// 1行が上限より大きくても、少なくとも1行は返す(進めなくならない)。
	hugeStore := &fakeDeployLogStore{lines: []model.DeployLog{{ID: 1, Line: strings.Repeat("y", MaxLogBytesPerResponse+10)}, {ID: 2, Line: "z"}}, tail: 2}
	hugeChunk, _ := NewDeployLogService(ownedStore, hugeStore).GetLogs(context.Background(), "user-a", "deploy-1", "uid-1", 0)
	if len(hugeChunk.Lines) != 1 || hugeChunk.NextSince != 1 {
		testingT.Fatalf("少なくとも1行は返すはず: %d 行 nextSince=%d", len(hugeChunk.Lines), hugeChunk.NextSince)
	}
}

// TestDeployLogServiceListLogPods は、ログのある Pod の一覧が、持ち主の確認の後で、上限(50)つきで読まれることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployLogServiceListLogPods(testingT *testing.T) {
	// 他のユーザーの deploy は、見つからない(ストリームも読まない)。
	logStore := &fakeDeployLogStore{streams: []model.DeployLogStream{{PodUID: "uid-1"}}}
	if _, listError := NewDeployLogService(&fakeDeployStore{findError: apperror.New(apperror.KindNotFound, "deploy が見つかりません")}, logStore).ListLogPods(context.Background(), "user-a", "deploy-1"); apperror.KindOf(listError) != apperror.KindNotFound || logStore.lastLimit != 0 {
		testingT.Fatalf("deploy が無ければ not_found のはず: %v", listError)
	}
	// 持ち主の deploy なら、一覧が、上限 50 で読まれる。
	streams, listError := NewDeployLogService(&fakeDeployStore{}, logStore).ListLogPods(context.Background(), "user-a", "deploy-1")
	if listError != nil || len(streams) != 1 || logStore.lastLimit != 50 {
		testingT.Fatalf("一覧が、上限 50 で読まれるはず: %+v %d %v", streams, logStore.lastLimit, listError)
	}
}
