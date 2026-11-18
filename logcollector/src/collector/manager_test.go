package collector

import (
	"context"
	"testing"
	"time"
)

// runManager は、Manager を別の goroutine で動かし、止めて終わるのを待つ関数を返す。
//
// 引数:
//   - environment: テストの環境。
//   - source: 偽のログ取得元。
//   - maxStreams: 同時に追う Pod の数の上限。
//
// 戻り値:
//   - 止めて、終わるのを待つ関数。
func runManager(environment *collectorEnvironment, source LogSource, maxStreams int) func() {
	// 決め直しの間隔を短くして、動かす。
	manager := NewManager(environment.repository, source, ManagerSettings{ReconcileInterval: 10 * time.Millisecond, MaxStreams: maxStreams, Follower: testSettings(), StopTimeout: 5 * time.Second}, environment.logger)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.Run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

// requestedPods は、これまでにログを頼まれた Pod の名前の集合を返す。
//
// 引数:
//   - source: 偽のログ取得元。
//
// 戻り値:
//   - Pod の名前の集合。
func requestedPods(source *scriptedSource) map[string]bool {
	// 頼みから、名前を集める。
	names := map[string]bool{}
	for _, request := range source.recordedRequests() {
		names[request.PodName] = true
	}
	return names
}

// TestManagerFollowsLivePodsAndFinishesRemovedOnes は、deploy_pods にある Pod のログを集め始め(ストリームも作る)、
// Pod が deploy_pods から消えたら、収集を止めて、ストリームを終了にし(保存した行は残る)、
// すでに終了しているストリームは、もう追わないことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestManagerFollowsLivePodsAndFinishesRemovedOnes(testingT *testing.T) {
	// Pod を2つ用意する(deploy_pods だけ。ストリームは、Manager が作る)。
	environment := newCollectorEnvironment(testingT)
	environment.addPod(testingT, "uid-1", 0, "Running")
	environment.addPod(testingT, "uid-2", 0, "Running")
	environment.database.Exec("DELETE FROM deploy_log_streams")
	source := &scriptedSource{byPod: map[string][]scriptedResponse{
		"app-uid-1": {{reader: textReader(kubeLine(1, 0, "from pod 1"))}},
		"app-uid-2": {{reader: textReader(kubeLine(1, 0, "from pod 2"))}},
	}}
	stop := runManager(environment, source, 10)
	defer stop()
	// 両方の Pod が追われ、ストリームが作られて、行が保存される。
	eventually(testingT, "両方の Pod が追われる", func() bool {
		return len(environment.storedLines("uid-1")) == 1 && len(environment.storedLines("uid-2")) == 1
	})
	pods := requestedPods(source)
	if !pods["app-uid-1"] || !pods["app-uid-2"] {
		testingT.Fatalf("両方の Pod のログが頼まれるはず: %v", pods)
	}
	// uid-2 が deploy_pods から消えると、ストリームが終了になる。行は残る。uid-1 は、続く。
	environment.database.Exec("DELETE FROM deploy_pods WHERE pod_uid = 'uid-2'")
	eventually(testingT, "消えた Pod のストリームが終了する", func() bool { return environment.stream("uid-2").FinishedAt != nil })
	if len(environment.storedLines("uid-2")) != 1 || environment.stream("uid-1").FinishedAt != nil {
		testingT.Fatal("保存した行は残り、他の Pod は続くはず")
	}
	// 終了したストリームの Pod が(同じ UID で)現れても、追わない(頼みが増えない)。
	before := len(source.recordedRequests())
	environment.addPodRowOnly(testingT, "uid-2")
	time.Sleep(100 * time.Millisecond)
	for _, request := range source.recordedRequests()[before:] {
		if request.PodName == "app-uid-2" {
			testingT.Fatal("終了したストリームの Pod は、追わないはず")
		}
	}
}

// TestManagerFinishesStaleOpenStreams は、前のリーダーが止まっていた間に消えた Pod の、収集中のままのストリームを、
// 起動時に終了にすることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestManagerFinishesStaleOpenStreams(testingT *testing.T) {
	// Pod を作ってストリームを用意し、deploy_pods から Pod を消す(収集中のストリームだけが残る)。
	environment := newCollectorEnvironment(testingT)
	environment.addPod(testingT, "uid-stale", 0, "Running")
	environment.database.Exec("DELETE FROM deploy_pods WHERE pod_uid = 'uid-stale'")
	stop := runManager(environment, &scriptedSource{}, 10)
	defer stop()
	eventually(testingT, "古いストリームが終了する", func() bool { return environment.stream("uid-stale").FinishedAt != nil })
}

// TestManagerResumesFromSavedPosition は、引き継いだ(起動した)ときに、保存されている読み取り位置から続け、
// 保存済みの行を、重複して保存しないことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestManagerResumesFromSavedPosition(testingT *testing.T) {
	// 前のリーダーが、2行を保存した状態にする。
	environment := newCollectorEnvironment(testingT)
	stream := environment.addPod(testingT, "uid-1", 0, "Running")
	if appendError := environment.repository.AppendLines(context.Background(), "uid-1", logLinesAt(kubeNS(1, 100), "a", "b"), positionAt(kubeNS(1, 101), 1)); appendError != nil {
		testingT.Fatal(appendError)
	}
	_ = stream
	// 新しいリーダーに、同じ2行(と、新しい1行)が届く。
	source := &scriptedSource{responses: []scriptedResponse{{reader: textReader(kubeLine(1, 100, "a"), kubeLine(1, 101, "b"), kubeLine(1, 102, "c"))}}}
	stop := runManager(environment, source, 10)
	defer stop()
	eventually(testingT, "新しい行だけが増える", func() bool { return len(environment.storedLines("uid-1")) == 3 })
	stop()
	if lines := environment.storedLines("uid-1"); lines[0] != "a" || lines[1] != "b" || lines[2] != "c" {
		testingT.Fatalf("重複せず、続きだけが保存されるはず: %v", lines)
	}
}

// TestManagerRespectsMaxStreams は、同時に追う Pod の数が、上限を超えないことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestManagerRespectsMaxStreams(testingT *testing.T) {
	// Pod を3つ用意し、上限を2にする(どの Pod も、つながったまま、行が流れない)。
	environment := newCollectorEnvironment(testingT)
	for _, uid := range []string{"uid-1", "uid-2", "uid-3"} {
		environment.addPod(testingT, uid, 0, "Running")
	}
	source := &scriptedSource{}
	stop := runManager(environment, source, 2)
	defer stop()
	eventually(testingT, "2つが追われる", func() bool { return len(requestedPods(source)) == 2 })
	// しばらく待っても、3つ目は追われない。
	time.Sleep(150 * time.Millisecond)
	if pods := requestedPods(source); len(pods) != 2 {
		testingT.Fatalf("上限の2つだけのはず: %v", pods)
	}
	// 1つの Pod が消えて、空きができると、残りの Pod が追われる。
	for uid := range map[string]bool{"uid-1": true, "uid-2": true, "uid-3": true} {
		if !requestedPods(source)["app-"+uid] {
			environment.database.Exec("DELETE FROM deploy_pods WHERE pod_uid = ?", "uid-1")
			break
		}
	}
	eventually(testingT, "空きができたら、3つ目が追われる", func() bool { return requestedPods(source)["app-uid-3"] })
}

// TestManagerStopWaitsForFollowers は、止める合図で、Follower が、書いていない分を書き終えてから、Manager が終わることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestManagerStopWaitsForFollowers(testingT *testing.T) {
	// 書き込みの間隔を長くして、行を、バッチに溜めたままにする。
	environment := newCollectorEnvironment(testingT)
	environment.addPod(testingT, "uid-1", 0, "Running")
	settings := testSettings()
	settings.FlushInterval = time.Hour
	pipeReader, pipeWriter := newPipe()
	source := &scriptedSource{responses: []scriptedResponse{{reader: pipeReader}}}
	manager := NewManager(environment.repository, source, ManagerSettings{ReconcileInterval: 10 * time.Millisecond, MaxStreams: 10, Follower: settings, StopTimeout: 5 * time.Second}, environment.logger)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.Run(ctx)
	}()
	eventually(testingT, "追い始める", func() bool { return len(source.recordedRequests()) == 1 })
	_, _ = pipeWriter.Write([]byte(kubeLine(1, 0, "pending") + "\n"))
	time.Sleep(50 * time.Millisecond)
	// 止めると、書き切ってから、Manager が終わる。
	cancel()
	<-done
	if lines := environment.storedLines("uid-1"); len(lines) != 1 {
		testingT.Fatalf("止める合図で、書き切るはず: %v", lines)
	}
	_ = pipeWriter.Close()
}
