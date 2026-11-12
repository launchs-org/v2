// Package apperror は、アプリケーション全体で使うエラーの型を定義する。
//
// エラーの種類(Kind)は、共通ルール(docs/endpoints/common.md)のエラーレスポンスの
// code と1対1で対応する。service と repository はこのエラーを返し、
// controller のエラーハンドラーが HTTP のステータスとレスポンスのボディに変換する。
package apperror

import "net/http"

// Kind は、アプリケーションのエラーの種類を表す。
//
// 値は、そのままエラーレスポンスの code(JSON)として返される。
type Kind string

const (
	// KindInvalidRequest は、リクエストの形式や値が不正なとき(HTTP 400)。
	KindInvalidRequest Kind = "invalid_request"

	// KindUnauthorized は、認証が無い、トークンが無効・期限切れ、ログインに失敗したとき(HTTP 401)。
	KindUnauthorized Kind = "unauthorized"

	// KindNotFound は、リソースが存在しない、または他のユーザーのリソースのとき(HTTP 404)。
	KindNotFound Kind = "not_found"

	// KindConflict は、排他制御(applying や deleting の最中)、状態に合わない操作、
	// 名前などの重複のとき(HTTP 409)。
	KindConflict Kind = "conflict"

	// KindPayloadTooLarge は、アップロードが上限を超えたとき(HTTP 413)。
	KindPayloadTooLarge Kind = "payload_too_large"

	// KindTooManyRequests は、レート制限にかかったとき(HTTP 429)。
	KindTooManyRequests Kind = "too_many_requests"

	// KindInternal は、サーバー内部のエラーのとき(HTTP 500)。
	KindInternal Kind = "internal_error"
)

// StatusCode は、この種類に対応する HTTP のステータスコードを返す。
//
// 戻り値:
//   - 種類に対応するステータスコード。未知の種類は、サーバー内部のエラーとして 500 を返す。
func (kind Kind) StatusCode() int {
	switch kind {
	case KindInvalidRequest:
		return http.StatusBadRequest
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindPayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case KindTooManyRequests:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
