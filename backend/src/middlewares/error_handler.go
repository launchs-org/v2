// Package middlewares は、バックエンドの HTTP の共通の処理(ミドルウェアとエラーハンドラー)を提供する。
//
// リクエスト全体にかかわる処理(アクセスログ、エラーの変換)をここに置き、
// どのエンドポイントからも、同じ振る舞いで使えるようにする。
// 組み立てて Echo に登録するのは、server パッケージの役目。
package middlewares

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/shared/apperror"
)

// RequestIDHeaderName は、リクエストを識別する ID を入れる HTTP ヘッダの名前。
// リクエスト ID のミドルウェア、アクセスログ、エラーのログが、同じヘッダを見る。
const RequestIDHeaderName = "X-Request-Id"

// defaultMessagesByKind は、アプリケーションのエラーではなく Echo が返すエラー
// (存在しないパスなど)を変換するときに使う、利用者向けのメッセージ。
var defaultMessagesByKind = map[apperror.Kind]string{
	apperror.KindInvalidRequest:  "リクエストが正しくありません",
	apperror.KindUnauthorized:    "認証が必要です",
	apperror.KindNotFound:        "指定されたものが見つかりません",
	apperror.KindConflict:        "現在の状態では実行できません",
	apperror.KindPayloadTooLarge: "リクエストが大きすぎます",
	apperror.KindTooManyRequests: "リクエストが多すぎます",
}

// NewErrorHandler は、Echo のエラーハンドラーを作る。
//
// ハンドラーやミドルウェアが返したエラーを、共通ルールの形式
// ({"error": {"code": "...", "message": "..."}})のレスポンスに変換する。
// 500 になるエラーは、原因を、リクエスト ID と一緒にログに出す(利用者には返さない)。
//
// 引数:
//   - appLogger: エラーを記録するロガー。
//
// 戻り値:
//   - Echo に設定するエラーハンドラー。
func NewErrorHandler(appLogger *slog.Logger) echo.HTTPErrorHandler {
	// Echo のエラーハンドラーの形をした関数を返す。ロガーは、この関数の中に閉じ込める。
	return func(echoContext *echo.Context, handledError error) {
		// すでにレスポンスを返し始めているときは、書き換えられないので何もしない。
		// ラッパーに包まれたレスポンスから、書き込み済みかどうかを持つ本体を取り出す。
		echoResponse, unwrapError := echo.UnwrapResponse(echoContext.Response())
		// 取り出せて、書き込み済みなら、二重に書かないよう、ここで終わる。
		if unwrapError == nil && echoResponse.Committed {
			// 何もせずに戻る。
			return
		}

		// Echo のエラーを含め、どんなエラーも、アプリケーションのエラーとして扱える形にそろえる。
		convertedError := convertToAppError(handledError)
		// エラーの種類から、返す HTTP ステータスと、共通ルールの形式のボディを決める。
		statusCode, responseBody := apperror.Describe(convertedError)

		// 500 番台(こちらの不具合)のときだけ、原因を調べられるよう、ログに出す。
		if statusCode >= http.StatusInternalServerError {
			// 利用者には返さない元のエラーを、リクエスト ID などと一緒に記録する。
			appLogger.Error("リクエストの処理に失敗した",
				"request_id", echoContext.Response().Header().Get(RequestIDHeaderName),
				"method", echoContext.Request().Method,
				"path", echoContext.Request().URL.Path,
				"error", handledError,
			)
		}

		// 決めたステータスとボディを、JSON で返す。書き込みに失敗したときは、ログに残すしかない。
		if writeError := echoContext.JSON(statusCode, responseBody); writeError != nil {
			// 書き込みの失敗を記録する。
			appLogger.Error("エラーのレスポンスを書き込めませんでした", "error", writeError)
		}
	}
}

// convertToAppError は、Echo が返すエラーを、アプリケーションのエラーに変換する。
//
// すでにアプリケーションのエラー(apperror.Error)を含むときは、そのまま返す。
// HTTP のステータスコードを持つエラー(存在しないパスなど)は、そのステータスに
// 対応する種類のアプリケーションのエラーにする。それ以外は、そのまま返す
// (あとで内部エラーとして扱われる)。
//
// 引数:
//   - handledError: 変換するエラー。
//
// 戻り値:
//   - 変換したエラー。
func convertToAppError(handledError error) error {
	// 包まれたエラーの中から、アプリケーションのエラーを探すための変数を用意する。
	var existingAppError *apperror.Error
	// アプリケーションのエラーを含んでいるときは、種類とメッセージが決まっているので、変換しない。
	if errors.As(handledError, &existingAppError) {
		// 元のエラーをそのまま返す。
		return handledError
	}

	// HTTP のステータスコードを返せるエラーを探すための変数を用意する。
	var statusCoder echo.HTTPStatusCoder
	// ステータスコードを持つエラー(Echo の 404 など)のときは、そのステータスに合う種類に変換する。
	if errors.As(handledError, &statusCoder) {
		// ステータスコードから、エラーの種類を決める。
		kind := kindFromStatusCode(statusCoder.StatusCode())
		// 種類に、利用者向けのメッセージが決まっているときだけ、変換する。
		if message, hasMessage := defaultMessagesByKind[kind]; hasMessage {
			// 種類と、既定のメッセージで、アプリケーションのエラーを作る。
			return apperror.New(kind, message)
		}
	}

	// 変換できないエラーは、そのまま返し、あとで内部エラー(500)として扱わせる。
	return handledError
}

// kindFromStatusCode は、HTTP のステータスコードに対応する、エラーの種類を返す。
//
// 共通ルールで決めているステータスは、対応する種類にする。決めていない 4xx(405 など)は、
// リクエストの不正(invalid_request)として扱う(仮)。5xx などは内部エラーにする。
//
// 引数:
//   - statusCode: HTTP のステータスコード。
//
// 戻り値:
//   - 対応する種類。
func kindFromStatusCode(statusCode int) apperror.Kind {
	// 共通ルールで決めているステータスを、対応する種類に1つずつ変換する。
	switch statusCode {
	case http.StatusUnauthorized:
		return apperror.KindUnauthorized
	case http.StatusNotFound:
		return apperror.KindNotFound
	case http.StatusConflict:
		return apperror.KindConflict
	case http.StatusRequestEntityTooLarge:
		return apperror.KindPayloadTooLarge
	case http.StatusTooManyRequests:
		return apperror.KindTooManyRequests
	}

	// 共通ルールに無い 4xx は、リクエストの不正として扱う。
	if statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError {
		// リクエストの不正の種類を返す。
		return apperror.KindInvalidRequest
	}
	// 4xx 以外は、こちらの不具合として、内部エラーの種類を返す。
	return apperror.KindInternal
}
