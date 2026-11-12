package apperror

import "errors"

// internalErrorMessage は、内部エラーのときに、利用者へ返す一般的なメッセージ。
// 元のエラーの内容(SQL や k8s のエラーなど)は、ここに入れない。
const internalErrorMessage = "サーバー内部でエラーが発生しました"

// ErrorBody は、エラーレスポンスの error の中身。
type ErrorBody struct {
	// Code は、エラーの種類(Kind の値)。
	Code string `json:"code"`

	// Message は、利用者に返してよいメッセージ。
	Message string `json:"message"`
}

// ErrorResponse は、エラーのときに返すレスポンスのボディ。
//
// 共通ルール(docs/endpoints/common.md)の形式で、
// {"error": {"code": "not_found", "message": "..."}} になる。
type ErrorResponse struct {
	// Error は、エラーの内容。
	Error ErrorBody `json:"error"`
}

// Describe は、エラーを、HTTP のステータスコードとレスポンスのボディに変換する。
//
// Error を含むエラーは、その種類とメッセージを使う。Error を含まない通常のエラーは、
// 内部エラー(500)として、一般的なメッセージを返す。元のエラーの内容は、利用者に漏らさない。
//
// 引数:
//   - err: 変換するエラー。nil を渡してはいけない。
//
// 戻り値:
//   - HTTP のステータスコード。
//   - レスポンスのボディ。
func Describe(err error) (int, ErrorResponse) {
	var appError *Error
	if !errors.As(err, &appError) {
		return KindInternal.StatusCode(), buildErrorResponse(KindInternal, internalErrorMessage)
	}

	message := appError.Message
	if message == "" {
		message = internalErrorMessage
	}
	return appError.Kind.StatusCode(), buildErrorResponse(appError.Kind, message)
}

// buildErrorResponse は、種類とメッセージから、レスポンスのボディを組み立てる。
//
// 引数:
//   - kind: エラーの種類。code になる。
//   - message: 利用者に返すメッセージ。
//
// 戻り値:
//   - 組み立てたレスポンスのボディ。
func buildErrorResponse(kind Kind, message string) ErrorResponse {
	return ErrorResponse{Error: ErrorBody{Code: string(kind), Message: message}}
}
