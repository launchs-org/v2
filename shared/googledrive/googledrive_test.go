package googledrive_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"launchs/shared/googledrive"
	"launchs/shared/googledrive/fakegoogle"
)

// TestAuthCodeURL は、同意の画面の URL に、必要な権限と state が入ることを確かめる。
func TestAuthCodeURL(testingT *testing.T) {
	client := googledrive.NewClient(googledrive.Config{ClientID: "cid", ClientSecret: "sec", RedirectURL: "https://launchs.example/api/v1/export/google/callback"}, nil)
	authURL := client.AuthCodeURL("state-123")
	for _, want := range []string{"accounts.google.com", "client_id=cid", "state=state-123", "access_type=offline", "prompt=consent", "drive.file", "redirect_uri=https%3A%2F%2Flaunchs.example"} {
		if !strings.Contains(authURL, want) {
			testingT.Errorf("%q を含むはず: %s", want, authURL)
		}
	}
	// 権限は、drive.file(と表示用のメール)だけ。Drive 全体の権限は、求めない。
	if strings.Contains(authURL, "auth%2Fdrive&") || strings.Contains(authURL, "auth%2Fdrive+") || strings.HasSuffix(authURL, "auth%2Fdrive") {
		testingT.Errorf("Drive 全体の権限を求めてはいけない: %s", authURL)
	}
	if !client.Enabled() || googledrive.NewClient(googledrive.Config{}, nil).Enabled() {
		testingT.Errorf("設定が揃っているときだけ、有効のはず")
	}
}

// TestExchangeAndSession は、コードの交換(合鍵とメール)、フォルダとファイルの操作、取り消しを、偽の Google で確かめる。
func TestExchangeAndSession(testingT *testing.T) {
	fake := fakegoogle.New()
	defer fake.HTTP.Close()
	client := googledrive.NewClient(fake.Config(), nil)
	ctx := context.Background()
	// コードを交換すると、合鍵とメールアドレスが得られる。
	token, exchangeError := client.Exchange(ctx, "abc")
	if exchangeError != nil || token.RefreshToken != "refresh-abc" || token.Email != "me@example.com" {
		testingT.Fatalf("交換できるはず: %+v %v", token, exchangeError)
	}
	// 無効なコードと、合鍵が無い応答は、エラー。
	if _, badError := client.Exchange(ctx, "bad"); !errors.Is(badError, googledrive.ErrInvalidGrant) {
		testingT.Errorf("無効なコードは ErrInvalidGrant のはず: %v", badError)
	}
	if _, noRefreshError := client.Exchange(ctx, "norefresh"); noRefreshError == nil {
		testingT.Errorf("合鍵が無ければエラーのはず")
	}
	// フォルダとファイル。
	session := client.NewSession(token.RefreshToken)
	rootID, folderError := session.CreateFolder(ctx, "launchs-access-logs", "")
	if folderError != nil {
		testingT.Fatalf("フォルダを作れるはず: %v", folderError)
	}
	projectID, _ := session.CreateFolder(ctx, "my-app", rootID)
	if got := fake.ChildrenNamed(rootID, "my-app"); len(got) != 1 || got[0].ID != projectID || !got[0].Folder {
		testingT.Errorf("親の下に、フォルダができるはず: %+v", got)
	}
	fileID, createError := session.CreateFile(ctx, "a.example.com_2026-11-26.ndjson", projectID, []byte("{\"id\":1}\n"))
	if createError != nil {
		testingT.Fatalf("ファイルを作れるはず: %v", createError)
	}
	if updateError := session.UpdateFile(ctx, fileID, []byte("{\"id\":1}\n{\"id\":2}\n")); updateError != nil {
		testingT.Fatalf("置き換えられるはず: %v", updateError)
	}
	content, downloadError := session.DownloadFile(ctx, fileID)
	if downloadError != nil || string(content) != "{\"id\":1}\n{\"id\":2}\n" {
		testingT.Errorf("置き換えた中身が取れるはず: %q %v", content, downloadError)
	}
	// フォルダの状態: あれば使える。ゴミ箱に入ると、使えない。無ければ、使えない。
	if usable, _ := session.FolderUsable(ctx, projectID); !usable {
		testingT.Errorf("使えるはず")
	}
	fake.Files[projectID].Trashed = true
	if usable, usableError := session.FolderUsable(ctx, projectID); usableError != nil || usable {
		testingT.Errorf("ゴミ箱のフォルダは、使えないはず: %v %v", usable, usableError)
	}
	if usable, usableError := session.FolderUsable(ctx, "nothing"); usableError != nil || usable {
		testingT.Errorf("無いフォルダは、使えないはず: %v %v", usable, usableError)
	}
	// 無いファイルの更新は、ErrNotFound。
	if missingError := session.UpdateFile(ctx, "nothing", []byte("x")); !errors.Is(missingError, googledrive.ErrNotFound) {
		testingT.Errorf("無いファイルは ErrNotFound のはず: %v", missingError)
	}
	// 取り消し。
	if revokeError := client.Revoke(ctx, token.RefreshToken); revokeError != nil || len(fake.RevokedTokens) != 1 || fake.RevokedTokens[0] != "refresh-abc" {
		testingT.Errorf("取り消せるはず: %v %v", revokeError, fake.RevokedTokens)
	}
}

// TestSessionInvalidGrant は、合鍵が効かなくなったとき、ErrInvalidGrant になることを確かめる。
func TestSessionInvalidGrant(testingT *testing.T) {
	fake := fakegoogle.New()
	defer fake.HTTP.Close()
	fake.InvalidRefreshTokens["revoked"] = true
	session := googledrive.NewClient(fake.Config(), nil).NewSession("revoked")
	if _, folderError := session.CreateFolder(context.Background(), "x", ""); !errors.Is(folderError, googledrive.ErrInvalidGrant) {
		testingT.Errorf("合鍵が効かなければ ErrInvalidGrant のはず: %v", folderError)
	}
}
