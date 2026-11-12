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
	rawText := strconv.FormatInt(position.CreatedAt.UnixMicro(), 10) + "|" + position.ID
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
	invalidCursorError := apperror.New(apperror.KindInvalidRequest, "cursor が正しくありません")

	rawBytes, decodeError := base64.RawURLEncoding.DecodeString(cursor)
	if decodeError != nil {
		return Position{}, invalidCursorError
	}
	microsecondsText, id, found := strings.Cut(string(rawBytes), "|")
	if !found || id == "" {
		return Position{}, invalidCursorError
	}
	microseconds, parseError := strconv.ParseInt(microsecondsText, 10, 64)
	if parseError != nil {
		return Position{}, invalidCursorError
	}
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
	if limitText == "" {
		return DefaultLimit, nil
	}
	limit, parseError := strconv.Atoi(limitText)
	if parseError != nil || limit < 1 || limit > MaximumLimit {
		return 0, apperror.New(apperror.KindInvalidRequest, fmt.Sprintf("limit は 1〜%d の整数で指定してください", MaximumLimit))
	}
	return limit, nil
}
