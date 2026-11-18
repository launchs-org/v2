package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"launchs/shared/model"
)

// newLogTestEnvironment は、使い捨てのデータベースに、プロジェクトと deploy を用意し、DeployLogRepository を返す。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - データベースと、その上の DeployLogRepository。deploy の ID は "deploy-00000001"。
func newLogTestEnvironment(testingT *testing.T) (*gorm.DB, *DeployLogRepository) {
	// プロジェクトと deploy を作る。
	database, _ := newDeployTestEnvironment(testingT)
	if createError := database.Create(newTestDeploy(1, "project-00000001", "user-a")).Error; createError != nil {
		testingT.Fatal(createError)
	}
	return database, NewDeployLogRepository(database)
}

// insertLivePod は、watcher が書く deploy_pods の行を、直接作る。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - database: 保存先。
//   - podUID: Pod の UID。
//   - restartCount: 再起動の回数。
//
// 戻り値:
//   - 作った Pod。
func insertLivePod(testingT *testing.T, database *gorm.DB, podUID string, restartCount int) model.DeployPod {
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()
	// 必須の列を埋めて保存する。
	now := time.Now().Truncate(time.Microsecond)
	pod := model.DeployPod{PodUID: podUID, DeployID: "deploy-00000001", ProjectID: "project-00000001", PodName: "app-" + podUID, Status: "running", Phase: "Running", RestartCount: restartCount, PodCreatedAt: now, ObservedAt: now}
	if createError := database.Create(&pod).Error; createError != nil {
		testingT.Fatal(createError)
	}
	return pod
}

// logLines は、番号つきのログの行を作る。
//
// 引数:
//   - startNS: 最初の行の時刻(ナノ秒)。1行ごとに 1 増える。
//   - count: 行数。
//
// 戻り値:
//   - ログの行。
func logLines(startNS int64, count int) []model.DeployLog {
	// 時刻を増やしながら、行を作る。
	lines := make([]model.DeployLog, 0, count)
	for index := 0; index < count; index++ {
		lines = append(lines, model.DeployLog{LoggedNS: startNS + int64(index), Line: fmt.Sprintf("line-%d", index)})
	}
	return lines
}

// TestDeployLogRepositoryStreamLifecycle は、ストリームの作成(2回呼んでも1つ)、行の保存と位置・大きさの更新、
// 再起動の回数、終了(時刻は最初の1回だけ)、deploy の削除での CASCADE を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployLogRepositoryStreamLifecycle(testingT *testing.T) {
	// deploy と Pod を用意して、ストリームを作る(2回呼んでも、同じ1つ)。
	database, logRepository := newLogTestEnvironment(testingT)
	ctx := context.Background()
	pod := insertLivePod(testingT, database, "uid-1", 0)
	stream, ensureError := logRepository.EnsureStream(ctx, pod)
	if ensureError != nil || stream.PodUID != "uid-1" || stream.DeployID != "deploy-00000001" || stream.FinishedAt != nil || !stream.StartedAt.Equal(pod.PodCreatedAt) {
		testingT.Fatalf("作れるはず: %+v %v", stream, ensureError)
	}
	if _, secondError := logRepository.EnsureStream(ctx, pod); secondError != nil {
		testingT.Fatal(secondError)
	}
	var streamCount int64
	database.Model(&model.DeployLogStream{}).Count(&streamCount)
	if streamCount != 1 {
		testingT.Fatalf("ストリームは1つのはず: %d", streamCount)
	}
	// 一覧に出る(収集中)。
	if open, _ := logRepository.ListOpenStreams(ctx); len(open) != 1 {
		testingT.Fatalf("収集中が1つのはず: %d", len(open))
	}
	// 行を保存すると、位置と大きさが、同じトランザクションで更新される。
	lines := logLines(1000, 3)
	if appendError := logRepository.AppendLines(ctx, "uid-1", lines, model.ResumePosition{NS: 1002, Count: 1}); appendError != nil {
		testingT.Fatalf("保存できるはず: %v", appendError)
	}
	stored, _ := logRepository.EnsureStream(ctx, pod)
	expectedBytes := int64(len("line-0") + len("line-1") + len("line-2"))
	if stored.ResumeNS != 1002 || stored.ResumeCount != 1 || stored.Bytes != expectedBytes || stored.ResumePosition() != (model.ResumePosition{NS: 1002, Count: 1}) {
		testingT.Fatalf("位置と大きさが更新されるはず: %+v", stored)
	}
	var savedLines []model.DeployLog
	database.Order("id").Find(&savedLines)
	if len(savedLines) != 3 || savedLines[0].PodUID != "uid-1" || savedLines[0].Line != "line-0" || savedLines[2].LoggedNS != 1002 {
		testingT.Fatalf("行が順に保存されるはず: %+v", savedLines)
	}
	// 行が無くても、位置は進む(捨てた行の分)。大きさは変わらない。
	if appendError := logRepository.AppendLines(ctx, "uid-1", nil, model.ResumePosition{NS: 1010, Count: 4}); appendError != nil {
		testingT.Fatal(appendError)
	}
	stored, _ = logRepository.EnsureStream(ctx, pod)
	if stored.ResumeNS != 1010 || stored.ResumeCount != 4 || stored.Bytes != expectedBytes {
		testingT.Fatalf("位置だけが進むはず: %+v", stored)
	}
	// 再起動の回数を保存できる。
	if setError := logRepository.SetRestartCount(ctx, "uid-1", 2); setError != nil {
		testingT.Fatal(setError)
	}
	// 終了にできる。時刻は、最初の1回だけ(2回目で変わらない)。
	if finishError := logRepository.FinishStream(ctx, "uid-1"); finishError != nil {
		testingT.Fatal(finishError)
	}
	var finished model.DeployLogStream
	database.Where("pod_uid = ?", "uid-1").Take(&finished)
	if finished.FinishedAt == nil || finished.RestartCount != 2 {
		testingT.Fatalf("終了していて、再起動の回数が保存されているはず: %+v", finished)
	}
	firstFinish := *finished.FinishedAt
	time.Sleep(5 * time.Millisecond)
	_ = logRepository.FinishStream(ctx, "uid-1")
	database.Where("pod_uid = ?", "uid-1").Take(&finished)
	if !finished.FinishedAt.Equal(firstFinish) {
		testingT.Fatal("終了の時刻は、変わらないはず")
	}
	if open, _ := logRepository.ListOpenStreams(ctx); len(open) != 0 {
		testingT.Fatalf("終了したものは、収集中に出ないはず: %d", len(open))
	}
	// deploy を消すと、ストリームも行も、CASCADE で消える。
	database.Exec("DELETE FROM deploys WHERE id = 'deploy-00000001'")
	var remaining int64
	database.Model(&model.DeployLog{}).Count(&remaining)
	database.Model(&model.DeployLogStream{}).Count(&streamCount)
	if remaining != 0 || streamCount != 0 {
		testingT.Fatalf("CASCADE で消えるはず: 行 %d、ストリーム %d", remaining, streamCount)
	}
}

// TestDeployLogRepositoryGone は、deploy が無いときのストリームの作成と、ストリームが無いときの保存が、ErrLogStreamGone になることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployLogRepositoryGone(testingT *testing.T) {
	// deploy が無い Pod は、ストリームを作れない。
	_, logRepository := newLogTestEnvironment(testingT)
	ctx := context.Background()
	orphan := model.DeployPod{PodUID: "uid-orphan", DeployID: "deploy-nothing", ProjectID: "project-00000001", PodName: "x", PodCreatedAt: time.Now()}
	if _, ensureError := logRepository.EnsureStream(ctx, orphan); !errors.Is(ensureError, ErrLogStreamGone) {
		testingT.Fatalf("ErrLogStreamGone のはず: %v", ensureError)
	}
	// ストリームが無いところへの保存は、行も位置も、保存されず、ErrLogStreamGone。
	if appendError := logRepository.AppendLines(ctx, "uid-nothing", logLines(1, 2), model.ResumePosition{NS: 2, Count: 1}); !errors.Is(appendError, ErrLogStreamGone) {
		testingT.Fatalf("ErrLogStreamGone のはず: %v", appendError)
	}
	if appendError := logRepository.AppendLines(ctx, "uid-nothing", nil, model.ResumePosition{NS: 2, Count: 1}); !errors.Is(appendError, ErrLogStreamGone) {
		testingT.Fatalf("行が無くても ErrLogStreamGone のはず: %v", appendError)
	}
}

// TestDeployLogRepositoryLivePods は、deploy_pods の読み取り(一覧と、1件の検索)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployLogRepositoryLivePods(testingT *testing.T) {
	// Pod を2つ用意する。
	database, logRepository := newLogTestEnvironment(testingT)
	ctx := context.Background()
	insertLivePod(testingT, database, "uid-1", 0)
	insertLivePod(testingT, database, "uid-2", 3)
	// 一覧に2つ出る。1件の検索は、あれば見つかり、無ければ見つからない。
	if pods, listError := logRepository.ListLivePods(ctx); listError != nil || len(pods) != 2 {
		testingT.Fatalf("2つのはず: %d %v", len(pods), listError)
	}
	if pod, found, findError := logRepository.FindLivePod(ctx, "uid-2"); findError != nil || !found || pod.RestartCount != 3 {
		testingT.Fatalf("見つかるはず: %+v %v %v", pod, found, findError)
	}
	if _, found, _ := logRepository.FindLivePod(ctx, "uid-nothing"); found {
		testingT.Fatal("見つからないはず")
	}
	if _, found, _ := logRepository.FindLivePod(ctx, ""); found {
		testingT.Fatal("空の UID は、見つからないはず")
	}
}

// TestDeployLogRepositoryPrune は、保存期間を過ぎた行の削除、Pod ごとの上限を超えた古い行の削除(新しい行は残る)、
// 大きさ(bytes)が削除に合わせて減ること、終了して保存期間を過ぎたストリームの行ごとの削除、他の Pod に影響しないことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployLogRepositoryPrune(testingT *testing.T) {
	// 2つの Pod にログを入れる。uid-1: 古い行3つ + 新しい行10つ(1行 10 バイト)、uid-2: 新しい行2つ。
	database, logRepository := newLogTestEnvironment(testingT)
	ctx := context.Background()
	now := time.Now()
	cutoff := now.Add(-7 * 24 * time.Hour)
	for _, uid := range []string{"uid-1", "uid-2"} {
		if _, ensureError := logRepository.EnsureStream(ctx, insertLivePod(testingT, database, uid, 0)); ensureError != nil {
			testingT.Fatal(ensureError)
		}
	}
	tenBytes := func(prefix string, startNS int64, count int) []model.DeployLog {
		lines := make([]model.DeployLog, 0, count)
		for index := 0; index < count; index++ {
			lines = append(lines, model.DeployLog{LoggedNS: startNS + int64(index), Line: fmt.Sprintf("%s%07d", prefix, index)[:10]})
		}
		return lines
	}
	oldNS, newNS := cutoff.Add(-time.Hour).UnixNano(), now.UnixNano()
	_ = logRepository.AppendLines(ctx, "uid-1", tenBytes("old-", oldNS, 3), model.ResumePosition{NS: oldNS + 2, Count: 1})
	_ = logRepository.AppendLines(ctx, "uid-1", tenBytes("new-", newNS, 10), model.ResumePosition{NS: newNS + 9, Count: 1})
	_ = logRepository.AppendLines(ctx, "uid-2", tenBytes("two-", newNS, 2), model.ResumePosition{NS: newNS + 1, Count: 1})
	// 保存期間: 古い3行だけが消え、大きさが減る。上限が大きければ、他は消えない。
	result, pruneError := logRepository.PruneLogs(ctx, cutoff, 1000)
	if pruneError != nil || result.ExpiredLines != 3 || result.OverflowLines != 0 || result.DeletedStreams != 0 {
		testingT.Fatalf("古い3行だけが消えるはず: %+v %v", result, pruneError)
	}
	var first model.DeployLogStream
	database.Where("pod_uid = ?", "uid-1").Take(&first)
	if first.Bytes != 100 {
		testingT.Fatalf("大きさは 100 になるはず: %d", first.Bytes)
	}
	// 上限: uid-1 を 45 バイトまでにすると、新しい行から数えて 4 行(40 バイト)が残り、古い 6 行が消える。uid-2(20 バイト)は影響されない。
	result, pruneError = logRepository.PruneLogs(ctx, cutoff, 45)
	if pruneError != nil || result.OverflowLines != 6 || result.ExpiredLines != 0 {
		testingT.Fatalf("古い6行が消えるはず: %+v %v", result, pruneError)
	}
	var remaining []model.DeployLog
	database.Where("pod_uid = ?", "uid-1").Order("id").Find(&remaining)
	if len(remaining) != 4 || remaining[0].LoggedNS != newNS+6 || remaining[3].LoggedNS != newNS+9 {
		testingT.Fatalf("新しい4行が残るはず: %+v", remaining)
	}
	database.Where("pod_uid = ?", "uid-1").Take(&first)
	var second model.DeployLogStream
	database.Where("pod_uid = ?", "uid-2").Take(&second)
	if first.Bytes != 40 || second.Bytes != 20 {
		testingT.Fatalf("大きさが合うはず: uid-1=%d uid-2=%d", first.Bytes, second.Bytes)
	}
	// 上限に収まっていれば、何も消えない。
	if again, _ := logRepository.PruneLogs(ctx, cutoff, 45); again.OverflowLines != 0 || again.ExpiredLines != 0 {
		testingT.Fatalf("何も消えないはず: %+v", again)
	}
	// 終了して保存期間を過ぎたストリームは、行ごと消える。終了が新しいものは残る。
	database.Exec("UPDATE deploy_log_streams SET finished_at = ? WHERE pod_uid = 'uid-1'", cutoff.Add(-time.Hour))
	database.Exec("UPDATE deploy_log_streams SET finished_at = ? WHERE pod_uid = 'uid-2'", now)
	result, _ = logRepository.PruneLogs(ctx, cutoff, 1000)
	var streamUIDs []string
	database.Model(&model.DeployLogStream{}).Pluck("pod_uid", &streamUIDs)
	var lineCount int64
	database.Model(&model.DeployLog{}).Count(&lineCount)
	if result.DeletedStreams != 1 || strings.Join(streamUIDs, ",") != "uid-2" || lineCount != 2 {
		testingT.Fatalf("uid-1 だけが、行ごと消えるはず: %+v %v 行 %d", result, streamUIDs, lineCount)
	}
}

// TestDeployLogRepositoryPruneKeepsBytesConsistentWithAppend は、削除と書き込みが同時に起きても、大きさ(bytes)が、
// 実際の行の合計と一致することを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployLogRepositoryPruneKeepsBytesConsistentWithAppend(testingT *testing.T) {
	// ストリームを作る。
	database, logRepository := newLogTestEnvironment(testingT)
	ctx := context.Background()
	if _, ensureError := logRepository.EnsureStream(ctx, insertLivePod(testingT, database, "uid-1", 0)); ensureError != nil {
		testingT.Fatal(ensureError)
	}
	// 書き込みと削除を、交互に何度も行う(上限は小さく、削除が毎回働く)。
	done := make(chan struct{})
	go func() {
		defer close(done)
		for round := 0; round < 40; round++ {
			_ = logRepository.AppendLines(ctx, "uid-1", logLines(int64(round*100)+1, 20), model.ResumePosition{NS: int64(round*100) + 20, Count: 1})
		}
	}()
	for round := 0; round < 40; round++ {
		_, _ = logRepository.PruneLogs(ctx, time.Unix(0, 0), 200)
	}
	<-done
	// 実際の行の合計と、bytes が一致する。
	var actual, recorded int64
	database.Raw("SELECT COALESCE(sum(octet_length(line)), 0) FROM deploy_logs WHERE pod_uid = 'uid-1'").Scan(&actual)
	database.Raw("SELECT bytes FROM deploy_log_streams WHERE pod_uid = 'uid-1'").Scan(&recorded)
	if actual != recorded {
		testingT.Fatalf("bytes と実際の合計が一致するはず: bytes=%d 実際=%d", recorded, actual)
	}
}
