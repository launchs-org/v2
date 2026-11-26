package exportsend_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"launchs/shared/credential"
	"launchs/shared/database/testdatabase"
	"launchs/shared/exportlog"
	"launchs/shared/exportsend"
	"launchs/shared/googledrive"
	"launchs/shared/googledrive/fakegoogle"
	"launchs/shared/model"
	"launchs/shared/repository"
	"launchs/shared/safehttp"
)

// newTestCipher は、試験用の暗号化(鍵は固定)を作る。
func newTestCipher(testingT *testing.T) *credential.Cipher {
	// 固定の 32 バイトの鍵。
	cipher, cipherError := credential.NewCipher(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if cipherError != nil {
		testingT.Fatalf("暗号化を作れるはず: %v", cipherError)
	}
	return cipher
}

// newWebhookDestination は、秘密を暗号化して持つ、試験用の webhook の設定を作る。
func newWebhookDestination(testingT *testing.T, cipher *credential.Cipher, targetURL string, secret string) model.ExportDestination {
	// 秘密を暗号化して入れる。
	encrypted, _ := cipher.Encrypt(secret)
	return model.ExportDestination{ID: "export-w1", UserID: "user-a", ProjectID: "project-00000001", IngressID: "ingress-1", Kind: model.ExportKindWebhook,
		Config: model.NewWebhookConfig(model.WebhookConfig{URL: targetURL, IntervalSeconds: 30}), Credential: encrypted}
}

// TestWebhookSend は、署名と、ヘッダーと、本文が正しく、受け取る側が検証できることを確かめる。
func TestWebhookSend(testingT *testing.T) {
	cipher := newTestCipher(testingT)
	var gotBody []byte
	var gotHeader http.Header
	receiver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotBody, _ = io.ReadAll(request.Body)
		gotHeader = request.Header.Clone()
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	sender := exportsend.NewWebhookSender(cipher, safehttp.Options{AllowHTTP: true, AllowPrivateNetworks: true})
	sender.SetNow(func() time.Time { return time.Unix(1790000000, 0) })
	destination := newWebhookDestination(testingT, cipher, receiver.URL+"/hook?token=abc", "whsec_secret")
	lines := []exportlog.Line{{ID: 5, IngressID: "ingress-1", Host: "a.example.com", Time: "2026-11-26T01:00:00.000Z", Method: "GET", URL: "/x", Status: 200}, {ID: 9, IngressID: "ingress-1", Host: "a.example.com", Method: "POST", URL: "/y", Status: 500}}
	if sendError := sender.Send(context.Background(), destination, lines); sendError != nil {
		testingT.Fatalf("送れるはず: %v", sendError)
	}
	// 署名は、タイムスタンプ + "." + 本文を、秘密で HMAC したもの。
	if gotHeader.Get(exportsend.SignatureHeader) != exportsend.Sign("whsec_secret", "1790000000", gotBody) || gotHeader.Get(exportsend.TimestampHeader) != "1790000000" {
		testingT.Errorf("署名とタイムスタンプが合うはず: %v", gotHeader)
	}
	// 配送の印は、設定の ID と、最後の行の id。
	if gotHeader.Get(exportsend.DeliveryHeader) != "export-w1-9" || gotHeader.Get("Content-Type") != "application/json" {
		testingT.Errorf("配送の印が違う: %v", gotHeader)
	}
	var decoded struct {
		ProjectID     string
		DestinationID string
		Logs          []exportlog.Line
	}
	if unmarshalError := json.Unmarshal(gotBody, &decoded); unmarshalError != nil || decoded.ProjectID != "project-00000001" || decoded.DestinationID != "export-w1" || len(decoded.Logs) != 2 || decoded.Logs[1].ID != 9 {
		testingT.Errorf("本文が違う: %s", gotBody)
	}
	// テスト送信は、test: true で、ログは空。
	if testError := sender.SendTest(context.Background(), destination); testError != nil || !strings.Contains(string(gotBody), `"test":true`) || !strings.Contains(string(gotBody), `"logs":[]`) {
		testingT.Errorf("テスト送信の本文が違う: %v %s", testError, gotBody)
	}
}

// TestWebhookSendFailures は、失敗の理由が、URL やクエリを含まない短いものになることと、内部の宛先に送らないことを確かめる。
func TestWebhookSendFailures(testingT *testing.T) {
	cipher := newTestCipher(testingT)
	failing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusInternalServerError) }))
	defer failing.Close()
	loose := safehttp.Options{AllowHTTP: true, AllowPrivateNetworks: true}
	// 2xx 以外は失敗。理由に、URL とクエリは入らない。
	sender := exportsend.NewWebhookSender(cipher, loose)
	sendError := sender.Send(context.Background(), newWebhookDestination(testingT, cipher, failing.URL+"/hook?token=SECRET", "s"), nil)
	if sendError == nil || !strings.Contains(sendError.Error(), "500") || strings.Contains(sendError.Error(), "SECRET") || strings.Contains(sendError.Error(), failing.URL) {
		testingT.Errorf("500 の理由が、URL を含まない形のはず: %v", sendError)
	}
	// つながらない宛先は、失敗(理由に URL を含まない)。
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closed.URL
	closed.Close()
	if connectError := sender.Send(context.Background(), newWebhookDestination(testingT, cipher, closedURL+"/x?token=SECRET", "s"), nil); connectError == nil || strings.Contains(connectError.Error(), "SECRET") {
		testingT.Errorf("つながらないときは、URL を含まない理由のはず: %v", connectError)
	}
	// 本番と同じ(ゆるめない)設定では、ループバックの宛先には、送らない。
	strict := exportsend.NewWebhookSender(cipher, safehttp.Options{AllowHTTP: true})
	if blockedError := strict.Send(context.Background(), newWebhookDestination(testingT, cipher, failing.URL+"/hook", "s"), nil); blockedError == nil {
		testingT.Errorf("内部の宛先には、送らないはず")
	}
	// 秘密を復号できない(鍵が違う)ときは、失敗。
	otherCipher, _ := credential.NewCipher(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	if decryptError := exportsend.NewWebhookSender(otherCipher, loose).Send(context.Background(), newWebhookDestination(testingT, cipher, failing.URL, "s"), nil); decryptError == nil {
		testingT.Errorf("別の鍵では、失敗するはず")
	}
}

// driveFixture は、Drive への書き込みの試験の道具(DB、偽の Google、書き手)。
type driveFixture struct {
	database    *gorm.DB
	repository  *repository.ExportRepository
	fake        *fakegoogle.Server
	writer      *exportsend.DriveWriter
	connection  model.GoogleDriveConnection
	destination model.ExportDestination
	cipher      *credential.Cipher
}

// newDriveFixture は、プロジェクト、ingress、Drive の接続、転送の設定を作る。
//
// 引数:
//   - testingT: テスト。
//   - maxFileBytes: 1 つのファイルの大きさの上限。
//
// 戻り値:
//   - 道具一式。
func newDriveFixture(testingT *testing.T, maxFileBytes int64) *driveFixture {
	// データベースと、偽の Google を用意する。
	database := testdatabase.New(testingT)
	fake := fakegoogle.New()
	testingT.Cleanup(fake.HTTP.Close)
	cipher := newTestCipher(testingT)
	ctx := context.Background()
	if createError := database.Create(&model.Project{ID: "project-00000001", UserID: "user-a", Name: "my-app", Status: model.ProjectStatusActive}).Error; createError != nil {
		testingT.Fatalf("プロジェクトを作れるはず: %v", createError)
	}
	ingress := &model.Ingress{ID: "ingress-1", ProjectID: "project-00000001", UserID: "user-a", Host: "a.launchs.org"}
	if createError := repository.NewIngressRepository(database).Create(ctx, ingress); createError != nil {
		testingT.Fatalf("ingress を作れるはず: %v", createError)
	}
	exportRepository := repository.NewExportRepository(database)
	encrypted, _ := cipher.Encrypt("refresh-abc")
	connection := model.GoogleDriveConnection{UserID: "user-a", AccountEmail: "me@example.com", Credential: encrypted, Status: model.ExportStatusActive}
	if saveError := exportRepository.SaveConnection(ctx, &connection); saveError != nil {
		testingT.Fatalf("接続を保存できるはず: %v", saveError)
	}
	destination := model.ExportDestination{ID: "export-d1", UserID: "user-a", ProjectID: "project-00000001", IngressID: "ingress-1", Kind: model.ExportKindGoogleDrive}
	if createError := exportRepository.CreateDestination(ctx, &destination); createError != nil {
		testingT.Fatalf("転送を作れるはず: %v", createError)
	}
	writer := exportsend.NewDriveWriter(googledrive.NewClient(fake.Config(), nil), exportRepository, cipher, maxFileBytes)
	return &driveFixture{database: database, repository: exportRepository, fake: fake, writer: writer, connection: connection, destination: destination, cipher: cipher}
}

// write は、接続を読み直してから、ログを書く(本番の exporter も、見回りのたびに、接続を読み直す)。
//
// 引数:
//   - testingT: テスト。
//   - rows: 書くログ。
//
// 戻り値:
//   - 書き込みのエラー。
func (fixture *driveFixture) write(testingT *testing.T, rows ...repository.ExportLogRow) error {
	// 大元のフォルダの ID などを、最新にする。
	connection, findError := fixture.repository.FindConnection(context.Background(), "user-a")
	if findError != nil {
		testingT.Fatalf("接続を読めるはず: %v", findError)
	}
	return fixture.writer.Write(context.Background(), connection, fixture.destination, rows)
}

// row は、試験用のログの行を作る。
//
// 引数:
//   - identifier: 行の id。
//   - moment: リクエストを受けた時刻。
//
// 戻り値:
//   - ログの行。
func row(identifier int64, moment time.Time) repository.ExportLogRow {
	// 必要な項目だけを入れる。
	return repository.ExportLogRow{ID: identifier, IngressID: "ingress-1", Host: "a.launchs.org", RequestedAt: moment, Method: "GET", URL: "/p/" + strconv.FormatInt(identifier, 10), Status: 200}
}

// TestDriveWriterDailyFiles は、日本時間の日ごとにファイルが分かれ、プロジェクトのフォルダに入り、続きが足され、同じ行が二重にならないことを確かめる。
func TestDriveWriterDailyFiles(testingT *testing.T) {
	fixture := newDriveFixture(testingT, 0)
	ctx := context.Background()
	// 日本時間で 11/26 の 23:59 と、11/27 の 0:01(UTC では、どちらも 11/26 の 14:59 と 15:01)。
	late := time.Date(2026, 11, 26, 14, 59, 0, 0, time.UTC)
	early := time.Date(2026, 11, 26, 15, 1, 0, 0, time.UTC)
	if writeError := fixture.write(testingT, row(1, late), row(2, early)); writeError != nil {
		testingT.Fatalf("書けるはず: %v", writeError)
	}
	// フォルダ: launchs-access-logs / my-app。
	roots := fixture.fake.ChildrenNamed("", "launchs-access-logs")
	if len(roots) != 1 {
		testingT.Fatalf("大元のフォルダが 1 つのはず: %d", len(roots))
	}
	projectFolders := fixture.fake.ChildrenNamed(roots[0].ID, "my-app")
	if len(projectFolders) != 1 {
		testingT.Fatalf("プロジェクトのフォルダが 1 つのはず: %d", len(projectFolders))
	}
	// 日本時間の日ごとに、別のファイル。
	first := fixture.fake.ChildrenNamed(projectFolders[0].ID, "a.launchs.org_2026-11-26.ndjson")
	second := fixture.fake.ChildrenNamed(projectFolders[0].ID, "a.launchs.org_2026-11-27.ndjson")
	if len(first) != 1 || len(second) != 1 || !strings.Contains(string(first[0].Content), `"id":1`) || strings.Contains(string(first[0].Content), `"id":2`) || !strings.Contains(string(second[0].Content), `"id":2`) {
		testingT.Fatalf("日ごとに分かれるはず: %+v %+v", first, second)
	}
	// 続き(id 3)を足すと、同じファイルの末尾に入る。やり直しで、同じ行(id 2)を渡しても、二重にならない。
	if writeError := fixture.write(testingT, row(2, early), row(3, early.Add(time.Minute))); writeError != nil {
		testingT.Fatalf("続きを書けるはず: %v", writeError)
	}
	content := string(fixture.fake.ChildrenNamed(projectFolders[0].ID, "a.launchs.org_2026-11-27.ndjson")[0].Content)
	if strings.Count(content, `"id":2`) != 1 || strings.Count(content, `"id":3`) != 1 || strings.Index(content, `"id":2`) > strings.Index(content, `"id":3`) || !strings.HasSuffix(content, "\n") {
		testingT.Errorf("二重にならず、順に並ぶはず: %q", content)
	}
	// 記録は、最後に書いた id と行数を持つ。フォルダは、2 回目でも、増えない。
	record, _ := fixture.repository.FindDriveFile(ctx, fixture.destination.ID, "2026-11-27")
	if record == nil || record.LastLogID != 3 || record.LogRows != 2 || len(fixture.fake.ChildrenNamed("", "launchs-access-logs")) != 1 {
		testingT.Errorf("記録が違う、またはフォルダが増えた: %+v", record)
	}
}

// TestDriveWriterRotatesAndRecovers は、大きくなりすぎたら次の分割に書くことと、ファイルやフォルダを消されたら作り直すことを確かめる。
func TestDriveWriterRotatesAndRecovers(testingT *testing.T) {
	// 上限を、ごく小さくして、すぐに分割させる。
	fixture := newDriveFixture(testingT, 200)
	ctx := context.Background()
	moment := time.Date(2026, 11, 26, 1, 0, 0, 0, time.UTC)
	for identifier := int64(1); identifier <= 4; identifier++ {
		if writeError := fixture.write(testingT, row(identifier, moment)); writeError != nil {
			testingT.Fatalf("書けるはず(%d): %v", identifier, writeError)
		}
	}
	roots := fixture.fake.ChildrenNamed("", "launchs-access-logs")
	projectFolder := fixture.fake.ChildrenNamed(roots[0].ID, "my-app")[0]
	if len(fixture.fake.ChildrenNamed(projectFolder.ID, "a.launchs.org_2026-11-26_part2.ndjson")) != 1 {
		testingT.Errorf("2 つ目の分割ができるはず")
	}
	// プロジェクトのフォルダが、ゴミ箱に入れられたら、次の書き込みで、作り直す。
	fixture.fake.Files[projectFolder.ID].Trashed = true
	if writeError := fixture.write(testingT, row(5, moment)); writeError != nil {
		testingT.Fatalf("フォルダを作り直して書けるはず: %v", writeError)
	}
	if len(fixture.fake.ChildrenNamed(roots[0].ID, "my-app")) != 1 {
		testingT.Errorf("ゴミ箱のフォルダは数えず、新しいフォルダが 1 つのはず")
	}
	// 書き込みが失敗したら、エラーを返し、記録は進まない(やり直せる)。
	fixture.fake.FailUploads = true
	before, _ := fixture.repository.FindDriveFile(ctx, fixture.destination.ID, "2026-11-26")
	if writeError := fixture.write(testingT, row(6, moment)); writeError == nil {
		testingT.Errorf("失敗するはず")
	}
	after, _ := fixture.repository.FindDriveFile(ctx, fixture.destination.ID, "2026-11-26")
	if before.LastLogID != after.LastLogID {
		testingT.Errorf("失敗したら、記録は進まないはず: %d → %d", before.LastLogID, after.LastLogID)
	}
}

// TestDriveWriterInvalidGrantAndTest は、合鍵が効かないとき ErrInvalidGrant になることと、テストのファイルが作られることを確かめる。
func TestDriveWriterInvalidGrantAndTest(testingT *testing.T) {
	fixture := newDriveFixture(testingT, 0)
	ctx := context.Background()
	// テストのファイルが、プロジェクトのフォルダに作られる。
	if testError := fixture.writer.WriteTest(ctx, fixture.connection, fixture.destination); testError != nil {
		testingT.Fatalf("テストのファイルを作れるはず: %v", testError)
	}
	roots := fixture.fake.ChildrenNamed("", "launchs-access-logs")
	projectFolder := fixture.fake.ChildrenNamed(roots[0].ID, "my-app")[0]
	if len(fixture.fake.ChildrenNamed(projectFolder.ID, "launchs-connection-test.txt")) != 1 {
		testingT.Errorf("テストのファイルがあるはず")
	}
	// 許可が取り消されたら、ErrInvalidGrant。
	fixture.fake.InvalidRefreshTokens["refresh-abc"] = true
	writeError := fixture.write(testingT, row(1, time.Now()))
	if !errors.Is(writeError, googledrive.ErrInvalidGrant) {
		testingT.Errorf("合鍵が効かなければ ErrInvalidGrant のはず: %v", writeError)
	}
}
