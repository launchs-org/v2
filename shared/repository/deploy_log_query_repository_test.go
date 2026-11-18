package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// TestDeployLogQueryRepositoryReadLogs は、ログの読み取り(since より後を、id の順に、上限まで)、
// 行が無いときの最大の id、他の deploy や存在しない Pod が not_found になること、行が全て無いストリームは空で返ることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployLogQueryRepositoryReadLogs(testingT *testing.T) {
	// ストリームを作って、10 行を保存する(別の Pod にも、2 行)。
	database, logRepository := newLogTestEnvironment(testingT)
	queryRepository := NewDeployLogQueryRepository(database)
	ctx := context.Background()
	for _, uid := range []string{"uid-1", "uid-2", "uid-empty"} {
		if _, ensureError := logRepository.EnsureStream(ctx, insertLivePod(testingT, database, uid, 0)); ensureError != nil {
			testingT.Fatal(ensureError)
		}
	}
	if appendError := logRepository.AppendLines(ctx, "uid-1", logLines(100, 10), model.ResumePosition{NS: 109, Count: 1}); appendError != nil {
		testingT.Fatal(appendError)
	}
	if appendError := logRepository.AppendLines(ctx, "uid-2", logLines(200, 2), model.ResumePosition{NS: 201, Count: 1}); appendError != nil {
		testingT.Fatal(appendError)
	}
	// 最初から、上限(4行)まで。id の順で、他の Pod の行は混ざらない。
	lines, _, readError := queryRepository.ReadLogs(ctx, "deploy-00000001", "uid-1", 0, 4)
	if readError != nil || len(lines) != 4 || lines[0].Line != "line-0" || lines[3].Line != "line-3" {
		testingT.Fatalf("最初の4行のはず: %+v %v", lines, readError)
	}
	// 続き: 最後の id より後から。残りは、6行(上限より少ない)。
	rest, _, _ := queryRepository.ReadLogs(ctx, "deploy-00000001", "uid-1", lines[3].ID, 100)
	if len(rest) != 6 || rest[0].Line != "line-4" || rest[5].Line != "line-9" {
		testingT.Fatalf("残りの6行のはず: %+v", rest)
	}
	// 末尾まで読んだ後は、行が無く、最大の id が返る。since が末尾を超えても、同じ。
	none, tail, _ := queryRepository.ReadLogs(ctx, "deploy-00000001", "uid-1", rest[5].ID, 100)
	if len(none) != 0 || tail != rest[5].ID {
		testingT.Fatalf("行が無く、最大の id が返るはず: %d %d", len(none), tail)
	}
	if beyond, beyondTail, _ := queryRepository.ReadLogs(ctx, "deploy-00000001", "uid-1", rest[5].ID+1000, 100); len(beyond) != 0 || beyondTail != rest[5].ID {
		testingT.Fatalf("末尾を超えても、最大の id が返るはず: %d %d", len(beyond), beyondTail)
	}
	// 行が全て無いストリームは、200 相当(空で、最大の id は 0)。
	if empty, emptyTail, emptyError := queryRepository.ReadLogs(ctx, "deploy-00000001", "uid-empty", 0, 100); emptyError != nil || len(empty) != 0 || emptyTail != 0 {
		testingT.Fatalf("空のはず: %d %d %v", len(empty), emptyTail, emptyError)
	}
	// その deploy のものでない Pod、存在しない Pod、空の指定は not_found。
	for name, arguments := range map[string][2]string{"別の deploy": {"deploy-other", "uid-1"}, "存在しない Pod": {"deploy-00000001", "uid-nothing"}, "空の Pod": {"deploy-00000001", ""}, "空の deploy": {"", "uid-1"}} {
		if _, _, notFoundError := queryRepository.ReadLogs(ctx, arguments[0], arguments[1], 0, 10); apperror.KindOf(notFoundError) != apperror.KindNotFound {
			testingT.Errorf("%s は not_found のはず: %v", name, notFoundError)
		}
	}
}

// TestDeployLogQueryRepositoryListLogStreams は、ログの行が残っているストリームだけが、新しい Pod の順に、上限まで返ること
// (行が無い・全て削除されたストリーム、他の deploy のストリームは載らない)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployLogQueryRepositoryListLogStreams(testingT *testing.T) {
	// 5つの Pod(古い順に作る)を用意する。uid-3 には行が無い。
	database, logRepository := newLogTestEnvironment(testingT)
	queryRepository := NewDeployLogQueryRepository(database)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	for index := 1; index <= 5; index++ {
		uid := fmt.Sprintf("uid-%d", index)
		pod := insertLivePod(testingT, database, uid, 0)
		database.Exec("UPDATE deploy_pods SET pod_created_at = ? WHERE pod_uid = ?", base.Add(time.Duration(index)*time.Minute), uid)
		pod.PodCreatedAt = base.Add(time.Duration(index) * time.Minute)
		if _, ensureError := logRepository.EnsureStream(ctx, pod); ensureError != nil {
			testingT.Fatal(ensureError)
		}
		if index != 3 {
			_ = logRepository.AppendLines(ctx, uid, logLines(int64(index*100), 1), model.ResumePosition{NS: int64(index * 100), Count: 1})
		}
	}
	// 別の deploy のストリーム(行あり)は、載らない。
	database.Create(newTestDeploy(2, "project-00000001", "user-a"))
	otherPod := insertLivePod(testingT, database, "uid-other", 0)
	database.Exec("UPDATE deploy_pods SET deploy_id = 'deploy-00000002' WHERE pod_uid = 'uid-other'")
	otherPod.DeployID = "deploy-00000002"
	_, _ = logRepository.EnsureStream(ctx, otherPod)
	_ = logRepository.AppendLines(ctx, "uid-other", logLines(900, 1), model.ResumePosition{NS: 900, Count: 1})
	// uid-4 は終了している。
	_ = logRepository.FinishStream(ctx, "uid-4")
	// 行のあるストリームが、新しい順(5、4、2、1)。uid-3(行なし)と、他の deploy は載らない。
	streams, listError := queryRepository.ListLogStreams(ctx, "deploy-00000001", 50)
	if listError != nil || len(streams) != 4 || streams[0].PodUID != "uid-5" || streams[1].PodUID != "uid-4" || streams[2].PodUID != "uid-2" || streams[3].PodUID != "uid-1" {
		testingT.Fatalf("行のある4つが、新しい順のはず: %+v %v", streams, listError)
	}
	if streams[1].FinishedAt == nil || streams[0].FinishedAt != nil {
		testingT.Fatalf("終了の時刻が合うはず: %+v", streams)
	}
	// 上限で絞れる。
	if limited, _ := queryRepository.ListLogStreams(ctx, "deploy-00000001", 2); len(limited) != 2 || limited[0].PodUID != "uid-5" {
		testingT.Fatalf("上限で絞れるはず: %+v", limited)
	}
	// 行が全て削除されたストリームは、載らなくなる。
	database.Exec("DELETE FROM deploy_logs WHERE pod_uid = 'uid-5'")
	if afterPrune, _ := queryRepository.ListLogStreams(ctx, "deploy-00000001", 50); len(afterPrune) != 3 || afterPrune[0].PodUID != "uid-4" {
		testingT.Fatalf("行が無くなったストリームは載らないはず: %+v", afterPrune)
	}
	// 空の deploy ID は、エラー。
	if _, emptyError := queryRepository.ListLogStreams(ctx, "", 50); emptyError == nil {
		testingT.Fatal("空の deploy ID は、エラーのはず")
	}
}
