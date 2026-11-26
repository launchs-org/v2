package runner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"launchs/shared/credential"
	"launchs/shared/database/testdatabase"
	"launchs/shared/exportsend"
	"launchs/shared/googledrive"
	"launchs/shared/googledrive/fakegoogle"
	"launchs/shared/model"
	"launchs/shared/repository"
	"launchs/shared/safehttp"
)

// receiver は、試験用の webhook の受け口(受け取った本文を覚える。status を変えて、失敗を再現できる)。
type receiver struct {
	mutex  sync.Mutex
	server *httptest.Server
	status int
	bodies []string
}

// newReceiver は、受け口を立ち上げる。
func newReceiver(testingT *testing.T) *receiver {
	// 既定では、204 を返す。
	instance := &receiver{status: http.StatusNoContent}
	instance.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		instance.mutex.Lock()
		instance.bodies = append(instance.bodies, string(body))
		status := instance.status
		instance.mutex.Unlock()
		writer.WriteHeader(status)
	}))
	testingT.Cleanup(instance.server.Close)
	return instance
}

// received は、受け取った回数を返す。
func (instance *receiver) received() int {
	// 排他で数える。
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	return len(instance.bodies)
}

// setStatus は、返すステータスを変える。
func (instance *receiver) setStatus(status int) {
	// 排他で変える。
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	instance.status = status
}

// lastLogIDs は、最後に受け取った本文に入っていた、ログの id を返す。
func (instance *receiver) lastLogIDs(testingT *testing.T) []int64 {
	// 最後の本文を読む。
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	var decoded struct {
		Logs []struct {
			ID int64 `json:"id"`
		} `json:"logs"`
	}
	if unmarshalError := json.Unmarshal([]byte(instance.bodies[len(instance.bodies)-1]), &decoded); unmarshalError != nil {
		testingT.Fatalf("本文を読めるはず: %v", unmarshalError)
	}
	var identifiers []int64
	for _, log := range decoded.Logs {
		identifiers = append(identifiers, log.ID)
	}
	return identifiers
}

// fixture は、見回りの試験の道具一式。
type fixture struct {
	database   *gorm.DB
	repository *repository.ExportRepository
	runner     *Runner
	receiver   *receiver
	fake       *fakegoogle.Server
	cipher     *credential.Cipher
	clock      time.Time

	// logSequence は、入れたログの通し番号(一意制約の seq が、重ならないようにする)。
	logSequence int
}

// newFixture は、プロジェクト、ingress、DB、偽の Google、受け口、runner を用意する。
//
// 引数:
//   - testingT: テスト。
//   - flushRows: Drive へ前倒しで書き出す行数。
//
// 戻り値:
//   - 道具一式。
func newFixture(testingT *testing.T, flushRows int) *fixture {
	// データベースと、プロジェクトと ingress。
	database := testdatabase.New(testingT)
	ctx := context.Background()
	if createError := database.Create(&model.Project{ID: "project-00000001", UserID: "user-a", Name: "my-app", Status: model.ProjectStatusActive}).Error; createError != nil {
		testingT.Fatalf("プロジェクトを作れるはず: %v", createError)
	}
	if createError := repository.NewIngressRepository(database).Create(ctx, &model.Ingress{ID: "ingress-1", ProjectID: "project-00000001", UserID: "user-a", Host: "a.launchs.org"}); createError != nil {
		testingT.Fatalf("ingress を作れるはず: %v", createError)
	}
	cipher, _ := credential.NewCipher(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)))
	exportRepository := repository.NewExportRepository(database)
	fake := fakegoogle.New()
	testingT.Cleanup(fake.HTTP.Close)
	webhook := exportsend.NewWebhookSender(cipher, safehttp.Options{AllowHTTP: true, AllowPrivateNetworks: true})
	drive := exportsend.NewDriveWriter(googledrive.NewClient(fake.Config(), nil), exportRepository, cipher, 0)
	instance := &fixture{database: database, repository: exportRepository, receiver: newReceiver(testingT), fake: fake, cipher: cipher, clock: time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)}
	instance.runner = New(exportRepository, webhook, drive, flushRows, 10*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	instance.runner.SetNow(func() time.Time { return instance.clock })
	return instance
}

// addWebhook は、受け口に送る webhook の転送を作る。
func (instance *fixture) addWebhook(testingT *testing.T, suffix string, intervalSeconds int) model.ExportDestination {
	// 秘密を暗号化して、設定と一緒に保存する。
	encrypted, _ := instance.cipher.Encrypt("whsec_x")
	destination := model.ExportDestination{ID: model.ExportDestinationIDPrefix + suffix, UserID: "user-a", ProjectID: "project-00000001", IngressID: "ingress-1", Kind: model.ExportKindWebhook,
		Config: model.NewWebhookConfig(model.WebhookConfig{URL: instance.receiver.server.URL + "/hook", IntervalSeconds: intervalSeconds}), Credential: encrypted}
	if createError := instance.repository.CreateDestination(context.Background(), &destination); createError != nil {
		testingT.Fatalf("webhook を作れるはず: %v", createError)
	}
	return destination
}

// addLogs は、アクセスログを、count 行足す(入れた行の id を返す)。
func (instance *fixture) addLogs(testingT *testing.T, count int, at time.Time) []int64 {
	// 1 行ずつ入れて、id を集める。
	var identifiers []int64
	for range count {
		var identifier int64
		instance.logSequence++
		if insertError := instance.database.Raw("INSERT INTO ingress_access_logs (ingress_id, replica, batch_start, seq, requested_at, method, url, status, duration_ms, client_ip, bytes) VALUES ('ingress-1', 'r', ?, ?, ?, 'GET', '/p', 200, 1, '', 0) RETURNING id",
			at, instance.logSequence, at).Scan(&identifier).Error; insertError != nil {
			testingT.Fatalf("ログを入れられるはず: %v", insertError)
		}
		identifiers = append(identifiers, identifier)
	}
	return identifiers
}

// reload は、転送の今の状態を読み直す。
func (instance *fixture) reload(testingT *testing.T, destination model.ExportDestination) model.ExportDestination {
	// 持ち主として、読み直す。
	reloaded, findError := instance.repository.FindDestination(context.Background(), destination.UserID, destination.ProjectID, destination.IngressID, destination.ID)
	if findError != nil {
		testingT.Fatalf("読み直せるはず: %v", findError)
	}
	return reloaded
}

// TestWebhookRounds は、新しいログだけが、間隔ごとに送られ、カーソルが進み、送るものが無ければ呼ばないことを確かめる。
func TestWebhookRounds(testingT *testing.T) {
	instance := newFixture(testingT, 6000)
	ctx := context.Background()
	destination := instance.addWebhook(testingT, "w1", 30)
	// 作る前のログは、送らない。作ったあとの 3 行が、送られる。
	instance.addLogs(testingT, 3, instance.clock)
	instance.clock = instance.clock.Add(time.Second)
	instance.runner.RunOnce(ctx)
	if instance.receiver.received() != 1 {
		testingT.Fatalf("1 回送られるはず: %d", instance.receiver.received())
	}
	sent := instance.receiver.lastLogIDs(testingT)
	if len(sent) != 3 || sent[0] >= sent[1] || sent[1] >= sent[2] {
		testingT.Errorf("3 行が、昇順で送られるはず: %v", sent)
	}
	saved := instance.reload(testingT, destination)
	if saved.LastCursor != sent[2] || saved.Status != model.ExportStatusActive || saved.LastSuccessAt == nil {
		testingT.Errorf("カーソルが進むはず: %+v", saved)
	}
	// 間隔(30 秒)の前に見回っても、送らない。
	instance.clock = instance.clock.Add(10 * time.Second)
	instance.addLogs(testingT, 1, instance.clock)
	instance.runner.RunOnce(ctx)
	if instance.receiver.received() != 1 {
		testingT.Errorf("間隔の前は、送らないはず: %d", instance.receiver.received())
	}
	// 間隔が過ぎたら、新しい 1 行だけが送られる。
	instance.clock = instance.clock.Add(25 * time.Second)
	instance.runner.RunOnce(ctx)
	if instance.receiver.received() != 2 || len(instance.receiver.lastLogIDs(testingT)) != 1 {
		testingT.Errorf("新しい 1 行だけが送られるはず: %d", instance.receiver.received())
	}
	// 新しい行が無ければ、呼ばない。
	instance.clock = instance.clock.Add(time.Minute)
	instance.runner.RunOnce(ctx)
	if instance.receiver.received() != 2 {
		testingT.Errorf("新しい行が無ければ、呼ばないはず: %d", instance.receiver.received())
	}
}

// TestWebhookFailureAndRecovery は、失敗したらカーソルが進まず、間隔が延び、直ったら溜まった分が送られることを確かめる。
func TestWebhookFailureAndRecovery(testingT *testing.T) {
	instance := newFixture(testingT, 6000)
	ctx := context.Background()
	destination := instance.addWebhook(testingT, "w1", 30)
	added := instance.addLogs(testingT, 2, instance.clock)
	instance.receiver.setStatus(http.StatusInternalServerError)
	instance.clock = instance.clock.Add(time.Second)
	instance.runner.RunOnce(ctx)
	failed := instance.reload(testingT, destination)
	if failed.Status != model.ExportStatusFailing || failed.FailureCount != 1 || failed.LastCursor != destination.LastCursor || !strings.Contains(failed.LastError, "500") {
		testingT.Fatalf("失敗が記録され、カーソルは進まないはず: %+v", failed)
	}
	// 次の時刻(30 秒後)の前には、もう一度送らない。
	instance.runner.RunOnce(ctx)
	if instance.receiver.received() != 1 {
		testingT.Errorf("すぐには、再試行しないはず: %d", instance.receiver.received())
	}
	// 2 回目の失敗では、間隔が倍(60 秒)になる。
	instance.clock = instance.clock.Add(31 * time.Second)
	instance.runner.RunOnce(ctx)
	if again := instance.reload(testingT, destination); again.FailureCount != 2 || again.NextAttemptAt.Sub(instance.clock) != 60*time.Second {
		testingT.Errorf("2 回目の失敗で、間隔が倍になるはず: %+v", again)
	}
	// 受け口が直ると、溜まっていた行が、そのまま送られる(取りこぼさない)。
	instance.receiver.setStatus(http.StatusOK)
	instance.clock = instance.clock.Add(61 * time.Second)
	instance.runner.RunOnce(ctx)
	recovered := instance.reload(testingT, destination)
	sent := instance.receiver.lastLogIDs(testingT)
	if recovered.Status != model.ExportStatusActive || recovered.FailureCount != 0 || recovered.LastError != "" || len(sent) != 2 || sent[0] != added[0] || recovered.LastCursor != added[1] {
		testingT.Errorf("直ったら、溜まった行が送られるはず: %+v %v", recovered, sent)
	}
}

// TestWebhookBatches は、1000 行を超えるログが、分けて送られ、全部送り切ることを確かめる。
func TestWebhookBatches(testingT *testing.T) {
	instance := newFixture(testingT, 6000)
	destination := instance.addWebhook(testingT, "w1", 10)
	added := instance.addLogs(testingT, 2300, instance.clock)
	instance.clock = instance.clock.Add(time.Second)
	instance.runner.RunOnce(context.Background())
	// 1000 + 1000 + 300 の 3 回。
	if instance.receiver.received() != 3 || len(instance.receiver.lastLogIDs(testingT)) != 300 || instance.reload(testingT, destination).LastCursor != added[len(added)-1] {
		testingT.Errorf("3 回に分けて、全部送るはず: %d 回", instance.receiver.received())
	}
}

// TestDriveSlotAndOverflow は、Drive が、自分の枠の時刻にだけ書くことと、行がたまりすぎたら前倒しで書くことを確かめる。
func TestDriveSlotAndOverflow(testingT *testing.T) {
	// 前倒しの行数を、小さく(3 行)する。
	instance := newFixture(testingT, 3)
	ctx := context.Background()
	encrypted, _ := instance.cipher.Encrypt("refresh-abc")
	if saveError := instance.repository.SaveConnection(ctx, &model.GoogleDriveConnection{UserID: "user-a", AccountEmail: "me@example.com", Credential: encrypted, Status: model.ExportStatusActive}); saveError != nil {
		testingT.Fatalf("接続を保存できるはず: %v", saveError)
	}
	drive := model.ExportDestination{ID: "export-d1", UserID: "user-a", ProjectID: "project-00000001", IngressID: "ingress-1", Kind: model.ExportKindGoogleDrive}
	if createError := instance.repository.CreateDestination(ctx, &drive); createError != nil {
		testingT.Fatalf("Drive を作れるはず: %v", createError)
	}
	// 作った直後は、次に動く時刻が今なので、(行があれば)すぐ 1 回動く。行は、まだ無い。
	instance.clock = instance.clock.Add(time.Second)
	instance.runner.RunOnce(ctx)
	if len(instance.fake.Files) != 0 {
		testingT.Fatalf("書くものが無ければ、Drive には何も作らないはず: %d", len(instance.fake.Files))
	}
	state := instance.reload(testingT, drive)
	if state.NextAttemptAt.Minute() != state.DriveSlot()*5 || !state.NextAttemptAt.After(instance.clock) {
		testingT.Fatalf("次の枠の時刻になるはず: %v(枠 %d)", state.NextAttemptAt, state.DriveSlot())
	}
	// 2 行たまっても、前倒しの数(3)に届かず、枠の時刻の前なので、書かない。
	instance.addLogs(testingT, 2, instance.clock)
	instance.runner.RunOnce(ctx)
	if len(instance.fake.Files) != 0 {
		testingT.Errorf("枠の時刻の前は、書かないはず: %d", len(instance.fake.Files))
	}
	// 3 行になると、前倒しで書く。
	instance.addLogs(testingT, 1, instance.clock)
	instance.runner.RunOnce(ctx)
	roots := instance.fake.ChildrenNamed("", "launchs-access-logs")
	if len(roots) != 1 {
		testingT.Fatalf("前倒しで書かれるはず")
	}
	projectFolder := instance.fake.ChildrenNamed(roots[0].ID, "my-app")[0]
	files := instance.fake.ChildrenNamed(projectFolder.ID, "a.launchs.org_2026-11-26.ndjson")
	if len(files) != 1 || strings.Count(string(files[0].Content), "\n") != 3 {
		testingT.Fatalf("3 行が、その日のファイルに入るはず: %+v", files)
	}
	// 次の枠の時刻に、新しい行があれば、同じファイルの続きに書かれる。
	instance.addLogs(testingT, 1, instance.clock)
	instance.clock = instance.reload(testingT, drive).NextAttemptAt.Add(time.Second)
	instance.runner.RunOnce(ctx)
	if files = instance.fake.ChildrenNamed(projectFolder.ID, "a.launchs.org_2026-11-26.ndjson"); len(files) != 1 || strings.Count(string(files[0].Content), "\n") != 4 {
		testingT.Errorf("続きに足されるはず: %+v", files)
	}
}

// TestDriveRevoked は、許可が取り消されたら、接続と転送が再接続待ちになり、動かされなくなることを確かめる。
func TestDriveRevoked(testingT *testing.T) {
	instance := newFixture(testingT, 6000)
	ctx := context.Background()
	encrypted, _ := instance.cipher.Encrypt("refresh-abc")
	_ = instance.repository.SaveConnection(ctx, &model.GoogleDriveConnection{UserID: "user-a", Credential: encrypted, Status: model.ExportStatusActive})
	drive := model.ExportDestination{ID: "export-d1", UserID: "user-a", ProjectID: "project-00000001", IngressID: "ingress-1", Kind: model.ExportKindGoogleDrive}
	_ = instance.repository.CreateDestination(ctx, &drive)
	instance.addLogs(testingT, 2, instance.clock)
	instance.fake.InvalidRefreshTokens["refresh-abc"] = true
	instance.clock = instance.clock.Add(time.Second)
	instance.runner.RunOnce(ctx)
	connection, _ := instance.repository.FindConnection(ctx, "user-a")
	state := instance.reload(testingT, drive)
	if connection.IsActive() || state.Status != model.ExportStatusReauthRequired || state.LastCursor != drive.LastCursor {
		testingT.Fatalf("接続と転送が、再接続待ちで、カーソルは進まないはず: %+v %+v", connection, state)
	}
	// 再接続待ちの間は、動かさない。つなぎ直すと、動き出す。
	instance.fake.InvalidRefreshTokens["refresh-abc"] = false
	instance.clock = instance.clock.Add(2 * time.Hour)
	instance.runner.RunOnce(ctx)
	if len(instance.fake.Files) != 0 {
		testingT.Errorf("再接続待ちの間は、動かさないはず")
	}
	reconnected := &model.GoogleDriveConnection{UserID: "user-a", Credential: encrypted, Status: model.ExportStatusActive}
	_ = instance.repository.SaveConnection(ctx, reconnected)
	instance.clock = instance.clock.Add(time.Second)
	instance.runner.RunOnce(ctx)
	if len(instance.fake.Files) == 0 || instance.reload(testingT, drive).Status != model.ExportStatusActive {
		testingT.Errorf("つなぎ直すと、書かれるはず: ファイル %d", len(instance.fake.Files))
	}
}
