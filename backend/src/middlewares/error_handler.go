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
	return func(echoContext *echo.Context, handledError error) {
		// すでにレスポンスを返し始めているときは、書き換えられないので何もしない。
		echoResponse, unwrapError := echo.UnwrapResponse(echoContext.Response())
		if unwrapError == nil && echoResponse.Committed {
			return
		}

		convertedError := convertToAppError(handledError)
		statusCode, responseBody := apperror.Describe(convertedError)

		if statusCode >= http.StatusInternalServerError {
			appLogger.Error("リクエストの処理に失敗した",
				"request_id", echoContext.Response().Header().Get(RequestIDHeaderName),
				"method", echoContext.Request().Method,
				"path", echoContext.Request().URL.Path,
				"error", handledError,
			)
		}

		if writeError := echoContext.JSON(statusCode, responseBody); writeError != nil {
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
	var existingAppError *apperror.Error
	if errors.As(handledError, &existingAppError) {
		return handledError
	}

	var statusCoder echo.HTTPStatusCoder
	if errors.As(handledError, &statusCoder) {
		kind := kindFromStatusCode(statusCoder.StatusCode())
		if message, hasMessage := defaultMessagesByKind[kind]; hasMessage {
			return apperror.New(kind, message)
		}
	}

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

	if statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError {
		return apperror.KindInvalidRequest
	}
	return apperror.KindInternal
}
