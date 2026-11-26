// Package exportsend は、アクセスログを、外(webhook と Google Drive)に送る処理。
// exporter(定期の送信)と backend(テスト送信)が使う。
package exportsend

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"launchs/shared/credential"
	"launchs/shared/exportlog"
	"launchs/shared/model"
	"launchs/shared/safehttp"
)

const (
	// WebhookTimeout は、webhook 1 回の待ち時間の上限。
	WebhookTimeout = 10 * time.Second

	// SignatureHeader などは、webhook に付けるヘッダー。
	SignatureHeader = "X-Launchs-Signature"
	TimestampHeader = "X-Launchs-Timestamp"
	DeliveryHeader  = "X-Launchs-Delivery"
)

// WebhookSender は、webhook に、ログをまとめて送る。
type WebhookSender struct {
	// client は、内部のネットワークに接続しない HTTP クライアント(safehttp)。
	client *http.Client

	// cipher は、署名の秘密を復号する。
	cipher *credential.Cipher

	// options は、URL の検査のゆるめ方(開発用)。
	options safehttp.Options

	// now は、今の時刻を返す(試験で差し替える)。
	now func() time.Time
}

// NewWebhookSender は、WebhookSender を作る。
//
// 引数:
//   - cipher: 署名の秘密を復号するもの。
//   - options: URL と接続の検査のゆるめ方(本番は、ゼロ値)。
//
// 戻り値:
//   - 作った送り手。
func NewWebhookSender(cipher *credential.Cipher, options safehttp.Options) *WebhookSender {
	// 内部に接続しないクライアントを使う。
	return &WebhookSender{client: safehttp.NewClient(WebhookTimeout, options), cipher: cipher, options: options, now: time.Now}
}

// SetNow は、今の時刻を返す関数を差し替える(試験用)。
//
// 引数:
//   - now: 差し替える関数。
func (sender *WebhookSender) SetNow(now func() time.Time) {
	// そのまま差し替える。
	sender.now = now
}

// webhookBody は、webhook に送る本文。
type webhookBody struct {
	// Test が true のときは、テスト送信(ログは入っていない)。
	Test bool `json:"test,omitempty"`

	// ProjectID と IngressID と DestinationID は、送り元を表す。
	ProjectID     string `json:"projectId"`
	IngressID     string `json:"ingressId"`
	DestinationID string `json:"destinationId"`

	// Logs は、ログの行(id の昇順)。
	Logs []exportlog.Line `json:"logs"`
}

// Sign は、webhook の署名を作る。受け取る側も、同じ計算で確かめる。
//
// 引数:
//   - secret: 署名の秘密。
//   - timestamp: 送った時刻(Unix 秒の文字列)。
//   - body: 本文。
//
// 戻り値:
//   - "sha256=" と、HMAC-SHA256(秘密, タイムスタンプ + "." + 本文)の 16 進数。
func Sign(secret string, timestamp string, body []byte) string {
	// タイムスタンプと本文をつないで、HMAC を取る。
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "." + string(body)))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Send は、ログを、webhook にまとめて送る。2xx なら成功。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - destination: 転送の設定(webhook)。
//   - lines: 送るログ(id の昇順)。
//
// 戻り値:
//   - 失敗したとき、利用者に見せられる短い理由のエラー(URL やクエリは、含めない)。
func (sender *WebhookSender) Send(requestContext context.Context, destination model.ExportDestination, lines []exportlog.Line) error {
	// 本文を作って、送る。
	return sender.post(requestContext, destination, webhookBody{ProjectID: destination.ProjectID, IngressID: destination.IngressID, DestinationID: destination.ID, Logs: lines}, lastID(lines))
}

// SendTest は、テスト送信(ログの入っていない、test: true の本文)をする。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - destination: 転送の設定(webhook)。
//
// 戻り値:
//   - 失敗したとき、利用者に見せられる短い理由のエラー。
func (sender *WebhookSender) SendTest(requestContext context.Context, destination model.ExportDestination) error {
	// テストの本文を送る。
	return sender.post(requestContext, destination, webhookBody{Test: true, ProjectID: destination.ProjectID, IngressID: destination.IngressID, DestinationID: destination.ID, Logs: []exportlog.Line{}}, 0)
}

// lastID は、行の最後の id を返す。
//
// 引数:
//   - lines: 行。
//
// 戻り値:
//   - 最後の id(行が無ければ 0)。
func lastID(lines []exportlog.Line) int64 {
	// 昇順なので、最後の行の id。
	if len(lines) == 0 {
		return 0
	}
	return lines[len(lines)-1].ID
}

// post は、本文に署名を付けて、webhook に POST する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - destination: 転送の設定。
//   - payload: 本文。
//   - deliveryLastID: 配送の印に入れる、最後の行の id。
//
// 戻り値:
//   - 失敗したときの、短い理由のエラー。
func (sender *WebhookSender) post(requestContext context.Context, destination model.ExportDestination, payload webhookBody, deliveryLastID int64) error {
	// 設定と秘密を読む。
	config, configError := destination.WebhookConfig()
	if configError != nil {
		return errors.New("設定が壊れています")
	}
	secret, decryptError := sender.cipher.Decrypt(destination.Credential)
	if decryptError != nil {
		return errors.New("署名の秘密を復号できませんでした(暗号化の鍵が変わった可能性があります。webhook を作り直してください)")
	}
	// 送る前に、もう一度 URL を検査する(登録のあとに、規則が変わったときのため)。
	target, urlError := safehttp.ValidateURL(config.URL, sender.options)
	if urlError != nil {
		return urlError
	}
	// 本文と、署名つきのヘッダーで POST する。
	body, _ := json.Marshal(payload)
	timestamp := strconv.FormatInt(sender.now().Unix(), 10)
	request, requestError := http.NewRequestWithContext(requestContext, http.MethodPost, target.String(), bytes.NewReader(body))
	if requestError != nil {
		return errors.New("URL が正しくありません")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "launchs-exporter")
	request.Header.Set(TimestampHeader, timestamp)
	request.Header.Set(SignatureHeader, Sign(secret, timestamp, body))
	request.Header.Set(DeliveryHeader, destination.ID+"-"+strconv.FormatInt(deliveryLastID, 10))
	response, doError := sender.client.Do(request)
	if doError != nil {
		return describeNetworkError(doError)
	}
	defer func() { _ = response.Body.Close() }()
	// 応答の本文は、使わない(読み捨てて、接続を使い回せるようにする)。
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("受け口が %d を返しました", response.StatusCode)
	}
	return nil
}

// describeNetworkError は、接続の失敗を、利用者に見せられる短い理由にする(エラーの文字列には URL が入るので、そのまま出さない)。
//
// 引数:
//   - networkError: 接続のエラー。
//
// 戻り値:
//   - 短い理由。
func describeNetworkError(networkError error) error {
	// 禁止した宛先、タイムアウト、それ以外に分ける。
	var timeoutError net.Error
	switch {
	case errors.Is(networkError, safehttp.ErrBlockedAddress):
		return errors.New("宛先が、内部のネットワークや、予約された範囲です")
	case errors.Is(networkError, context.DeadlineExceeded) || (errors.As(networkError, &timeoutError) && timeoutError.Timeout()):
		return errors.New("受け口が、時間内に応答しませんでした")
	case errors.Is(networkError, context.Canceled):
		return errors.New("中断されました")
	default:
		return errors.New("受け口に接続できませんでした")
	}
}
