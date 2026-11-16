// Package turnstile は、Cloudflare Turnstile(人間かどうかの確認)のトークンを、Cloudflare の siteverify API で検証する。
//
// ブラウザのウィジェットが作ったトークンを、シークレットキーといっしょに Cloudflare へ送り、結果を確かめる。
// トークンは、1回しか使えず、期限(300秒)がある(Cloudflare が確かめる)。
package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"launchs/shared/apperror"
)

const (
	// DefaultVerifyURL は、Cloudflare の siteverify の URL。
	DefaultVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

	// MaxTokenLength は、トークンの長さの上限(Cloudflare の仕様)。これより長いものは、送らずに、拒否する。
	MaxTokenLength = 2048

	// requestTimeout は、Cloudflare への問い合わせを待つ時間の上限。
	requestTimeout = 5 * time.Second

	// maxResponseBytes は、Cloudflare のレスポンスを読む大きさの上限。
	maxResponseBytes = 64 * 1024
)

// Options は、Verifier を作るときの設定。
type Options struct {
	// SecretKey は、Turnstile のシークレットキー(必須。ログに出さない)。
	SecretKey string

	// VerifyURL は、siteverify の URL(空なら DefaultVerifyURL)。テストや、Cloudflare を通さない検証で、差し替える。
	VerifyURL string

	// Hostnames は、トークンを作ったページのホスト名として許すもの。空なら、確かめない(Cloudflare のサイトの設定の、ホスト名に任せる)。
	Hostnames []string

	// HTTPClient は、問い合わせに使うクライアント(nil なら、タイムアウト 5 秒のもの)。
	HTTPClient *http.Client
}

// siteverifyResponse は、Cloudflare の siteverify のレスポンス(使う項目だけ)。
type siteverifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
	Hostname   string   `json:"hostname"`
}

// Verifier は、Turnstile のトークンを検証する。複数の goroutine から、同時に使える。
type Verifier struct {
	options Options
}

// NewVerifier は、Verifier を作る。
//
// 引数:
//   - options: 設定。SecretKey は必須。
//
// 戻り値:
//   - 作成した Verifier。
func NewVerifier(options Options) *Verifier {
	// 省略された設定に、既定値を入れる。
	if options.VerifyURL == "" {
		options.VerifyURL = DefaultVerifyURL
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: requestTimeout}
	}
	return &Verifier{options: options}
}

// Verify は、トークンを検証する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - token: ブラウザのウィジェットが作った、トークン。
//   - remoteIP: クライアントの IP(Cloudflare に、確認の材料として渡す。空なら、渡さない)。
//
// 戻り値:
//   - 通れば nil。
//   - トークンが無い・長すぎる・無効・使用済み・期限切れ・許さないホスト名なら、invalid_request。
//   - Cloudflare に届かない、Cloudflare 側の障害なら、service_unavailable(あとで、もう一度)。
//   - シークレットキーが違う(設定の誤り)なら、internal_error(利用者には、詳細を見せない)。
func (verifier *Verifier) Verify(requestContext context.Context, token string, remoteIP string) error {
	// トークンの形を確かめる(送る前に、明らかに無効なものを断る)。
	if strings.TrimSpace(token) == "" {
		return apperror.New(apperror.KindInvalidRequest, "人間であることの確認(turnstileToken)が必要です")
	}
	if len(token) > MaxTokenLength {
		return apperror.New(apperror.KindInvalidRequest, "人間であることの確認に失敗しました。もう一度試してください")
	}
	// Cloudflare に、フォームの形式で送る。
	form := url.Values{"secret": {verifier.options.SecretKey}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	request, requestError := http.NewRequestWithContext(requestContext, http.MethodPost, verifier.options.VerifyURL, strings.NewReader(form.Encode()))
	if requestError != nil {
		return apperror.Wrap(apperror.KindInternal, "確認のリクエストを作れませんでした", requestError)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, doError := verifier.options.HTTPClient.Do(request)
	if doError != nil {
		return apperror.Wrap(apperror.KindUnavailable, "人間であることの確認のサービスに接続できません。しばらくしてから、もう一度試してください", doError)
	}
	defer func() { _ = response.Body.Close() }()
	// Cloudflare 側の障害(5xx)は、あとで、もう一度試してもらう。
	if response.StatusCode >= http.StatusInternalServerError {
		return apperror.New(apperror.KindUnavailable, "人間であることの確認のサービスが、一時的に使えません。しばらくしてから、もう一度試してください")
	}
	// 結果を読む(大きさに上限を付ける)。読めなければ、サービスの異常として扱う。
	var result siteverifyResponse
	if decodeError := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&result); decodeError != nil {
		return apperror.Wrap(apperror.KindUnavailable, "人間であることの確認のサービスから、想定外の応答がありました。しばらくしてから、もう一度試してください", decodeError)
	}
	if !result.Success {
		return classifyFailure(result.ErrorCodes)
	}
	// ホスト名を確かめる(許すものが決まっているときだけ)。別のサイトで作ったトークンを、使い回されないため。
	if len(verifier.options.Hostnames) > 0 && !slices.Contains(verifier.options.Hostnames, result.Hostname) {
		return apperror.New(apperror.KindInvalidRequest, "人間であることの確認に失敗しました。もう一度試してください")
	}
	return nil
}

// classifyFailure は、Cloudflare が success=false を返したときの、error-codes から、エラーの種類を決める。
//
// 引数:
//   - codes: Cloudflare の error-codes。
//
// 戻り値:
//   - シークレットキーの誤りは internal_error、Cloudflare 側の内部エラーは service_unavailable、それ以外(トークンの問題)は invalid_request。
func classifyFailure(codes []string) error {
	// 設定の誤り(シークレットキー)は、利用者の責任ではないので、内部のエラーにする。
	for _, code := range codes {
		if code == "missing-input-secret" || code == "invalid-input-secret" {
			// 利用者には、設定の詳細を見せない(原因は、ログに残る)。
			return apperror.Wrap(apperror.KindInternal, "人間であることの確認を処理できませんでした", errors.New("Turnstile のシークレットキーが正しくありません: "+strings.Join(codes, ",")))
		}
	}
	// Cloudflare 側の内部エラーは、あとで、もう一度。
	if slices.Contains(codes, "internal-error") {
		return apperror.New(apperror.KindUnavailable, "人間であることの確認のサービスが、一時的に使えません。しばらくしてから、もう一度試してください")
	}
	// それ以外は、トークンの問題(無い、無効、使用済み、期限切れ)。
	return apperror.New(apperror.KindInvalidRequest, "人間であることの確認に失敗しました。もう一度試してください")
}
