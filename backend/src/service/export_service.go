package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"launchs/shared/apperror"
	"launchs/shared/credential"
	"launchs/shared/exportsend"
	"launchs/shared/googledrive"
	"launchs/shared/model"
	"launchs/shared/safehttp"
)

// oauthStateLifetime は、Google の同意の画面に渡す state の有効な時間。
const oauthStateLifetime = 10 * time.Minute

// ExportStore は、アクセスログの転送の保存先。repository.ExportRepository が実装する。
type ExportStore interface {
	CreateDestination(requestContext context.Context, destination *model.ExportDestination) error
	ListDestinations(requestContext context.Context, userID string, projectID string, ingressID string) ([]model.ExportDestination, error)
	FindDestination(requestContext context.Context, userID string, projectID string, ingressID string, destinationID string) (model.ExportDestination, error)
	UpdateDestination(requestContext context.Context, destination *model.ExportDestination) error
	DeleteDestination(requestContext context.Context, userID string, projectID string, ingressID string, destinationID string) error
	FindConnection(requestContext context.Context, userID string) (model.GoogleDriveConnection, error)
	SaveConnection(requestContext context.Context, connection *model.GoogleDriveConnection) error
	DeleteConnection(requestContext context.Context, userID string) (model.GoogleDriveConnection, error)
}

// ExportService は、アクセスログの転送(webhook と Google Drive)の設定の操作をまとめる。
// 送る処理そのものは、別コンテナ exporter が行う(テスト送信だけは、ここで行う)。
type ExportService struct {
	// store は、保存先。
	store ExportStore

	// cipher は、秘密の暗号化。nil のとき、機能は無効(暗号化の鍵が設定されていない)。
	cipher *credential.Cipher

	// google は、Google の API のクライアント。
	google *googledrive.Client

	// webhook は、webhook の送り手(テスト送信に使う)。
	webhook *exportsend.WebhookSender

	// drive は、Drive の書き手(テスト送信に使う)。
	drive *exportsend.DriveWriter

	// safeOptions は、webhook の URL の検査のゆるめ方(開発用)。
	safeOptions safehttp.Options

	// stateKey は、Google に渡す state の署名の鍵。
	stateKey []byte

	// now は、今の時刻を返す(試験で差し替える)。
	now func() time.Time
}

// NewExportService は、ExportService を作る。
//
// 引数:
//   - store: 保存先。
//   - cipher: 秘密の暗号化(nil なら、機能は無効)。
//   - google: Google の API のクライアント。
//   - webhook: webhook の送り手。
//   - drive: Drive の書き手。
//   - safeOptions: webhook の URL の検査のゆるめ方。
//   - stateKey: state の署名の鍵。
//
// 戻り値:
//   - 作った service。
func NewExportService(store ExportStore, cipher *credential.Cipher, google *googledrive.Client, webhook *exportsend.WebhookSender, drive *exportsend.DriveWriter, safeOptions safehttp.Options, stateKey []byte) *ExportService {
	// 渡されたものを保持する。時刻は、本物。
	return &ExportService{store: store, cipher: cipher, google: google, webhook: webhook, drive: drive, safeOptions: safeOptions, stateKey: stateKey, now: time.Now}
}

// SetNow は、今の時刻を返す関数を差し替える(試験用)。
//
// 引数:
//   - now: 差し替える関数。
func (exportService *ExportService) SetNow(now func() time.Time) {
	// そのまま差し替える。
	exportService.now = now
}

// Enabled は、転送の機能が使えるか(暗号化の鍵が設定されているか)を返す。
//
// 戻り値:
//   - 使えるなら true。
func (exportService *ExportService) Enabled() bool {
	// 鍵があれば、使える。
	return exportService.cipher != nil
}

// DriveAvailable は、Google Drive に新しくつなげるか(暗号化の鍵と、Google の OAuth の設定が揃っているか)を返す。
//
// 戻り値:
//   - つなげるなら true。
func (exportService *ExportService) DriveAvailable() bool {
	// 鍵と、Google の設定が揃っていること。
	return exportService.Enabled() && exportService.google.Enabled()
}

// requireEnabled は、機能が無効のとき、利用者に見せられるエラーを返す。
//
// 戻り値:
//   - 無効なら service_unavailable。使えるなら nil。
func (exportService *ExportService) requireEnabled() error {
	// 鍵が無ければ、使えない。
	if !exportService.Enabled() {
		return apperror.New(apperror.KindUnavailable, "このサーバーでは、アクセスログの転送が有効になっていません(管理者に、お問い合わせください)")
	}
	return nil
}

// GoogleStatus は、アカウントの Google Drive の状態。
type GoogleStatus struct {
	// Available は、サーバーで Google Drive に新しくつなげるか。
	Available bool

	// Connected は、つないでいるか。
	Connected bool

	// Email は、つないだ Google アカウントのメールアドレス。
	Email string

	// Status は、接続の状態(active / reauth_required)。つないでいなければ空。
	Status string

	// LastError は、最後のエラー。
	LastError string
}

// GoogleStatus は、アカウントの Google Drive の接続の状態を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザー ID。
//
// 戻り値:
//   - 状態。つないでいなくても、エラーにはならない。
func (exportService *ExportService) GoogleStatus(requestContext context.Context, userID string) (GoogleStatus, error) {
	// サーバーで使えるかは、つないでいるかに関わらず、返す。
	status := GoogleStatus{Available: exportService.DriveAvailable()}
	connection, findError := exportService.store.FindConnection(requestContext, userID)
	var appError *apperror.Error
	if errors.As(findError, &appError) && appError.Kind == apperror.KindNotFound {
		return status, nil
	}
	if findError != nil {
		return GoogleStatus{}, findError
	}
	status.Connected = true
	status.Email = connection.AccountEmail
	status.Status = connection.Status
	status.LastError = connection.LastError
	return status, nil
}

// oauthState は、Google に渡す state の中身。
type oauthState struct {
	// UserID は、つなごうとしているユーザー。
	UserID string `json:"u"`

	// ExpiresAt は、有効期限(Unix 秒)。
	ExpiresAt int64 `json:"e"`

	// Nonce は、毎回違う値(同じユーザーでも、state が毎回違うように)。
	Nonce string `json:"n"`
}

// signState は、state を作る(中身を base64url にして、署名を付ける)。
//
// 引数:
//   - state: 中身。
//
// 戻り値:
//   - "<中身>.<署名>" の文字列。
func (exportService *ExportService) signState(state oauthState) string {
	// 中身を base64url にして、HMAC-SHA256 の署名を付ける。
	encoded, _ := json.Marshal(state)
	payload := base64.RawURLEncoding.EncodeToString(encoded)
	mac := hmac.New(sha256.New, exportService.stateKey)
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verifyState は、state の署名と有効期限を確かめて、中身を返す。
//
// 引数:
//   - raw: 戻ってきた state。
//
// 戻り値:
//   - 中身。署名が合わない、期限切れ、壊れているときは invalid_request。
func (exportService *ExportService) verifyState(raw string) (oauthState, error) {
	// 中身と署名に分ける。
	invalid := apperror.New(apperror.KindInvalidRequest, "つなぎ直してください(確認の有効期限が切れたか、不正な要求です)")
	payload, signature, found := strings.Cut(raw, ".")
	if !found {
		return oauthState{}, invalid
	}
	// 署名を、定数時間で比べる。
	mac := hmac.New(sha256.New, exportService.stateKey)
	_, _ = mac.Write([]byte(payload))
	given, decodeError := base64.RawURLEncoding.DecodeString(signature)
	if decodeError != nil || !hmac.Equal(given, mac.Sum(nil)) {
		return oauthState{}, invalid
	}
	// 中身を読んで、期限を確かめる。
	decoded, payloadError := base64.RawURLEncoding.DecodeString(payload)
	if payloadError != nil {
		return oauthState{}, invalid
	}
	var state oauthState
	if unmarshalError := json.Unmarshal(decoded, &state); unmarshalError != nil || state.UserID == "" || exportService.now().Unix() > state.ExpiresAt {
		return oauthState{}, invalid
	}
	return state, nil
}

// GoogleAuthorizeURL は、Google の同意の画面の URL を作る(再接続も、同じ)。
//
// 引数:
//   - userID: つなぐユーザー。
//
// 戻り値:
//   - 同意の画面の URL。サーバーで Drive が設定されていなければ service_unavailable。
func (exportService *ExportService) GoogleAuthorizeURL(userID string) (string, error) {
	// 機能と、Google の設定が揃っていること。
	if requireError := exportService.requireEnabled(); requireError != nil {
		return "", requireError
	}
	if !exportService.DriveAvailable() {
		return "", apperror.New(apperror.KindUnavailable, "このサーバーでは、Google Drive が設定されていません(管理者に、お問い合わせください)")
	}
	// 署名つきの state を作って、同意の画面の URL にする。
	state := exportService.signState(oauthState{UserID: userID, ExpiresAt: exportService.now().Add(oauthStateLifetime).Unix(), Nonce: uuid.NewString()})
	return exportService.google.AuthCodeURL(state), nil
}

// GoogleCallback は、Google から戻ってきたときの処理(state を確かめ、コードを合鍵に交換して、暗号化して保存する)。
// 認証のヘッダーは無いので、state の署名で、ユーザーを確かめる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - code: Google が戻した、1 回だけ使えるコード。
//   - rawState: 戻ってきた state。
//
// 戻り値:
//   - state が不正なら invalid_request。Google との交換に失敗したときは、利用者に見せられる理由つきのエラー。
func (exportService *ExportService) GoogleCallback(requestContext context.Context, code string, rawState string) error {
	// 機能が使えて、state が正しいこと。
	if requireError := exportService.requireEnabled(); requireError != nil {
		return requireError
	}
	state, stateError := exportService.verifyState(rawState)
	if stateError != nil {
		return stateError
	}
	if code == "" {
		return apperror.New(apperror.KindInvalidRequest, "Google から、コードを受け取れませんでした")
	}
	// コードを合鍵に交換する。
	token, exchangeError := exportService.google.Exchange(requestContext, code)
	if exchangeError != nil {
		return apperror.Wrap(apperror.KindInvalidRequest, exchangeError.Error(), exchangeError)
	}
	encrypted, encryptError := exportService.cipher.Encrypt(token.RefreshToken)
	if encryptError != nil {
		return apperror.Wrap(apperror.KindInternal, "合鍵を暗号化できませんでした", encryptError)
	}
	// つなぎ直しのときは、フォルダの記録を引き継ぐ(別の Google アカウントなら、使えないので、書き込みのときに作り直される)。
	connection := model.GoogleDriveConnection{UserID: state.UserID, AccountEmail: token.Email, Credential: encrypted, Status: model.ExportStatusActive}
	if existing, findError := exportService.store.FindConnection(requestContext, state.UserID); findError == nil {
		connection.RootFolderID = existing.RootFolderID
	}
	return exportService.store.SaveConnection(requestContext, &connection)
}

// DisconnectGoogle は、アカウントの Google Drive の接続を解除する(この接続を使う転送も、消える)。
// Google の側の許可も、取り消しを依頼する(失敗しても、解除は進める)。Drive に書き出したファイルは、残す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザー ID。
//
// 戻り値:
//   - つないでいなければ not_found。
func (exportService *ExportService) DisconnectGoogle(requestContext context.Context, userID string) error {
	// 接続と、それを使う転送を、削除する。
	connection, deleteError := exportService.store.DeleteConnection(requestContext, userID)
	if deleteError != nil {
		return deleteError
	}
	// 合鍵が復号できれば、Google の側でも取り消す(できなくても、続ける)。
	if exportService.cipher != nil && exportService.google.CanWrite() {
		if refreshToken, decryptError := exportService.cipher.Decrypt(connection.Credential); decryptError == nil {
			_ = exportService.google.Revoke(requestContext, refreshToken)
		}
	}
	return nil
}

// LogExportInput は、転送の設定を作る・変えるときの入力。
type LogExportInput struct {
	// Kind は、種類(作るときだけ。webhook / google_drive)。
	Kind string

	// URL は、webhook の URL(webhook のとき)。変えるときは、nil なら変えない。
	URL *string

	// IntervalSeconds は、webhook の送る間隔(秒)。nil なら、作るときは既定、変えるときは変えない。
	IntervalSeconds *int

	// Enabled は、有効・無効(変えるときだけ。nil なら変えない)。
	Enabled *bool
}

// ListLogExports は、ingress の転送の設定を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - 設定の一覧。ingress が無いときは not_found。
func (exportService *ExportService) ListLogExports(requestContext context.Context, userID string, projectID string, ingressID string) ([]model.ExportDestination, error) {
	// 保存先に任せる(持ち主でなければ not_found)。
	return exportService.store.ListDestinations(requestContext, userID, projectID, ingressID)
}

// CreateLogExport は、ingress に、転送の設定を作る。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - input: 入力(Kind、webhook なら URL と IntervalSeconds)。
//
// 戻り値:
//   - 作った設定と、webhook の署名の秘密(webhook のときだけ。このときしか見せない)。
//     不正な入力は invalid_request。Google Drive にまだつないでいない、上限、重複は conflict。機能が無効なら service_unavailable。
func (exportService *ExportService) CreateLogExport(requestContext context.Context, userID string, projectID string, ingressID string, input LogExportInput) (model.ExportDestination, string, error) {
	// 機能が使えること。
	if requireError := exportService.requireEnabled(); requireError != nil {
		return model.ExportDestination{}, "", requireError
	}
	destination := model.ExportDestination{ID: model.ExportDestinationIDPrefix + uuid.NewString(), UserID: userID, ProjectID: projectID, IngressID: ingressID, Kind: input.Kind}
	secret := ""
	switch input.Kind {
	case model.ExportKindWebhook:
		// URL と間隔を検査して、秘密を作って暗号化する。
		if input.URL == nil {
			return model.ExportDestination{}, "", apperror.New(apperror.KindInvalidRequest, "webhook の URL を入力してください")
		}
		validated, urlError := safehttp.ValidateURL(*input.URL, exportService.safeOptions)
		if urlError != nil {
			return model.ExportDestination{}, "", apperror.Wrap(apperror.KindInvalidRequest, urlError.Error(), urlError)
		}
		interval := model.DefaultWebhookIntervalSeconds
		if input.IntervalSeconds != nil {
			interval = *input.IntervalSeconds
		}
		if intervalError := model.ValidateWebhookInterval(interval); intervalError != nil {
			return model.ExportDestination{}, "", intervalError
		}
		newSecret, secretError := model.NewWebhookSecret()
		if secretError != nil {
			return model.ExportDestination{}, "", secretError
		}
		encrypted, encryptError := exportService.cipher.Encrypt(newSecret)
		if encryptError != nil {
			return model.ExportDestination{}, "", apperror.Wrap(apperror.KindInternal, "秘密を暗号化できませんでした", encryptError)
		}
		destination.Config = model.NewWebhookConfig(model.WebhookConfig{URL: validated.String(), IntervalSeconds: interval})
		destination.Credential = encrypted
		secret = newSecret
	case model.ExportKindGoogleDrive:
		// アカウントが、Google Drive につながっていること。
		connection, findError := exportService.store.FindConnection(requestContext, userID)
		var appError *apperror.Error
		if errors.As(findError, &appError) && appError.Kind == apperror.KindNotFound {
			return model.ExportDestination{}, "", apperror.New(apperror.KindConflict, "先に、アカウント設定で、Google Drive に接続してください")
		}
		if findError != nil {
			return model.ExportDestination{}, "", findError
		}
		_ = connection
	default:
		return model.ExportDestination{}, "", apperror.New(apperror.KindInvalidRequest, "kind は、webhook か google_drive にしてください")
	}
	if createError := exportService.store.CreateDestination(requestContext, &destination); createError != nil {
		return model.ExportDestination{}, "", createError
	}
	// つないではいるが、再接続待ちのときは、転送も、再接続待ちから始める。
	if destination.Kind == model.ExportKindGoogleDrive {
		if connection, findError := exportService.store.FindConnection(requestContext, userID); findError == nil && !connection.IsActive() {
			destination.Status = model.ExportStatusReauthRequired
			destination.LastError = connection.LastError
			if updateError := exportService.store.UpdateDestination(requestContext, &destination); updateError != nil {
				return model.ExportDestination{}, "", updateError
			}
		}
	}
	return destination, secret, nil
}

// UpdateLogExport は、転送の設定を変える(有効・無効、webhook の URL と間隔)。
// 変えたあとは、失敗の記録を消して、すぐに動けるようにする(Google の再接続待ちは、そのまま)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - destinationID: 転送の設定の ID。
//   - input: 変える項目(nil の項目は、変えない)。
//
// 戻り値:
//   - 変えたあとの設定。無ければ not_found。不正な入力は invalid_request(Google Drive の URL や間隔を変えようとしたときも)。
func (exportService *ExportService) UpdateLogExport(requestContext context.Context, userID string, projectID string, ingressID string, destinationID string, input LogExportInput) (model.ExportDestination, error) {
	// 持ち主の設定を取得する。
	destination, findError := exportService.store.FindDestination(requestContext, userID, projectID, ingressID, destinationID)
	if findError != nil {
		return model.ExportDestination{}, findError
	}
	// webhook の URL と間隔を変える(Google Drive には、無い)。
	if input.URL != nil || input.IntervalSeconds != nil {
		if destination.Kind != model.ExportKindWebhook {
			return model.ExportDestination{}, apperror.New(apperror.KindInvalidRequest, "URL と間隔は、webhook だけで変えられます")
		}
		config, configError := destination.WebhookConfig()
		if configError != nil {
			return model.ExportDestination{}, apperror.Wrap(apperror.KindInternal, "設定を読めませんでした", configError)
		}
		if input.URL != nil {
			validated, urlError := safehttp.ValidateURL(*input.URL, exportService.safeOptions)
			if urlError != nil {
				return model.ExportDestination{}, apperror.Wrap(apperror.KindInvalidRequest, urlError.Error(), urlError)
			}
			config.URL = validated.String()
		}
		if input.IntervalSeconds != nil {
			if intervalError := model.ValidateWebhookInterval(*input.IntervalSeconds); intervalError != nil {
				return model.ExportDestination{}, intervalError
			}
			config.IntervalSeconds = *input.IntervalSeconds
		}
		destination.Config = model.NewWebhookConfig(config)
	}
	if input.Enabled != nil {
		destination.Enabled = *input.Enabled
	}
	// 変えたら、失敗の記録を消して、すぐに動けるようにする(再接続待ちは、接続が直るまで、そのまま)。
	if destination.Status != model.ExportStatusReauthRequired {
		destination.Status = model.ExportStatusActive
		destination.LastError = ""
		destination.FailureCount = 0
	}
	destination.NextAttemptAt = exportService.now()
	if updateError := exportService.store.UpdateDestination(requestContext, &destination); updateError != nil {
		return model.ExportDestination{}, updateError
	}
	return destination, nil
}

// DeleteLogExport は、転送の設定を削除する(Drive に書き出したファイルは、残す)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - destinationID: 転送の設定の ID。
//
// 戻り値:
//   - 無ければ not_found。
func (exportService *ExportService) DeleteLogExport(requestContext context.Context, userID string, projectID string, ingressID string, destinationID string) error {
	// 保存先に任せる。
	return exportService.store.DeleteDestination(requestContext, userID, projectID, ingressID, destinationID)
}

// TestResult は、テスト送信の結果。
type TestResult struct {
	// OK は、送れたか。
	OK bool

	// Message は、送れなかったときの理由(利用者に見せる)。
	Message string
}

// TestLogExport は、テスト送信をする(webhook は、test: true の本文を送る。Drive は、テスト用のファイルを作る)。
// 送れなかったことは、エラーではなく、結果で返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - destinationID: 転送の設定の ID。
//
// 戻り値:
//   - 結果。設定が無ければ not_found。
func (exportService *ExportService) TestLogExport(requestContext context.Context, userID string, projectID string, ingressID string, destinationID string) (TestResult, error) {
	// 機能が使えること。持ち主の設定を取得する。
	if requireError := exportService.requireEnabled(); requireError != nil {
		return TestResult{}, requireError
	}
	destination, findError := exportService.store.FindDestination(requestContext, userID, projectID, ingressID, destinationID)
	if findError != nil {
		return TestResult{}, findError
	}
	// 種類ごとに、送ってみる。
	switch destination.Kind {
	case model.ExportKindWebhook:
		if sendError := exportService.webhook.SendTest(requestContext, destination); sendError != nil {
			return TestResult{Message: sendError.Error()}, nil
		}
	case model.ExportKindGoogleDrive:
		connection, connectionError := exportService.store.FindConnection(requestContext, userID)
		if connectionError != nil || !connection.IsActive() {
			return TestResult{Message: "Google Drive に接続し直してください(アカウント設定)"}, nil
		}
		if exportService.drive == nil {
			return TestResult{Message: "このサーバーでは、Google Drive が設定されていません"}, nil
		}
		if writeError := exportService.drive.WriteTest(requestContext, connection, destination); writeError != nil {
			if errors.Is(writeError, googledrive.ErrInvalidGrant) {
				return TestResult{Message: googledrive.ErrInvalidGrant.Error()}, nil
			}
			return TestResult{Message: "Google Drive に書けませんでした"}, nil
		}
	}
	return TestResult{OK: true}, nil
}
