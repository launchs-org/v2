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
	// 原因が無いときは、種類とメッセージだけの短い形にする。
	if appError.Cause == nil {
		// 種類とメッセージを「種類: メッセージ」の形にして返す。
		return fmt.Sprintf("%s: %s", appError.Kind, appError.Message)
	}
	// 原因があるときは、末尾に原因を足して、ログから原因を追えるようにする。
	return fmt.Sprintf("%s: %s: %s", appError.Kind, appError.Message, appError.Cause)
}

// Unwrap は、内部の原因になったエラーを返す。
//
// errors.Is や errors.As が、原因のエラーまで辿れるようにするためのメソッド。
//
// 戻り値:
//   - 原因のエラー。無ければ nil。
func (appError *Error) Unwrap() error {
	// errors.Is と errors.As が原因まで辿れるように、原因をそのまま返す。
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
	// 原因を持たない(Cause が nil の)Error を組み立てて返す。
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
	// 原因を保持した Error を組み立てて返す。原因は利用者には返さず、ログにだけ使う。
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
	// エラーが無いときは、種類も無いので空の Kind を返す。
	if err == nil {
		// 空の Kind を返して、呼び出し側がエラー無しと区別できるようにする。
		return Kind("")
	}

	// errors.As の結果を受け取るための変数を用意する。
	var appError *Error
	// %w で包まれていても、中にある Error を探し出す。
	if errors.As(err, &appError) {
		// 見つかった Error の種類をそのまま返す。
		return appError.Kind
	}
	// Error を含まない通常のエラーは、想定外の失敗として内部エラー扱いにする。
	return KindInternal
}
