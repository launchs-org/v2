package service

import (
	"errors"
	"testing"
	"time"

	"launchs/shared/apperror"
)

// TestOAuthState は、state の署名が合うときだけ受け付け、期限切れ・改ざん・別の鍵のものを拒否することを確かめる。
func TestOAuthState(testingT *testing.T) {
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	exportService := &ExportService{stateKey: []byte("key-1"), now: func() time.Time { return now }}
	// 合っていて、期限内なら、ユーザーが取り出せる。
	signed := exportService.signState(oauthState{UserID: "user-a", ExpiresAt: now.Add(5 * time.Minute).Unix(), Nonce: "n"})
	if state, verifyError := exportService.verifyState(signed); verifyError != nil || state.UserID != "user-a" {
		testingT.Fatalf("受け付けるはず: %+v %v", state, verifyError)
	}
	// 期限が切れたら、拒否する。
	exportService.now = func() time.Time { return now.Add(6 * time.Minute) }
	var appError *apperror.Error
	if _, expiredError := exportService.verifyState(signed); !errors.As(expiredError, &appError) || appError.Kind != apperror.KindInvalidRequest {
		testingT.Errorf("期限切れは拒否するはず: %v", expiredError)
	}
	// 改ざん、署名なし、別の鍵で署名したものは、拒否する。
	exportService.now = func() time.Time { return now }
	other := &ExportService{stateKey: []byte("key-2"), now: exportService.now}
	for name, raw := range map[string]string{"改ざん": "x" + signed, "署名なし": "abc", "空": "", "別の鍵": other.signState(oauthState{UserID: "user-a", ExpiresAt: now.Add(time.Minute).Unix()})} {
		if _, verifyError := exportService.verifyState(raw); verifyError == nil {
			testingT.Errorf("%s は拒否するはず", name)
		}
	}
	// ユーザーが空の state は、拒否する。
	if _, emptyError := exportService.verifyState(exportService.signState(oauthState{ExpiresAt: now.Add(time.Minute).Unix()})); emptyError == nil {
		testingT.Errorf("ユーザーが空の state は拒否するはず")
	}
}
