package collector

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"launchs/shared/model"
)

// TestFollowerStoresLinesAndResumesWithoutDuplicates は、行が保存され、読み取り位置が進み、
// つなぎ直したときに、同じ秒の行が、また届いても、重複せず、新しい行だけが保存されることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestFollowerStoresLinesAndResumesWithoutDuplicates(testingT *testing.T) {
	// Pod を用意し、1回目のつながりは3行で切れ、2回目は、同じ3行に、新しい1行が付いて届く。
	environment := newCollectorEnvironment(testingT)
	stream := environment.addPod(testingT, "uid-1", 0, "Running")
	source := &scriptedSource{responses: []scriptedResponse{
		{reader: textReader(kubeLine(1, 100, "a"), kubeLine(1, 200, "b"), kubeLine(1, 200, "c"))},
		{reader: textReader(kubeLine(1, 100, "a"), kubeLine(1, 200, "b"), kubeLine(1, 200, "c"), kubeLine(1, 300, "d"))},
	}}
	stop, _ := runFollower(environment, source, testSettings(), stream)
	defer stop()
	// 4行が、順に、1回ずつ保存される。
	eventually(testingT, "4行が保存される", func() bool { return len(environment.storedLines("uid-1")) == 4 })
	stop()
	if lines := strings.Join(environment.storedLines("uid-1"), ","); lines != "a,b,c,d" {
		testingT.Fatalf("重複せず、順に保存されるはず: %s", lines)
	}
	// 読み取り位置は、最後の行(時刻が 300 の1行目)。大きさは、4行の長さ。
	saved := environment.stream("uid-1")
	if saved.ResumeNS != kubeNS(1, 300) || saved.ResumeCount != 1 || saved.Bytes != 4 {
		testingT.Fatalf("位置と大きさが合うはず: %+v", saved)
	}
	// 2回目の頼みは、1回目の位置の秒(sinceTime)から。どちらも、app コンテナの follow。
	requests := source.recordedRequests()
	if len(requests) < 2 || !requests[0].SinceTime.IsZero() || !requests[1].SinceTime.Equal(time.Date(2026, 11, 15, 12, 0, 1, 0, time.UTC)) {
		testingT.Fatalf("2回目は、位置の秒から頼むはず: %+v", requests)
	}
	if requests[0].Container != "app" || !requests[0].Follow || requests[0].Previous || requests[0].Namespace != "project-00000001" || requests[0].PodName != "app-uid-1" {
		testingT.Fatalf("頼みの内容が違う: %+v", requests[0])
	}
}

// TestFollowerFlushesByInterval は、行が少なくても、書き込みの間隔で保存されることを確かめる(読み取りが待っていても、書ける)。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestFollowerFlushesByInterval(testingT *testing.T) {
	// つながったままのストリームを用意する。
	environment := newCollectorEnvironment(testingT)
	stream := environment.addPod(testingT, "uid-1", 0, "Running")
	pipeReader, pipeWriter := io.Pipe()
	source := &scriptedSource{responses: []scriptedResponse{{reader: pipeReader}}}
	stop, _ := runFollower(environment, source, testSettings(), stream)
	defer stop()
	// 1行だけ流すと、続きが無くても、間隔で保存される。
	if _, writeError := pipeWriter.Write([]byte(kubeLine(2, 0, "only one line") + "\n")); writeError != nil {
		testingT.Fatal(writeError)
	}
	eventually(testingT, "1行が、間隔で保存される", func() bool { return len(environment.storedLines("uid-1")) == 1 })
	// 次の1行も、同じ。
	_, _ = pipeWriter.Write([]byte(kubeLine(3, 0, "second") + "\n"))
	eventually(testingT, "2行目も保存される", func() bool { return len(environment.storedLines("uid-1")) == 2 })
	_ = pipeWriter.Close()
}

// TestFollowerReadsPreviousContainerAfterRestart は、再起動の回数が増えていると、先に前のコンテナのログを読み
// (follow ではない)、そのあと、いまのコンテナを follow すること、再起動の回数が保存されることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestFollowerReadsPreviousContainerAfterRestart(testingT *testing.T) {
	// 再起動が1回ある Pod(ストリームは、まだ0回までしか処理していない)。
	environment := newCollectorEnvironment(testingT)
	stream := environment.addPod(testingT, "uid-1", 1, "Running")
	source := &scriptedSource{responses: []scriptedResponse{
		{reader: textReader(kubeLine(1, 0, "last words of the crashed container"))},
		{reader: textReader(kubeLine(5, 0, "new container started"))},
	}}
	stop, _ := runFollower(environment, source, testSettings(), stream)
	defer stop()
	eventually(testingT, "2行が保存される", func() bool { return len(environment.storedLines("uid-1")) == 2 })
	stop()
	// 前のコンテナの出力が、先に保存される。
	if lines := strings.Join(environment.storedLines("uid-1"), "|"); lines != "last words of the crashed container|new container started" {
		testingT.Fatalf("前のコンテナの出力が先のはず: %s", lines)
	}
	requests := source.recordedRequests()
	if len(requests) < 2 || !requests[0].Previous || requests[0].Follow || requests[1].Previous || !requests[1].Follow {
		testingT.Fatalf("1回目が previous、2回目が follow のはず: %+v", requests)
	}
	if environment.stream("uid-1").RestartCount != 1 {
		testingT.Fatalf("再起動の回数が保存されるはず: %d", environment.stream("uid-1").RestartCount)
	}
	// 同じ再起動の回数なら、もう previous は読まない(すでに先へ進んでいる)。
	for _, request := range requests[2:] {
		if request.Previous {
			testingT.Fatalf("処理済みの再起動では、previous は読まないはず: %+v", request)
		}
	}
}

// TestFollowerPreviousContainerMissingIsNotAnError は、前のコンテナが無い(まだ読めない)ときは、
// エラーにせず、処理済みにして、いまのコンテナを読むことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestFollowerPreviousContainerMissingIsNotAnError(testingT *testing.T) {
	// previous の取得が、読めない(ErrLogUnavailable)を返す。
	environment := newCollectorEnvironment(testingT)
	stream := environment.addPod(testingT, "uid-1", 2, "Running")
	source := &scriptedSource{responses: []scriptedResponse{{err: ErrLogUnavailable}, {reader: textReader(kubeLine(1, 0, "current"))}}}
	stop, _ := runFollower(environment, source, testSettings(), stream)
	defer stop()
	eventually(testingT, "いまのコンテナの行が保存される", func() bool { return len(environment.storedLines("uid-1")) == 1 })
	stop()
	if environment.stream("uid-1").RestartCount != 2 {
		testingT.Fatalf("処理済みになるはず: %d", environment.stream("uid-1").RestartCount)
	}
}

// TestFollowerFinishesWhenPodIsGone は、k8s に Pod が無い(ErrPodGone)、deploy_pods から消えた、コンテナが終了した Pod(Succeeded)の
// それぞれで、ストリームを終了にして、Follower が終わることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestFollowerFinishesWhenPodIsGone(testingT *testing.T) {
	// 1. k8s に Pod が無い。
	environment := newCollectorEnvironment(testingT)
	stream := environment.addPod(testingT, "uid-gone", 0, "Running")
	_, done := runFollower(environment, &scriptedSource{responses: []scriptedResponse{{err: ErrPodGone}}}, testSettings(), stream)
	eventually(testingT, "k8s に無い Pod は終了", func() bool { return environment.stream("uid-gone").FinishedAt != nil })
	<-done
	// 2. deploy_pods から消えた(次のつなぎ直しで気づく)。
	second := environment.addPod(testingT, "uid-removed", 0, "Running")
	source := &scriptedSource{responses: []scriptedResponse{{reader: textReader(kubeLine(1, 0, "bye"))}}}
	environment.database.Exec("DELETE FROM deploy_pods WHERE pod_uid = 'uid-removed'")
	_, secondDone := runFollower(environment, source, testSettings(), second)
	eventually(testingT, "deploy_pods に無い Pod は終了", func() bool { return environment.stream("uid-removed").FinishedAt != nil })
	<-secondDone
	// 3. コンテナが終了した Pod(Succeeded): 読み切ったら、終了にして、終わる。
	third := environment.addPod(testingT, "uid-done", 0, "Succeeded")
	thirdSource := &scriptedSource{responses: []scriptedResponse{{reader: textReader(kubeLine(1, 0, "finished work"))}}}
	_, thirdDone := runFollower(environment, thirdSource, testSettings(), third)
	eventually(testingT, "Succeeded の Pod は、読み切って終了", func() bool { return environment.stream("uid-done").FinishedAt != nil })
	<-thirdDone
	if lines := environment.storedLines("uid-done"); len(lines) != 1 || lines[0] != "finished work" {
		testingT.Fatalf("読み切った行が保存されているはず: %v", lines)
	}
}

// TestFollowerStopsWhenStreamIsGone は、deploy が削除されて(ストリームが CASCADE で消えて)、保存できなくなったとき、
// 収集を終えることを確かめる(エラーで、つなぎ直し続けない)。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestFollowerStopsWhenStreamIsGone(testingT *testing.T) {
	// deploy を消してから、行が届く。
	environment := newCollectorEnvironment(testingT)
	stream := environment.addPod(testingT, "uid-1", 0, "Running")
	environment.database.Exec("DELETE FROM deploys WHERE id = 'deploy-00000001'")
	source := &scriptedSource{responses: []scriptedResponse{{reader: textReader(kubeLine(1, 0, "orphan line"))}}}
	_, done := runFollower(environment, source, testSettings(), stream)
	// ストリームが無いので、保存に失敗して、終わる。
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		testingT.Fatal("Follower が終わるはず")
	}
}

// TestFollowerRetriesOnOpenError は、取得の失敗(一時的なエラー)のあと、つなぎ直して、続きを保存することを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestFollowerRetriesOnOpenError(testingT *testing.T) {
	// 1回目は失敗、2回目は成功。
	environment := newCollectorEnvironment(testingT)
	stream := environment.addPod(testingT, "uid-1", 0, "Running")
	source := &scriptedSource{responses: []scriptedResponse{{err: errors.New("connection refused")}, {reader: textReader(kubeLine(1, 0, "after retry"))}}}
	stop, _ := runFollower(environment, source, testSettings(), stream)
	defer stop()
	eventually(testingT, "やり直して保存される", func() bool { return len(environment.storedLines("uid-1")) == 1 })
}

// TestFollowerSanitizesAndTruncatesLines は、NUL の除去、不正な UTF-8 の置き換え、長い行の切り詰め、空行の保存、
// 時刻を読めない行(直前の時刻で保存)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestFollowerSanitizesAndTruncatesLines(testingT *testing.T) {
	// 整える対象の行を並べる(長い行は、上限(1024)の何倍もある)。
	environment := newCollectorEnvironment(testingT)
	stream := environment.addPod(testingT, "uid-1", 0, "Running")
	veryLong := strings.Repeat("x", 200000)
	source := &scriptedSource{responses: []scriptedResponse{{reader: textReader(
		kubeLine(1, 1, "nul\x00byte"), kubeLine(1, 2, "bad\xffutf8"), kubeLine(1, 3, veryLong), kubeLine(1, 4, ""), "garbage without timestamp", kubeLine(1, 5, "after"),
	)}}}
	stop, _ := runFollower(environment, source, testSettings(), stream)
	defer stop()
	eventually(testingT, "6行が保存される", func() bool { return len(environment.storedLines("uid-1")) == 6 })
	stop()
	lines := environment.storedLines("uid-1")
	if lines[0] != "nulbyte" || lines[1] != "bad�utf8" || lines[3] != "" || lines[5] != "after" {
		testingT.Fatalf("整った行のはず: %q", lines)
	}
	if !strings.HasSuffix(lines[2], model.LogTruncatedSuffix) || len(lines[2]) != 1024+len(model.LogTruncatedSuffix) {
		testingT.Fatalf("長い行は切り詰められるはず: %d バイト", len(lines[2]))
	}
	// 時刻を読めない行は、全体が本文で、直前の行の時刻で保存され、位置は、それに引きずられない。
	if lines[4] != "garbage without timestamp" {
		testingT.Fatalf("時刻の無い行も保存されるはず: %q", lines[4])
	}
	var garbage model.DeployLog
	environment.database.Where("pod_uid = ? AND line = ?", "uid-1", "garbage without timestamp").Take(&garbage)
	if garbage.LoggedNS != kubeNS(1, 4) {
		testingT.Fatalf("直前の行の時刻で保存されるはず: %d", garbage.LoggedNS)
	}
	if environment.stream("uid-1").ResumeNS != kubeNS(1, 5) {
		testingT.Fatalf("位置は、最後の時刻つきの行のはず: %d", environment.stream("uid-1").ResumeNS)
	}
}

// TestFollowerDropsLinesOverIngestLimit は、書き込みの速さの上限を超えた行が捨てられ、捨てた数が、印の行で残り、
// 位置は、捨てた行の分も進む(同じ行を、またつなぎ直しで読まない)ことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestFollowerDropsLinesOverIngestLimit(testingT *testing.T) {
	// 1秒に 25 バイトまで。時計は止める(10 バイトの行が 10 行、同じ窓に入る)。
	environment := newCollectorEnvironment(testingT)
	stream := environment.addPod(testingT, "uid-1", 0, "Running")
	settings := testSettings()
	settings.IngestBytesPerSecond = 25
	fixedNow := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	settings.Now = func() time.Time { return fixedNow }
	lines := make([]string, 0, 10)
	for index := 0; index < 10; index++ {
		lines = append(lines, kubeLine(1, index+1, "0123456789"))
	}
	source := &scriptedSource{responses: []scriptedResponse{{reader: textReader(lines...)}}}
	stop, _ := runFollower(environment, source, settings, stream)
	defer stop()
	// 通るのは 2 行(20 バイト)。残り 8 行は捨てられ、ストリームの終わりに、印の行が1行残る。
	eventually(testingT, "印の行が残る", func() bool {
		stored := environment.storedLines("uid-1")
		return len(stored) == 3 && strings.Contains(stored[2], "8 行を捨てました")
	})
	stop()
	if environment.stream("uid-1").ResumeNS != kubeNS(1, 10) {
		testingT.Fatalf("捨てた行の分も、位置が進むはず: %d", environment.stream("uid-1").ResumeNS)
	}
}

// TestFollowerStopFlushesPendingBatch は、止める合図のとき、まだ書いていない行を、書いてから終わることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestFollowerStopFlushesPendingBatch(testingT *testing.T) {
	// 書き込みの間隔を長くして、行が、バッチに溜まったままの状態にする。
	environment := newCollectorEnvironment(testingT)
	stream := environment.addPod(testingT, "uid-1", 0, "Running")
	settings := testSettings()
	settings.FlushInterval = time.Hour
	pipeReader, pipeWriter := io.Pipe()
	source := &scriptedSource{responses: []scriptedResponse{{reader: pipeReader}}}
	stop, _ := runFollower(environment, source, settings, stream)
	if _, writeError := pipeWriter.Write([]byte(kubeLine(1, 0, "pending") + "\n")); writeError != nil {
		testingT.Fatal(writeError)
	}
	time.Sleep(50 * time.Millisecond)
	if len(environment.storedLines("uid-1")) != 0 {
		testingT.Fatal("まだ書かれていないはず")
	}
	// 止めると、書いてから終わる。
	stop()
	if lines := environment.storedLines("uid-1"); len(lines) != 1 || lines[0] != "pending" {
		testingT.Fatalf("止める合図で、書き切るはず: %v", lines)
	}
	_ = pipeWriter.Close()
}
