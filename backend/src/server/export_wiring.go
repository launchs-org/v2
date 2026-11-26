package server

import (
	"crypto/sha256"
	"os"
	"strings"

	"launchs/shared/credential"
	"launchs/shared/googledrive"
	"launchs/shared/safehttp"
)

// ExportDependencies は、アクセスログの転送が使う、暗号化と Google の設定。Dependencies で渡されなければ、環境変数から読む。
type ExportDependencies struct {
	// Cipher は、秘密の暗号化(環境変数 EXPORT_CREDENTIAL_KEY)。nil なら、転送の機能は無効。
	Cipher *credential.Cipher

	// CredentialKey は、暗号化の鍵の文字列(state の署名の鍵を作るのに使う)。
	CredentialKey string

	// Google は、Google の OAuth クライアントと、戻り先(環境変数 GOOGLE_OAUTH_CLIENT_ID、GOOGLE_OAUTH_CLIENT_SECRET、GOOGLE_OAUTH_REDIRECT_URL)。
	Google googledrive.Config

	// SafeOptions は、webhook の URL の検査のゆるめ方(環境変数 EXPORT_ALLOW_HTTP、EXPORT_ALLOW_PRIVATE_NETWORKS。開発用)。
	SafeOptions safehttp.Options
}

// loadExportDependencies は、環境変数から、転送の設定を読む。鍵が正しくないときは、機能を無効にする(起動は止めない)。
//
// 戻り値:
//   - 読んだ設定。
func loadExportDependencies() ExportDependencies {
	// 暗号化の鍵を読む。無い、または正しくなければ、機能は無効。
	loaded := ExportDependencies{CredentialKey: os.Getenv("EXPORT_CREDENTIAL_KEY")}
	if loaded.CredentialKey != "" {
		if cipher, cipherError := credential.NewCipher(loaded.CredentialKey); cipherError == nil {
			loaded.Cipher = cipher
		}
	}
	// Google の OAuth クライアントと、戻り先。戻り先が無ければ、LAUNCHS_HOST から作る。
	redirectURL := os.Getenv("GOOGLE_OAUTH_REDIRECT_URL")
	if redirectURL == "" {
		if host := strings.TrimSpace(os.Getenv("LAUNCHS_HOST")); host != "" {
			redirectURL = "https://" + host + "/api/v1/export/google/callback"
		}
	}
	loaded.Google = googledrive.Config{ClientID: os.Getenv("GOOGLE_OAUTH_CLIENT_ID"), ClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"), RedirectURL: redirectURL}
	loaded.SafeOptions = safehttp.Options{AllowHTTP: os.Getenv("EXPORT_ALLOW_HTTP") == "true", AllowPrivateNetworks: os.Getenv("EXPORT_ALLOW_PRIVATE_NETWORKS") == "true"}
	return loaded
}

// stateKey は、Google に渡す state の署名の鍵を、暗号化の鍵から作る(用途が違う鍵として、別の値にする)。
//
// 引数:
//   - credentialKey: 暗号化の鍵の文字列。
//
// 戻り値:
//   - 署名の鍵。
func stateKey(credentialKey string) []byte {
	// 用途の名前を付けて、ハッシュにする。
	sum := sha256.Sum256([]byte("launchs-google-oauth-state:" + credentialKey))
	return sum[:]
}
