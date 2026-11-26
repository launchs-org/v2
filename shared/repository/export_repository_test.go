package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// newTestExportDestination は、テスト用の転送の設定(保存前)を作る。
//
// 引数:
//   - suffix: ID の末尾。
//   - ingressID: 対象の ingress の ID。
//   - kind: 種類。
//
// 戻り値:
//   - 保存前の転送の設定。
func newTestExportDestination(suffix string, ingressID string, kind string) *model.ExportDestination {
	destination := &model.ExportDestination{ID: model.ExportDestinationIDPrefix + suffix, UserID: "user-a", ProjectID: "project-00000001", IngressID: ingressID, Kind: kind}
	// webhook には、設定と秘密を入れる。
	if kind == model.ExportKindWebhook {
		destination.Config = model.NewWebhookConfig(model.WebhookConfig{URL: "https://example.com/hook", IntervalSeconds: 30})
		destination.Credential = []byte("encrypted")
	}
	return destination
}

// TestExportDestinationLifecycle は、作成(カーソルの開始位置、上限、Drive の重複)、取得、更新、削除、他人のものを確かめる。
func TestExportDestinationLifecycle(testingT *testing.T) {
	ctx := context.Background()
	ingressRepository, database, _ := newIngressTestEnvironment(testingT)
	exportRepository := NewExportRepository(database)
	first := newTestIngress("a.launchs.org")
	if createError := ingressRepository.Create(ctx, first); createError != nil {
		testingT.Fatalf("ingress を作れるはず: %v", createError)
	}
	// ログを 3 行入れておく(作った時点の、最後の id から始めることを確かめるため)。
	var lastLogID int64
	for index := 0; index < 3; index++ {
		if insertError := database.Raw("INSERT INTO ingress_access_logs (ingress_id, replica, batch_start, seq, requested_at, method, url, status, duration_ms, client_ip, bytes) VALUES (?, 'r', now(), ?, now(), 'GET', '/', 200, 1, '', 0) RETURNING id", first.ID, index).Scan(&lastLogID).Error; insertError != nil {
			testingT.Fatalf("ログを入れられるはず: %v", insertError)
		}
	}
	// 作れる。カーソルは、今の最後の id から始まる。
	webhook := newTestExportDestination("w1", first.ID, model.ExportKindWebhook)
	if createError := exportRepository.CreateDestination(ctx, webhook); createError != nil {
		testingT.Fatalf("作れるはず: %v", createError)
	}
	if webhook.LastCursor != lastLogID || !webhook.Enabled || webhook.Status != model.ExportStatusActive {
		testingT.Errorf("作った時点の最後の id から始まるはず: %+v(期待 %d)", webhook, lastLogID)
	}
	// Google Drive は 1 つの ingress に 1 つまで。
	var appError *apperror.Error
	drive := newTestExportDestination("d1", first.ID, model.ExportKindGoogleDrive)
	if createError := exportRepository.CreateDestination(ctx, drive); createError != nil {
		testingT.Fatalf("Drive を作れるはず: %v", createError)
	}
	if duplicateError := exportRepository.CreateDestination(ctx, newTestExportDestination("d2", first.ID, model.ExportKindGoogleDrive)); !errors.As(duplicateError, &appError) || appError.Kind != apperror.KindConflict {
		testingT.Errorf("Drive の重複は conflict のはず: %v", duplicateError)
	}
	// 他人の ingress には、作れない。
	other := newTestExportDestination("x1", first.ID, model.ExportKindWebhook)
	other.UserID = "user-b"
	if otherError := exportRepository.CreateDestination(ctx, other); !errors.As(otherError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("他人の ingress は not_found のはず: %v", otherError)
	}
	// 上限(10)を超えると conflict。すでに 2 つ(webhook と Drive)あるので、あと 8 つ。
	for index := 0; index < 8; index++ {
		if createError := exportRepository.CreateDestination(ctx, newTestExportDestination("n"+string(rune('a'+index)), first.ID, model.ExportKindWebhook)); createError != nil {
			testingT.Fatalf("上限までは作れるはず(%d): %v", index, createError)
		}
	}
	if overError := exportRepository.CreateDestination(ctx, newTestExportDestination("over", first.ID, model.ExportKindWebhook)); !errors.As(overError, &appError) || appError.Kind != apperror.KindConflict {
		testingT.Errorf("上限を超えたら conflict のはず: %v", overError)
	}
	// 一覧と取得。他人からは見えない。
	destinations, _ := exportRepository.ListDestinations(ctx, "user-a", "project-00000001", first.ID)
	if len(destinations) != 10 {
		testingT.Errorf("10 個あるはず: %d", len(destinations))
	}
	if _, listError := exportRepository.ListDestinations(ctx, "user-b", "project-00000001", first.ID); !errors.As(listError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("他人からは not_found のはず: %v", listError)
	}
	found, findError := exportRepository.FindDestination(ctx, "user-a", "project-00000001", first.ID, webhook.ID)
	if findError != nil || found.Kind != model.ExportKindWebhook {
		testingT.Fatalf("取得できるはず: %v", findError)
	}
	if _, otherFindError := exportRepository.FindDestination(ctx, "user-b", "project-00000001", first.ID, webhook.ID); !errors.As(otherFindError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("他人のものは not_found のはず: %v", otherFindError)
	}
	// 無効にできる。カーソルは、触らない。
	found.Enabled = false
	if updateError := exportRepository.UpdateDestination(ctx, &found); updateError != nil {
		testingT.Fatalf("更新できるはず: %v", updateError)
	}
	reloaded, _ := exportRepository.FindDestination(ctx, "user-a", "project-00000001", first.ID, webhook.ID)
	if reloaded.Enabled || reloaded.LastCursor != lastLogID {
		testingT.Errorf("無効になり、カーソルは変わらないはず: %+v", reloaded)
	}
	// 削除できる。他人は 404、無いものも 404。
	if deleteError := exportRepository.DeleteDestination(ctx, "user-b", "project-00000001", first.ID, webhook.ID); !errors.As(deleteError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("他人の削除は not_found のはず: %v", deleteError)
	}
	if deleteError := exportRepository.DeleteDestination(ctx, "user-a", "project-00000001", first.ID, webhook.ID); deleteError != nil {
		testingT.Errorf("削除できるはず: %v", deleteError)
	}
	if deleteError := exportRepository.DeleteDestination(ctx, "user-a", "project-00000001", first.ID, webhook.ID); !errors.As(deleteError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("無いものは not_found のはず: %v", deleteError)
	}
	// ingress を消すと、転送の設定も消える(外部キーの連鎖)。
	if deleteError := database.Delete(first).Error; deleteError != nil {
		testingT.Fatalf("ingress を消せるはず: %v", deleteError)
	}
	var remaining int64
	database.Model(&model.ExportDestination{}).Count(&remaining)
	if remaining != 0 {
		testingT.Errorf("ingress と一緒に消えるはず: %d", remaining)
	}
}

// TestExportExporterSide は、exporter が使う読み出し(動かす転送、ログ、数え上げ)と、進み具合の保存を確かめる。
func TestExportExporterSide(testingT *testing.T) {
	ctx := context.Background()
	ingressRepository, database, _ := newIngressTestEnvironment(testingT)
	exportRepository := NewExportRepository(database)
	first := newTestIngress("a.launchs.org")
	if createError := ingressRepository.Create(ctx, first); createError != nil {
		testingT.Fatalf("ingress を作れるはず: %v", createError)
	}
	webhook := newTestExportDestination("w1", first.ID, model.ExportKindWebhook)
	if createError := exportRepository.CreateDestination(ctx, webhook); createError != nil {
		testingT.Fatalf("作れるはず: %v", createError)
	}
	// 作ったあとに、ログが 5 行増える。
	var ids []int64
	for index := 0; index < 5; index++ {
		var logID int64
		if insertError := database.Raw("INSERT INTO ingress_access_logs (ingress_id, replica, batch_start, seq, requested_at, method, url, status, duration_ms, client_ip, bytes) VALUES (?, 'r', now(), ?, now(), 'GET', '/p', 200, 1.5, '203.0.113.9', 10) RETURNING id", first.ID, index).Scan(&logID).Error; insertError != nil {
			testingT.Fatalf("ログを入れられるはず: %v", insertError)
		}
		ids = append(ids, logID)
	}
	// 動かす転送: 作った直後は、次に動く時刻が今なので、出てくる。
	runnable, _ := exportRepository.ListRunnable(ctx, time.Now().Add(time.Second))
	if len(runnable) != 1 || runnable[0].ID != webhook.ID {
		testingT.Fatalf("動かす転送に出るはず: %+v", runnable)
	}
	// ログは、id の昇順で、カーソルより後を、上限まで読める。ホスト名が付く。
	rows, readError := exportRepository.ReadLogs(ctx, first.ID, webhook.LastCursor, 3)
	if readError != nil || len(rows) != 3 || rows[0].ID != ids[0] || rows[2].ID != ids[2] || rows[0].Host != "a.launchs.org" || rows[0].ToLine().Method != "GET" {
		testingT.Fatalf("先頭の 3 行が、昇順で読めるはず: %+v %v", rows, readError)
	}
	// 数え上げは、上限で止まる。
	if count, _ := exportRepository.CountPendingLogs(ctx, first.ID, webhook.LastCursor, 3); count != 3 {
		testingT.Errorf("上限の 3 で止まるはず: %d", count)
	}
	if count, _ := exportRepository.CountPendingLogs(ctx, first.ID, ids[4], 3); count != 0 {
		testingT.Errorf("全部送ったら 0 のはず: %d", count)
	}
	// 成功を保存すると、カーソルが進み、次に動く時刻が先になって、動かす転送から外れる。
	webhook.RecordSuccess(time.Now(), ids[2])
	if saveError := exportRepository.SaveProgress(ctx, webhook); saveError != nil {
		testingT.Fatalf("保存できるはず: %v", saveError)
	}
	if runnableAfter, _ := exportRepository.ListRunnable(ctx, time.Now()); len(runnableAfter) != 0 {
		testingT.Errorf("次の時刻までは動かさないはず: %+v", runnableAfter)
	}
	saved, _ := exportRepository.FindDestination(ctx, "user-a", "project-00000001", first.ID, webhook.ID)
	if saved.LastCursor != ids[2] || saved.LastSuccessAt == nil {
		testingT.Errorf("カーソルが進むはず: %+v", saved)
	}
	// プロジェクトの名前が取れる。
	if name, nameError := exportRepository.ProjectName(ctx, "project-00000001"); nameError != nil || name != "my-app" {
		testingT.Errorf("プロジェクトの名前が取れるはず: %q %v", name, nameError)
	}
}

// TestGoogleDriveConnection は、接続の保存・再接続待ち・つなぎ直し・削除(転送も消える)と、フォルダとファイルの記録を確かめる。
func TestGoogleDriveConnection(testingT *testing.T) {
	ctx := context.Background()
	ingressRepository, database, _ := newIngressTestEnvironment(testingT)
	exportRepository := NewExportRepository(database)
	first := newTestIngress("a.launchs.org")
	if createError := ingressRepository.Create(ctx, first); createError != nil {
		testingT.Fatalf("ingress を作れるはず: %v", createError)
	}
	var appError *apperror.Error
	// つないでいなければ not_found。
	if _, findError := exportRepository.FindConnection(ctx, "user-a"); !errors.As(findError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("つないでいなければ not_found のはず: %v", findError)
	}
	connection := &model.GoogleDriveConnection{UserID: "user-a", AccountEmail: "me@example.com", Credential: []byte("enc"), Status: model.ExportStatusActive}
	if saveError := exportRepository.SaveConnection(ctx, connection); saveError != nil {
		testingT.Fatalf("保存できるはず: %v", saveError)
	}
	drive := newTestExportDestination("d1", first.ID, model.ExportKindGoogleDrive)
	if createError := exportRepository.CreateDestination(ctx, drive); createError != nil {
		testingT.Fatalf("Drive を作れるはず: %v", createError)
	}
	// 再接続待ちにすると、接続も転送も、再接続待ちになり、動かす転送から外れる。
	if markError := exportRepository.MarkConnectionReauthRequired(ctx, "user-a", "許可が取り消されました"); markError != nil {
		testingT.Fatalf("再接続待ちにできるはず: %v", markError)
	}
	if reloaded, _ := exportRepository.FindConnection(ctx, "user-a"); reloaded.IsActive() || reloaded.LastError == "" {
		testingT.Errorf("接続が再接続待ちになるはず: %+v", reloaded)
	}
	if runnable, _ := exportRepository.ListRunnable(ctx, time.Now().Add(time.Hour)); len(runnable) != 0 {
		testingT.Errorf("再接続待ちは動かさないはず: %+v", runnable)
	}
	// つなぎ直す(上書き)と、転送が、動ける状態に戻る。
	connection.Credential = []byte("enc2")
	if saveError := exportRepository.SaveConnection(ctx, connection); saveError != nil {
		testingT.Fatalf("つなぎ直せるはず: %v", saveError)
	}
	if reloaded, _ := exportRepository.FindConnection(ctx, "user-a"); !reloaded.IsActive() || string(reloaded.Credential) != "enc2" {
		testingT.Errorf("つなぎ直すと、使える状態になるはず: %+v", reloaded)
	}
	if runnable, _ := exportRepository.ListRunnable(ctx, time.Now().Add(time.Hour)); len(runnable) != 1 {
		testingT.Errorf("転送が動ける状態に戻るはず: %+v", runnable)
	}
	// フォルダとファイルの記録。
	if saveError := exportRepository.SaveProjectFolder(ctx, "user-a", "project-00000001", "folder-1"); saveError != nil {
		testingT.Fatalf("フォルダを保存できるはず: %v", saveError)
	}
	_ = exportRepository.SaveProjectFolder(ctx, "user-a", "project-00000001", "folder-2")
	if folderID, _ := exportRepository.FindProjectFolder(ctx, "user-a", "project-00000001"); folderID != "folder-2" {
		testingT.Errorf("上書きされるはず: %q", folderID)
	}
	if saveError := exportRepository.SaveDriveFile(ctx, &DriveFile{DestinationID: drive.ID, Day: "2026-11-26", Part: 1, FileID: "file-1", Bytes: 10, LogRows: 1}); saveError != nil {
		testingT.Fatalf("ファイルを記録できるはず: %v", saveError)
	}
	_ = exportRepository.SaveDriveFile(ctx, &DriveFile{DestinationID: drive.ID, Day: "2026-11-26", Part: 2, FileID: "file-2", Bytes: 5, LogRows: 1})
	if file, _ := exportRepository.FindDriveFile(ctx, drive.ID, "2026-11-26"); file == nil || file.Part != 2 || file.FileID != "file-2" {
		testingT.Errorf("一番新しい分割が返るはず: %+v", file)
	}
	if file, _ := exportRepository.FindDriveFile(ctx, drive.ID, "2026-11-27"); file != nil {
		testingT.Errorf("別の日は無いはず: %+v", file)
	}
	// 解除すると、接続と、Drive の転送と、フォルダの記録が消える。
	if _, deleteError := exportRepository.DeleteConnection(ctx, "user-a"); deleteError != nil {
		testingT.Fatalf("解除できるはず: %v", deleteError)
	}
	var count int64
	database.Model(&model.ExportDestination{}).Count(&count)
	if folderID, _ := exportRepository.FindProjectFolder(ctx, "user-a", "project-00000001"); count != 0 || folderID != "" {
		testingT.Errorf("転送とフォルダの記録も消えるはず: %d %q", count, folderID)
	}
	if _, deleteError := exportRepository.DeleteConnection(ctx, "user-a"); !errors.As(deleteError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("つないでいなければ not_found のはず: %v", deleteError)
	}
}
