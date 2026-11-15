package collector

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"launchs/shared/database/testdatabase"
	"launchs/shared/logger"
	"launchs/shared/model"
	"launchs/shared/repository"
)

// scriptedResponse は、偽の LogSource が、1回の Open に返すもの。
type scriptedResponse struct {
	// reader は、返す読み取り口(nil なら、error を返す)。
	reader io.ReadCloser

	// err は、Open が返すエラー。
	err error
}

// scriptedSource は、テスト用の LogSource。Open の呼び出しごとに、用意した応答を、順に返す。
// 用意した応答が尽きたら、つながったまま、何も流れない読み取り口を返す(止める合図まで待つ)。
type scriptedSource struct {
	mutex     sync.Mutex
	responses []scriptedResponse
	// byPod は、Pod の名前ごとの応答。その Pod の頼みには、responses より先に、これを順に返す。
	byPod    map[string][]scriptedResponse
	requests []LogRequest
}

// Open は、次の応答を返し、頼みを記録する。
//
// 引数:
//   - ctx: キャンセルを伝える context(応答が尽きたときの、待ちの読み取り口が使う)。
//   - request: 取得の頼み。
//
// 戻り値:
//   - 読み取り口、またはエラー。
func (source *scriptedSource) Open(ctx context.Context, request LogRequest) (io.ReadCloser, error) {
	// 頼みを記録して、次の応答を取り出す。
	source.mutex.Lock()
	defer source.mutex.Unlock()
	source.requests = append(source.requests, request)
	if queue := source.byPod[request.PodName]; len(queue) > 0 {
		source.byPod[request.PodName] = queue[1:]
		if queue[0].err != nil {
			return nil, queue[0].err
		}
		return queue[0].reader, nil
	}
	if len(source.responses) == 0 {
		return newBlockingReader(ctx), nil
	}
	response := source.responses[0]
	source.responses = source.responses[1:]
	if response.err != nil {
		return nil, response.err
	}
	return response.reader, nil
}

// recordedRequests は、これまでの頼みの、コピーを返す。
//
// 戻り値:
//   - 頼みの一覧。
func (source *scriptedSource) recordedRequests() []LogRequest {
	// ロックして、コピーする。
	source.mutex.Lock()
	defer source.mutex.Unlock()
	return append([]LogRequest(nil), source.requests...)
}

// newBlockingReader は、何も流れず、ctx が終わるか、閉じられるまで、読み取りを待つ読み取り口を作る。
//
// 引数:
//   - ctx: キャンセルを伝える context。
//
// 戻り値:
//   - 読み取り口。
func newBlockingReader(ctx context.Context) io.ReadCloser {
	// パイプの書き込み側を、ctx が終わったら閉じる。
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		<-ctx.Done()
		_ = pipeWriter.Close()
	}()
	return pipeReader
}

// textReader は、行を連結した文字列を、読み切ったら EOF になる読み取り口にする。
//
// 引数:
//   - lines: 行(改行は、ここで付ける)。
//
// 戻り値:
//   - 読み取り口。
func textReader(lines ...string) io.ReadCloser {
	// 各行に改行を付けて、つなげる。
	return io.NopCloser(strings.NewReader(strings.Join(lines, "\n") + "\n"))
}

// kubeLine は、k8s のログの1行(時刻つき)を作る。
//
// 引数:
//   - second: 時刻の秒(2026-11-15T12:00:<second>)。
//   - nanosecond: 時刻のナノ秒の部分。
//   - body: 本文。
//
// 戻り値:
//   - 「<RFC3339Nano の時刻> <本文>」の行。
func kubeLine(second int, nanosecond int, body string) string {
	// 時刻を作って、本文を付ける。
	timestamp := time.Date(2026, 11, 15, 12, 0, second, nanosecond, time.UTC)
	return timestamp.Format(time.RFC3339Nano) + " " + body
}

// kubeNS は、kubeLine と同じ時刻を、unix ナノ秒で返す。
//
// 引数:
//   - second: 時刻の秒。
//   - nanosecond: 時刻のナノ秒の部分。
//
// 戻り値:
//   - unix ナノ秒。
func kubeNS(second int, nanosecond int) int64 {
	// 同じ時刻を作って、ナノ秒にする。
	return time.Date(2026, 11, 15, 12, 0, second, nanosecond, time.UTC).UnixNano()
}

// testSettings は、テスト用の、速い Follower の設定を返す。
//
// 戻り値:
//   - Follower の設定(待ち時間は短く、書き込みの間隔も短い)。
func testSettings() FollowerSettings {
	// 待ち時間を、ミリ秒にする。
	return FollowerSettings{
		MaxLineBytes: 1024, IngestBytesPerSecond: 1 << 20, BatchLines: 500, FlushInterval: 10 * time.Millisecond,
		MinBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond, StableAfter: time.Hour, Now: time.Now,
	}
}

// collectorEnvironment は、収集のテストに必要なものの一式。
type collectorEnvironment struct {
	database   *gorm.DB
	repository *repository.DeployLogRepository
	logger     *slog.Logger
}

// newCollectorEnvironment は、使い捨てのデータベースに、プロジェクトと deploy を用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - テストの環境。deploy の ID は "deploy-00000001"。
func newCollectorEnvironment(testingT *testing.T) *collectorEnvironment {
	// データベースに、プロジェクトと deploy を作る。
	database := testdatabase.New(testingT)
	project := model.Project{ID: "project-00000001", UserID: "user-a", Name: "my-app", Status: model.ProjectStatusActive}
	deploy := model.Deploy{ID: "deploy-00000001", ProjectID: project.ID, UserID: "user-a", ImageRef: "nginx", Spec: "small", Replicas: 1, Status: model.DeployStatusActive, Env: datatypes.JSON(`{}`)}
	if createError := database.Create(&project).Error; createError != nil {
		testingT.Fatal(createError)
	}
	if createError := database.Create(&deploy).Error; createError != nil {
		testingT.Fatal(createError)
	}
	return &collectorEnvironment{database: database, repository: repository.NewDeployLogRepository(database), logger: logger.New(&bytes.Buffer{}, "logcollector", slog.LevelInfo)}
}

// addPod は、deploy_pods に Pod を足して、ストリームを作る。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - podUID: Pod の UID。
//   - restartCount: 再起動の回数。
//   - phase: Pod の phase(Running など)。
//
// 戻り値:
//   - 作ったストリーム。
func (environment *collectorEnvironment) addPod(testingT *testing.T, podUID string, restartCount int, phase string) model.DeployLogStream {
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()
	// deploy_pods に保存して、ストリームを作る。
	now := time.Now().Truncate(time.Microsecond)
	pod := model.DeployPod{PodUID: podUID, DeployID: "deploy-00000001", ProjectID: "project-00000001", PodName: "app-" + podUID, Status: "running", Phase: phase, RestartCount: restartCount, PodCreatedAt: now, ObservedAt: now}
	if createError := environment.database.Create(&pod).Error; createError != nil {
		testingT.Fatal(createError)
	}
	stream, ensureError := environment.repository.EnsureStream(context.Background(), pod)
	if ensureError != nil {
		testingT.Fatal(ensureError)
	}
	return stream
}

// storedLines は、Pod の保存済みの行の本文を、順に返す。
//
// 引数:
//   - podUID: Pod の UID。
//
// 戻り値:
//   - 本文の一覧。
func (environment *collectorEnvironment) storedLines(podUID string) []string {
	// id の順に読む。
	var lines []model.DeployLog
	environment.database.Where("pod_uid = ?", podUID).Order("id").Find(&lines)
	bodies := make([]string, 0, len(lines))
	for _, line := range lines {
		bodies = append(bodies, line.Line)
	}
	return bodies
}

// stream は、ストリームの現在の値を返す。
//
// 引数:
//   - podUID: Pod の UID。
//
// 戻り値:
//   - ストリーム。
func (environment *collectorEnvironment) stream(podUID string) model.DeployLogStream {
	// 読む。
	var stream model.DeployLogStream
	environment.database.Where("pod_uid = ?", podUID).Take(&stream)
	return stream
}

// eventually は、条件が満たされるまで(最大 5 秒)、待つ。満たされなければ、テストを失敗させる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - description: 待つものの説明(失敗のメッセージ用)。
//   - condition: 条件。
func eventually(testingT *testing.T, description string, condition func() bool) {
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()
	// 条件を、短い間隔で確かめる。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	testingT.Fatalf("条件が満たされなかった: %s", description)
}

// runFollower は、Follower を別の goroutine で動かし、止める関数(終わるのを待つ)を返す。
//
// 引数:
//   - environment: テストの環境。
//   - source: 偽のログ取得元。
//   - settings: Follower の設定。
//   - stream: 追うストリーム。
//
// 戻り値:
//   - 止めて、終わるのを待つ関数。Follower が終わったら閉じる、終了のチャネル。
func runFollower(environment *collectorEnvironment, source LogSource, settings FollowerSettings, stream model.DeployLogStream) (func(), <-chan struct{}) {
	// Follower を動かす。
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		NewFollower(environment.repository, source, settings, environment.logger, stream).Run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}, done
}

// addPodRowOnly は、deploy_pods に Pod の行だけを足す(ストリームは作らない)。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - podUID: Pod の UID。
func (environment *collectorEnvironment) addPodRowOnly(testingT *testing.T, podUID string) {
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()
	// deploy_pods に保存する。
	now := time.Now().Truncate(time.Microsecond)
	pod := model.DeployPod{PodUID: podUID, DeployID: "deploy-00000001", ProjectID: "project-00000001", PodName: "app-" + podUID, Status: "running", Phase: "Running", PodCreatedAt: now, ObservedAt: now}
	if createError := environment.database.Create(&pod).Error; createError != nil {
		testingT.Fatal(createError)
	}
}

// logLinesAt は、同じ時刻から1ずつ増える時刻の、保存用の行を作る。
//
// 引数:
//   - startNS: 最初の行の時刻(ナノ秒)。
//   - bodies: 本文。
//
// 戻り値:
//   - 保存用の行。
func logLinesAt(startNS int64, bodies ...string) []model.DeployLog {
	// 時刻を増やしながら作る。
	lines := make([]model.DeployLog, 0, len(bodies))
	for index, body := range bodies {
		lines = append(lines, model.DeployLog{LoggedNS: startNS + int64(index), Line: body})
	}
	return lines
}

// positionAt は、読み取り位置を作る。
//
// 引数:
//   - ns: 時刻(ナノ秒)。
//   - count: その時刻の行数。
//
// 戻り値:
//   - 読み取り位置。
func positionAt(ns int64, count int) model.ResumePosition {
	// 値を持たせて返す。
	return model.ResumePosition{NS: ns, Count: count}
}

// newPipe は、書き込み側と読み取り側のパイプを作る(読み取り側は、scriptedResponse に使える)。
//
// 戻り値:
//   - 読み取り側、書き込み側。
func newPipe() (*io.PipeReader, *io.PipeWriter) {
	// 標準のパイプを返す。
	return io.Pipe()
}
