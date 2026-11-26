package model

import (
	"strings"
	"testing"
	"time"
)

// TestValidateWebhookInterval は、選べる間隔だけを受け付けることを確かめる。
func TestValidateWebhookInterval(testingT *testing.T) {
	for _, valid := range []int{10, 30, 60, 300} {
		if validateError := ValidateWebhookInterval(valid); validateError != nil {
			testingT.Errorf("%d 秒は受け付けるはず: %v", valid, validateError)
		}
	}
	for _, invalid := range []int{0, 1, 5, 45, 600} {
		if validateError := ValidateWebhookInterval(invalid); validateError == nil {
			testingT.Errorf("%d 秒は断るはず", invalid)
		}
	}
}

// TestWebhookConfig は、設定の往復と、間隔が無いときの既定を確かめる。
func TestWebhookConfig(testingT *testing.T) {
	destination := ExportDestination{Kind: ExportKindWebhook, Config: NewWebhookConfig(WebhookConfig{URL: "https://example.com/h", IntervalSeconds: 60})}
	config, configError := destination.WebhookConfig()
	if configError != nil || config.URL != "https://example.com/h" || config.IntervalSeconds != 60 {
		testingT.Errorf("往復できるはず: %+v %v", config, configError)
	}
	// 間隔が無い設定は、既定(30 秒)になる。
	legacy := ExportDestination{Kind: ExportKindWebhook, Config: []byte(`{"url":"https://example.com/h"}`)}
	if legacyConfig, _ := legacy.WebhookConfig(); legacyConfig.IntervalSeconds != DefaultWebhookIntervalSeconds {
		testingT.Errorf("既定の間隔のはず: %d", legacyConfig.IntervalSeconds)
	}
	// Google Drive には、webhook の設定は無い。
	if _, driveError := (&ExportDestination{Kind: ExportKindGoogleDrive}).WebhookConfig(); driveError == nil {
		testingT.Errorf("Drive には webhook の設定は無いはず")
	}
}

// TestNewWebhookSecret は、秘密が、毎回違い、決まった形であることを確かめる。
func TestNewWebhookSecret(testingT *testing.T) {
	first, _ := NewWebhookSecret()
	second, _ := NewWebhookSecret()
	if first == second || !strings.HasPrefix(first, "whsec_") || len(first) != len("whsec_")+48 {
		testingT.Errorf("形が正しくない、または重なった: %q %q", first, second)
	}
}

// TestRecordSuccessAndFailure は、成功でカーソルが進んで次の時刻が決まり、失敗で間隔が倍々に延びて上限で止まることを確かめる。
func TestRecordSuccessAndFailure(testingT *testing.T) {
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	destination := &ExportDestination{ID: "export-a", Kind: ExportKindWebhook, Config: NewWebhookConfig(WebhookConfig{URL: "https://example.com", IntervalSeconds: 30})}
	// 失敗: 30 秒、60 秒、120 秒と延びる。
	for index, want := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute} {
		destination.RecordFailure(now, "つながりません")
		if got := destination.NextAttemptAt.Sub(now); got != want || destination.Status != ExportStatusFailing || destination.FailureCount != index+1 {
			testingT.Errorf("%d 回目の失敗の間隔が違う: %v(期待 %v)", index+1, got, want)
		}
	}
	// 何度も失敗しても、上限(1 時間)で止まる。
	for range 20 {
		destination.RecordFailure(now, "つながりません")
	}
	if got := destination.NextAttemptAt.Sub(now); got != MaxExportBackoff {
		testingT.Errorf("上限で止まるはず: %v", got)
	}
	// 成功: カーソルが進み、失敗の記録が消え、選んだ間隔のあとに動く。
	destination.RecordSuccess(now, 123)
	if destination.LastCursor != 123 || destination.FailureCount != 0 || destination.LastError != "" || destination.Status != ExportStatusActive || destination.NextAttemptAt.Sub(now) != 30*time.Second {
		testingT.Errorf("成功の状態が違う: %+v", destination)
	}
	// 長い理由は、切る。
	destination.RecordFailure(now, strings.Repeat("あ", 1000))
	if len([]rune(destination.LastError)) != ExportLastErrorMaxLength {
		testingT.Errorf("理由は切るはず: %d", len([]rune(destination.LastError)))
	}
}

// TestDriveSlot は、枠の番号が決まった範囲で、同じ ID なら同じで、次の枠の時刻が、毎時 5n 分になることを確かめる。
func TestDriveSlot(testingT *testing.T) {
	destination := &ExportDestination{ID: "export-0a1b", Kind: ExportKindGoogleDrive}
	slot := destination.DriveSlot()
	if slot < 0 || slot >= DriveSlotCount || slot != destination.DriveSlot() {
		testingT.Fatalf("枠は 0〜11 で、安定しているはず: %d", slot)
	}
	// 次の枠は、いつも、毎時 5n 分で、今より後。
	now := time.Date(2026, 11, 26, 10, 7, 30, 0, time.UTC)
	next := destination.NextDriveSlotTime(now)
	if !next.After(now) || next.Minute() != slot*5 || next.Second() != 0 || next.Sub(now) > time.Hour {
		testingT.Errorf("次の枠の時刻が違う: %v(枠 %d)", next, slot)
	}
	// 枠の ID が違えば、散る(たくさん作れば、12 の枠の全部に当たる)。
	seen := map[int]bool{}
	for index := range 500 {
		seen[(&ExportDestination{ID: ExportDestinationIDPrefix + strings.Repeat("x", index%7) + string(rune('a'+index%26)) + time.Duration(index).String()}).DriveSlot()] = true
	}
	if len(seen) != DriveSlotCount {
		testingT.Errorf("12 の枠に散るはず: %d", len(seen))
	}
	// 成功すると、Drive は次の枠に動く。
	destination.RecordSuccess(now, 5)
	if destination.NextAttemptAt.Minute() != slot*5 {
		testingT.Errorf("Drive は次の枠に動くはず: %v", destination.NextAttemptAt)
	}
}
