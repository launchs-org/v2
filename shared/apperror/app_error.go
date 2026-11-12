package apperror

import (
	"errors"
	"fmt"
)

// Error は、利用者に返してよい情報(種類とメッセージ)と、内部の原因を持つエラー。
//
// service と repository は、利用者に返す必要があるエラーを、この型で返す。
type Error struct {
	// Kind は、エラーの種類。HTTP のステータスと、レスポンスの code を決める。
	Kind Kind

	// Message は、利用者に返してよいメッセージ。
	// SQL や k8s のエラーなどの内部情報を入れてはいけない。
	Message string

	// Cause は、内部の原因になったエラー。利用者には返さず、ログにだけ出す。無ければ nil。
	Cause error
}

// Error は、ログに出すための文字列を返す(error インターフェースの実装)。
//
// 「種類: メッセージ」の形で、原因があれば「: 原因」を後ろに足す。
// この文字列には内部の情報が入るので、利用者に返してはいけない。
//
// 戻り値:
//   - ログ用の文字列。
func (appError *Error) Error() string {
	if appError.Cause == nil {
		return fmt.Sprintf("%s: %s", appError.Kind, appError.Message)
	}
	return fmt.Sprintf("%s: %s: %s", appError.Kind, appError.Message, appError.Cause)
}

// Unwrap は、内部の原因になったエラーを返す。
//
// errors.Is や errors.As が、原因のエラーまで辿れるようにするためのメソッド。
//
// 戻り値:
//   - 原因のエラー。無ければ nil。
func (appError *Error) Unwrap() error {
	return appError.Cause
}

// New は、原因を持たない Error を作る。
//
// 引数:
//   - kind: エラーの種類。
//   - message: 利用者に返してよいメッセージ。
//
// 戻り値:
//   - 作成した Error。
func New(kind Kind, message string) *Error {
	return &Error{Kind: kind, Message: message}
}

// Wrap は、内部の原因になったエラーを持つ Error を作る。
//
// 引数:
//   - kind: エラーの種類。
//   - message: 利用者に返してよいメッセージ。原因の内容は入れない。
//   - cause: 内部の原因になったエラー。ログにだけ出る。
//
// 戻り値:
//   - 作成した Error。
func Wrap(kind Kind, message string, cause error) *Error {
	return &Error{Kind: kind, Message: message, Cause: cause}
}

// KindOf は、エラーの種類を返す。
//
// fmt.Errorf の %w で包まれていても、中にある Error の種類を見つける。
//
// 引数:
//   - err: 調べるエラー。
//
// 戻り値:
//   - Error を含むときは、その種類。
//   - Error を含まない通常のエラーは KindInternal。
//   - err が nil のときは空の Kind。
func KindOf(err error) Kind {
	if err == nil {
		return Kind("")
	}

	var appError *Error
	if errors.As(err, &appError) {
		return appError.Kind
	}
	return KindInternal
}
