// Package googledrive は、Google の OAuth 2.0 と Drive API(v3)の、必要な部分だけを呼ぶ。
//
// 権限(scope)は、drive.file(このアプリが作ったファイルとフォルダだけ)と、表示用のメールアドレス(openid email)だけ。
// 呼び出し先の URL は、設定で差し替えられる(試験で、偽の Google の API を使うため)。
package googledrive

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

const (
	// Scopes は、求める権限。drive.file は、このアプリが作った(または、利用者が開くと選んだ)ファイルだけに触れる。
	Scopes = "https://www.googleapis.com/auth/drive.file openid email"

	// FolderMimeType は、Drive のフォルダの種類。
	FolderMimeType = "application/vnd.google-apps.folder"

	// maxResponseBytes は、API の応答として読む大きさの上限(ダウンロードするログのファイルは、別)。
	maxResponseBytes = 1 << 20

	// MaxDownloadBytes は、ダウンロードするファイルの大きさの上限(大きすぎるときは、別のファイルに分けるので、これを超えない)。
	MaxDownloadBytes = 64 << 20
)

// ErrInvalidGrant は、合鍵が効かないときのエラー(許可が取り消された、長く使わなかった、など)。再接続が要る。
var ErrInvalidGrant = errors.New("Google の許可が無効です。Google Drive に、もう一度接続してください")

// ErrNotFound は、フォルダやファイルが無い(消された、ゴミ箱に入った)ときのエラー。
var ErrNotFound = errors.New("Drive のフォルダまたはファイルが見つかりません")

// Config は、クライアントの設定。
type Config struct {
	// ClientID と ClientSecret は、Google Cloud の OAuth クライアント。
	ClientID     string
	ClientSecret string

	// RedirectURL は、同意のあとに戻る先(Google Cloud に登録したものと同じ)。
	RedirectURL string

	// 以下は、呼び出し先の URL。空なら、本物の Google。試験で差し替える。
	AuthURL    string
	TokenURL   string
	RevokeURL  string
	APIBase    string
	UploadBase string
}

// withDefaults は、空の URL を、本物の Google のものにする。
//
// 戻り値:
//   - 補った設定。
func (config Config) withDefaults() Config {
	// 空のものだけ、既定にする。
	if config.AuthURL == "" {
		config.AuthURL = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if config.TokenURL == "" {
		config.TokenURL = "https://oauth2.googleapis.com/token"
	}
	if config.RevokeURL == "" {
		config.RevokeURL = "https://oauth2.googleapis.com/revoke"
	}
	if config.APIBase == "" {
		config.APIBase = "https://www.googleapis.com/drive/v3"
	}
	if config.UploadBase == "" {
		config.UploadBase = "https://www.googleapis.com/upload/drive/v3"
	}
	return config
}

// HasClient は、OAuth のクライアント(ID と秘密)が設定されているかを返す(合鍵で Drive に書くだけなら、これで足りる)。
//
// 戻り値:
//   - 揃っていれば true。
func (config Config) HasClient() bool {
	// 2 つとも、空でないこと。
	return config.ClientID != "" && config.ClientSecret != ""
}

// Enabled は、Google Drive に新しくつなげる設定(クライアント ID と秘密と、戻り先)が揃っているかを返す。
//
// 戻り値:
//   - 揃っていれば true。
func (config Config) Enabled() bool {
	// クライアントと、戻り先が、空でないこと。
	return config.HasClient() && config.RedirectURL != ""
}

// Client は、Google の API を呼ぶ。
type Client struct {
	// config は、設定。
	config Config

	// httpClient は、HTTP クライアント。
	httpClient *http.Client
}

// NewClient は、Client を作る。
//
// 引数:
//   - config: 設定。
//   - httpClient: HTTP クライアント(nil なら、60 秒の待ち時間のもの)。
//
// 戻り値:
//   - 作ったクライアント。
func NewClient(config Config, httpClient *http.Client) *Client {
	// HTTP クライアントが無ければ、待ち時間つきのものを使う。
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{config: config.withDefaults(), httpClient: httpClient}
}

// Enabled は、Google Drive に新しくつなげる設定が揃っているかを返す。
//
// 戻り値:
//   - 揃っていれば true。
func (client *Client) Enabled() bool {
	// 設定に任せる。
	return client != nil && client.config.Enabled()
}

// CanWrite は、合鍵で Drive に書ける(OAuth のクライアントが設定されている)かを返す。
//
// 戻り値:
//   - 書けるなら true。
func (client *Client) CanWrite() bool {
	// 設定に任せる。
	return client != nil && client.config.HasClient()
}

// AuthCodeURL は、Google の同意の画面の URL を作る。
//
// 引数:
//   - state: 戻ってきたときに、確かめる値(署名つき)。
//
// 戻り値:
//   - 同意の画面の URL。合鍵(リフレッシュトークン)を必ず得るため、offline と、毎回の同意を求める。
func (client *Client) AuthCodeURL(state string) string {
	// 必要なパラメーターを付ける。
	query := url.Values{}
	query.Set("client_id", client.config.ClientID)
	query.Set("redirect_uri", client.config.RedirectURL)
	query.Set("response_type", "code")
	query.Set("scope", Scopes)
	query.Set("access_type", "offline")
	query.Set("prompt", "consent")
	query.Set("include_granted_scopes", "false")
	query.Set("state", state)
	return client.config.AuthURL + "?" + query.Encode()
}

// Token は、コードの交換の結果。
type Token struct {
	// RefreshToken は、合鍵(長く使える)。
	RefreshToken string

	// AccessToken は、短い間だけ使える鍵。
	AccessToken string

	// Email は、つないだ Google アカウントのメールアドレス(分からなければ空)。
	Email string
}

// tokenResponse は、トークンの API の応答。
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

// Exchange は、同意のあとに戻ってきたコードを、合鍵に交換する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - code: Google が戻した、1 回だけ使えるコード。
//
// 戻り値:
//   - トークン。合鍵が得られなかったとき(以前の許可が残っているなど)は、エラー。
func (client *Client) Exchange(requestContext context.Context, code string) (Token, error) {
	// コードと、クライアントの情報を送る。
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", client.config.ClientID)
	form.Set("client_secret", client.config.ClientSecret)
	form.Set("redirect_uri", client.config.RedirectURL)
	form.Set("grant_type", "authorization_code")
	response, postError := client.postToken(requestContext, form)
	if postError != nil {
		return Token{}, postError
	}
	// 合鍵が無ければ、使えない(Google が、すでに許可済みのときは、返さないことがある)。
	if response.RefreshToken == "" {
		return Token{}, errors.New("Google から、合鍵を受け取れませんでした。Google アカウントの「サードパーティのアクセス」から、launchs を一度削除して、もう一度つないでください")
	}
	return Token{RefreshToken: response.RefreshToken, AccessToken: response.AccessToken, Email: emailFromIDToken(response.IDToken)}, nil
}

// postToken は、トークンの API を呼ぶ。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - form: 送る内容。
//
// 戻り値:
//   - 応答。invalid_grant は ErrInvalidGrant。
func (client *Client) postToken(requestContext context.Context, form url.Values) (tokenResponse, error) {
	// POST して、応答を読む。
	request, requestError := http.NewRequestWithContext(requestContext, http.MethodPost, client.config.TokenURL, strings.NewReader(form.Encode()))
	if requestError != nil {
		return tokenResponse{}, requestError
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, doError := client.httpClient.Do(request)
	if doError != nil {
		return tokenResponse{}, fmt.Errorf("Google に接続できませんでした: %w", doError)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	var decoded tokenResponse
	_ = json.Unmarshal(body, &decoded)
	// 合鍵が効かないときは、専用のエラー(再接続が要る)。
	if decoded.Error == "invalid_grant" {
		return tokenResponse{}, ErrInvalidGrant
	}
	if response.StatusCode != http.StatusOK || decoded.AccessToken == "" {
		return tokenResponse{}, fmt.Errorf("Google の認証に失敗しました(%d %s)", response.StatusCode, decoded.Error)
	}
	return decoded, nil
}

// emailFromIDToken は、ID トークン(JWT)から、メールアドレスを取り出す。
// 署名は確かめない(トークンの API から、TLS で直接受け取ったものを、表示にだけ使う。認可の判断には使わない)。
//
// 引数:
//   - idToken: ID トークン。
//
// 戻り値:
//   - メールアドレス。読めなければ空。
func emailFromIDToken(idToken string) string {
	// JWT の真ん中(ペイロード)を、base64url で戻して、email を読む。
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, decodeError := base64.RawURLEncoding.DecodeString(parts[1])
	if decodeError != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(payload, &claims)
	return claims.Email
}

// Revoke は、合鍵を取り消す(Google の側で、許可を無効にする)。失敗しても、呼び出し側は、続けてよい。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - refreshToken: 取り消す合鍵。
//
// 戻り値:
//   - 取り消せなかったときのエラー。
func (client *Client) Revoke(requestContext context.Context, refreshToken string) error {
	// 合鍵を送って、取り消す。
	form := url.Values{}
	form.Set("token", refreshToken)
	request, requestError := http.NewRequestWithContext(requestContext, http.MethodPost, client.config.RevokeURL, strings.NewReader(form.Encode()))
	if requestError != nil {
		return requestError
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, doError := client.httpClient.Do(request)
	if doError != nil {
		return doError
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("取り消せませんでした(%d)", response.StatusCode)
	}
	return nil
}

// Session は、合鍵から得た、短い鍵を使って、Drive を操作する(短い鍵は、使い回す)。
type Session struct {
	// client は、親のクライアント。
	client *Client

	// refreshToken は、合鍵。
	refreshToken string

	// accessToken は、今の短い鍵。空なら、まだ取っていない。
	accessToken string
}

// NewSession は、合鍵を使う Session を作る。
//
// 引数:
//   - refreshToken: 合鍵。
//
// 戻り値:
//   - 作った Session。
func (client *Client) NewSession(refreshToken string) *Session {
	// 合鍵だけを持つ(短い鍵は、最初の呼び出しで取る)。
	return &Session{client: client, refreshToken: refreshToken}
}

// ensureToken は、短い鍵を持っていなければ、合鍵で取る。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//
// 戻り値:
//   - 合鍵が効かないときは ErrInvalidGrant。
func (session *Session) ensureToken(requestContext context.Context) error {
	// すでに持っていれば、使う。
	if session.accessToken != "" {
		return nil
	}
	form := url.Values{}
	form.Set("client_id", session.client.config.ClientID)
	form.Set("client_secret", session.client.config.ClientSecret)
	form.Set("refresh_token", session.refreshToken)
	form.Set("grant_type", "refresh_token")
	response, postError := session.client.postToken(requestContext, form)
	if postError != nil {
		return postError
	}
	session.accessToken = response.AccessToken
	return nil
}

// call は、Drive の API を呼ぶ(短い鍵を付ける。期限切れ(401)なら、取り直して、1 回だけやり直す)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - method: HTTP のメソッド。
//   - endpoint: 呼び出す URL。
//   - contentType: 本文の種類(本文が無ければ空)。
//   - body: 本文。
//   - limit: 読む応答の大きさの上限。
//
// 戻り値:
//   - 応答の本文。404 は ErrNotFound、合鍵が効かないときは ErrInvalidGrant。
func (session *Session) call(requestContext context.Context, method string, endpoint string, contentType string, body []byte, limit int64) ([]byte, error) {
	// 最大 2 回(1 回目で期限切れなら、短い鍵を取り直して、もう 1 回)。
	for attempt := 0; attempt < 2; attempt++ {
		if tokenError := session.ensureToken(requestContext); tokenError != nil {
			return nil, tokenError
		}
		request, requestError := http.NewRequestWithContext(requestContext, method, endpoint, bytes.NewReader(body))
		if requestError != nil {
			return nil, requestError
		}
		request.Header.Set("Authorization", "Bearer "+session.accessToken)
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		response, doError := session.client.httpClient.Do(request)
		if doError != nil {
			return nil, fmt.Errorf("Google Drive に接続できませんでした: %w", doError)
		}
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, limit))
		_ = response.Body.Close()
		switch {
		case response.StatusCode == http.StatusUnauthorized && attempt == 0:
			// 短い鍵が切れた。取り直して、もう 1 回。
			session.accessToken = ""
			continue
		case response.StatusCode == http.StatusNotFound:
			return nil, ErrNotFound
		case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden && strings.Contains(string(responseBody), "insufficientPermissions"):
			return nil, ErrInvalidGrant
		case response.StatusCode < 200 || response.StatusCode >= 300:
			return nil, fmt.Errorf("Google Drive がエラーを返しました(%d)", response.StatusCode)
		}
		return responseBody, nil
	}
	return nil, ErrInvalidGrant
}

// fileResource は、Drive のファイルの情報(必要な項目だけ)。
type fileResource struct {
	ID      string `json:"id"`
	Trashed bool   `json:"trashed"`
}

// CreateFolder は、フォルダを作る。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - name: フォルダの名前。
//   - parentID: 親のフォルダの ID(空なら、マイドライブの直下)。
//
// 戻り値:
//   - 作ったフォルダの ID。
func (session *Session) CreateFolder(requestContext context.Context, name string, parentID string) (string, error) {
	// フォルダの情報を、JSON で送る。
	metadata := map[string]any{"name": name, "mimeType": FolderMimeType}
	if parentID != "" {
		metadata["parents"] = []string{parentID}
	}
	encoded, _ := json.Marshal(metadata)
	responseBody, callError := session.call(requestContext, http.MethodPost, session.client.config.APIBase+"/files?fields=id", "application/json", encoded, maxResponseBytes)
	if callError != nil {
		return "", callError
	}
	var created fileResource
	if unmarshalError := json.Unmarshal(responseBody, &created); unmarshalError != nil || created.ID == "" {
		return "", errors.New("フォルダの ID を受け取れませんでした")
	}
	return created.ID, nil
}

// FolderUsable は、フォルダが、まだ使えるか(あって、ゴミ箱に入っていない)を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - folderID: フォルダの ID。
//
// 戻り値:
//   - 使えれば true。無い、またはゴミ箱なら false(エラーではない)。
func (session *Session) FolderUsable(requestContext context.Context, folderID string) (bool, error) {
	// 情報を取って、ゴミ箱に入っていないかを見る。
	responseBody, callError := session.call(requestContext, http.MethodGet, session.client.config.APIBase+"/files/"+url.PathEscape(folderID)+"?fields=id,trashed", "", nil, maxResponseBytes)
	if errors.Is(callError, ErrNotFound) {
		return false, nil
	}
	if callError != nil {
		return false, callError
	}
	var found fileResource
	if unmarshalError := json.Unmarshal(responseBody, &found); unmarshalError != nil {
		return false, unmarshalError
	}
	return !found.Trashed, nil
}

// CreateFile は、ファイルを(中身つきで)作る。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - name: ファイルの名前。
//   - parentID: 親のフォルダの ID。
//   - content: 中身。
//
// 戻り値:
//   - 作ったファイルの ID。
func (session *Session) CreateFile(requestContext context.Context, name string, parentID string, content []byte) (string, error) {
	// 情報(JSON)と中身を、multipart で 1 回で送る。
	metadata, _ := json.Marshal(map[string]any{"name": name, "parents": []string{parentID}})
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	metadataHeader := textproto.MIMEHeader{}
	metadataHeader.Set("Content-Type", "application/json; charset=UTF-8")
	metadataPart, _ := writer.CreatePart(metadataHeader)
	_, _ = metadataPart.Write(metadata)
	contentHeader := textproto.MIMEHeader{}
	contentHeader.Set("Content-Type", "application/x-ndjson")
	contentPart, _ := writer.CreatePart(contentHeader)
	_, _ = contentPart.Write(content)
	_ = writer.Close()
	responseBody, callError := session.call(requestContext, http.MethodPost, session.client.config.UploadBase+"/files?uploadType=multipart&fields=id", "multipart/related; boundary="+writer.Boundary(), buffer.Bytes(), maxResponseBytes)
	if callError != nil {
		return "", callError
	}
	var created fileResource
	if unmarshalError := json.Unmarshal(responseBody, &created); unmarshalError != nil || created.ID == "" {
		return "", errors.New("ファイルの ID を受け取れませんでした")
	}
	return created.ID, nil
}

// UpdateFile は、ファイルの中身を、まるごと置き換える。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - fileID: ファイルの ID。
//   - content: 新しい中身。
//
// 戻り値:
//   - ファイルが無いときは ErrNotFound。
func (session *Session) UpdateFile(requestContext context.Context, fileID string, content []byte) error {
	// 中身だけを、置き換える。
	_, callError := session.call(requestContext, http.MethodPatch, session.client.config.UploadBase+"/files/"+url.PathEscape(fileID)+"?uploadType=media&fields=id", "application/x-ndjson", content, maxResponseBytes)
	return callError
}

// DownloadFile は、ファイルの中身を取る(上限は MaxDownloadBytes)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - fileID: ファイルの ID。
//
// 戻り値:
//   - 中身。ファイルが無い(ゴミ箱を含む)ときは ErrNotFound。
func (session *Session) DownloadFile(requestContext context.Context, fileID string) ([]byte, error) {
	// ゴミ箱のものは、使わない(先に状態を見る)。
	usable, usableError := session.FolderUsable(requestContext, fileID)
	if usableError != nil {
		return nil, usableError
	}
	if !usable {
		return nil, ErrNotFound
	}
	return session.call(requestContext, http.MethodGet, session.client.config.APIBase+"/files/"+url.PathEscape(fileID)+"?alt=media", "", nil, MaxDownloadBytes)
}
