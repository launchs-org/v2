// Package pagination は、一覧のページング(limit + cursor)の共通の部品を持つ。
//
// 一覧は、作成日時の降順で返す。カーソルは、「最後に返した行の、作成日時と ID」を
// 文字列にしたもので、クライアントは中身を解釈しない。
package pagination

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"launchs/shared/apperror"
)

const (
	// DefaultLimit は、limit を省略したときの件数。
	DefaultLimit = 20

	// MaximumLimit は、limit に指定できる最大の件数。
	MaximumLimit = 100
)

// Position は、一覧の中の位置。この位置より後ろ(より古い行)から、次のページが始まる。
type Position struct {
	// CreatedAt は、最後に返した行の作成日時。
	CreatedAt time.Time

	// ID は、最後に返した行の ID。作成日時が同じ行の順序を決めるために使う。
	ID string
}

// EncodeCursor は、位置を、クライアントに返すカーソルの文字列にする。
//
// 引数:
//   - position: 最後に返した行の位置。
//
// 戻り値:
//   - カーソルの文字列(URL に入れても安全な文字だけ)。
func EncodeCursor(position Position) string {
	// 作成日時(マイクロ秒)と ID を「|」でつないだ文字列を作る。マイクロ秒にするのは、DB の精度に合わせるため。
	rawText := strconv.FormatInt(position.CreatedAt.UnixMicro(), 10) + "|" + position.ID
	// URL に入れても安全で、中身を解釈させない文字列にして返す。
	return base64.RawURLEncoding.EncodeToString([]byte(rawText))
}

// DecodeCursor は、カーソルの文字列を、位置に戻す。
//
// 引数:
//   - cursor: クライアントから受け取ったカーソル。
//
// 戻り値:
//   - 位置。
//   - カーソルが不正なときの、invalid_request のエラー。
func DecodeCursor(cursor string) (Position, error) {
	// 不正なときに返すエラーを、最初に1つ作っておく(どの失敗でも同じ内容を返すため)。
	invalidCursorError := apperror.New(apperror.KindInvalidRequest, "cursor が正しくありません")

	// base64 を元のバイト列に戻す。
	rawBytes, decodeError := base64.RawURLEncoding.DecodeString(cursor)
	// base64 として読めないカーソルは、不正として扱う。
	if decodeError != nil {
		// 不正なカーソルとして、空の位置とエラーを返す。
		return Position{}, invalidCursorError
	}
	// 「|」の前後で、日時と ID に分ける。
	microsecondsText, id, found := strings.Cut(string(rawBytes), "|")
	// 区切りが無い、または ID が空のカーソルは、不正として扱う。
	if !found || id == "" {
		// 不正なカーソルとして、空の位置とエラーを返す。
		return Position{}, invalidCursorError
	}
	// 日時の部分を整数(マイクロ秒)として読む。
	microseconds, parseError := strconv.ParseInt(microsecondsText, 10, 64)
	// 日時が数字でないカーソルは、不正として扱う。
	if parseError != nil {
		// 不正なカーソルとして、空の位置とエラーを返す。
		return Position{}, invalidCursorError
	}
	// マイクロ秒を UTC の日時に戻して、位置として返す。
	return Position{CreatedAt: time.UnixMicro(microseconds).UTC(), ID: id}, nil
}

// ParseLimit は、limit のクエリの文字列を件数にする。
//
// 引数:
//   - limitText: limit のクエリの値。省略(空)のときは既定の件数にする。
//
// 戻り値:
//   - 件数(1〜MaximumLimit)。
//   - 整数でない、または範囲外のときの、invalid_request のエラー。
func ParseLimit(limitText string) (int, error) {
	// limit が省略されたときは、既定の件数にする。
	if limitText == "" {
		// 既定の件数を返す。
		return DefaultLimit, nil
	}
	// 文字列を整数として読む。
	limit, parseError := strconv.Atoi(limitText)
	// 整数でない、または範囲(1〜最大)の外なら、不正として扱う。
	if parseError != nil || limit < 1 || limit > MaximumLimit {
		// 利用者が直せるように、指定できる範囲を含むエラーを返す。
		return 0, apperror.New(apperror.KindInvalidRequest, fmt.Sprintf("limit は 1〜%d の整数で指定してください", MaximumLimit))
	}
	// 正しい件数を返す。
	return limit, nil
}
