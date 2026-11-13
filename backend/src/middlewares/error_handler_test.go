package middlewares

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"launchs/shared/apperror"
	"launchs/shared/logger"
)

// newServerReturning は、指定したエラーを返すルート(GET /fail)を持つ Echo のサーバーを作る(テスト用)。
// エラーハンドラーは NewErrorHandler のものを使う。
//
// 引数:
//   - appLogger: エラーハンドラーが使うロガー。
//   - errorToReturn: ルートが返すエラー。
//
// 戻り値:
//   - 組み立てた Echo のサーバー。
func newServerReturning(appLogger *slog.Logger, errorToReturn error) *echo.Echo {
	// 検証の対象のエラーハンドラーを設定したサーバーを作る。
	echoServer := echo.NewWithConfig(echo.Config{HTTPErrorHandler: NewErrorHandler(appLogger)})
	// 指定されたエラーを必ず返すルートを登録する。
	echoServer.GET("/fail", func(echoContext *echo.Context) error {
		// 受け取ったエラーを、そのまま返す。
		return errorToReturn
	})
	// 組み立てたサーバーを返す。
	return echoServer
}

// sendRequest は、サーバーにリクエストを送り、レスポンスを返す(テスト用)。
//
// 引数:
//   - echoServer: リクエストを受けるサーバー。
//   - method: HTTP のメソッド。
//   - path: リクエストのパス。
//
// 戻り値:
//   - レスポンスを記録したもの。
func sendRequest(echoServer *echo.Echo, method string, path string) *httptest.ResponseRecorder {
	// 指定されたメソッドとパスで、本文の無いリクエストを作る。
	request := httptest.NewRequest(method, path, nil)
	// レスポンスを記録する入れ物を作る。
	recorder := httptest.NewRecorder()
	// ネットワークを使わずに、サーバーへリクエストを直接渡す。
	echoServer.ServeHTTP(recorder, request)
	// 記録されたレスポンスを返す。
	return recorder
}

// TestErrorHandlerConvertsErrors は、ハンドラーが返したエラーが、種類に応じたステータスと、
// 共通ルールの形式のボディに変換されることを確認する。
func TestErrorHandlerConvertsErrors(testingT *testing.T) {
	// ハンドラーが返すエラーの種類ごとに、期待するステータス、code、message を並べる。ケースの意図は name で示す。
	testCases := []struct {
		name            string
		errorToReturn   error
		expectedStatus  int
		expectedCode    string
		expectedMessage string
	}{
		{
			name:            "アプリケーションのエラーは、その種類とメッセージになる",
			errorToReturn:   apperror.New(apperror.KindConflict, "名前が重複している"),
			expectedStatus:  http.StatusConflict,
			expectedCode:    "conflict",
			expectedMessage: "名前が重複している",
		},
		{
			name:            "%w で包まれたアプリケーションのエラーも、その種類とメッセージになる",
			errorToReturn:   fmt.Errorf("保存で失敗: %w", apperror.New(apperror.KindNotFound, "プロジェクトが見つからない")),
			expectedStatus:  http.StatusNotFound,
			expectedCode:    "not_found",
			expectedMessage: "プロジェクトが見つからない",
		},
		{
			name:            "Echo の 404 は not_found になる",
			errorToReturn:   echo.NewHTTPError(http.StatusNotFound, "Not Found"),
			expectedStatus:  http.StatusNotFound,
			expectedCode:    "not_found",
			expectedMessage: defaultMessagesByKind[apperror.KindNotFound],
		},
		{
			name:            "Echo の 401 は unauthorized になる",
			errorToReturn:   echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized"),
			expectedStatus:  http.StatusUnauthorized,
			expectedCode:    "unauthorized",
			expectedMessage: defaultMessagesByKind[apperror.KindUnauthorized],
		},
		{
			name:            "Echo の 413 は payload_too_large になる",
			errorToReturn:   echo.NewHTTPError(http.StatusRequestEntityTooLarge, "Request Entity Too Large"),
			expectedStatus:  http.StatusRequestEntityTooLarge,
			expectedCode:    "payload_too_large",
			expectedMessage: defaultMessagesByKind[apperror.KindPayloadTooLarge],
		},
		{
			name:            "共通ルールに無い 4xx(405)は invalid_request(400)になる(仮)",
			errorToReturn:   echo.NewHTTPError(http.StatusMethodNotAllowed, "Method Not Allowed"),
			expectedStatus:  http.StatusBadRequest,
			expectedCode:    "invalid_request",
			expectedMessage: defaultMessagesByKind[apperror.KindInvalidRequest],
		},
		{
			name:            "通常のエラーは、内部エラー(500)で一般的なメッセージになる",
			errorToReturn:   errors.New("pq: connection refused"),
			expectedStatus:  http.StatusInternalServerError,
			expectedCode:    "internal_error",
			expectedMessage: "サーバー内部でエラーが発生しました",
		},
	}

	// ケースごとに、サブテストを走らせる。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// ログの出力を受け取る入れ物を用意する。
			var logOutput bytes.Buffer
			// ケースのエラーを返すルートを持つサーバーを、入れ物に書くロガーで組み立てる。
			echoServer := newServerReturning(logger.New(&logOutput, "backend", slog.LevelInfo), testCase.errorToReturn)

			// エラーを返すルートにリクエストを送る。
			recorder := sendRequest(echoServer, http.MethodGet, "/fail")

			// ステータスコードが期待と違うときは失敗させる。
			if recorder.Code != testCase.expectedStatus {
				subTest.Errorf("ステータスコードが違う: 期待 %d、実際 %d", testCase.expectedStatus, recorder.Code)
			}
			// ボディを読み込む先の変数を用意する。
			var actualResponse apperror.ErrorResponse
			// ボディが共通ルールの形式の JSON として読めなければ、以降の比較ができないので、すぐに失敗させる。
			if unmarshalError := json.Unmarshal(recorder.Body.Bytes(), &actualResponse); unmarshalError != nil {
				subTest.Fatalf("ボディが JSON ではない: %v\nボディ: %s", unmarshalError, recorder.Body.String())
			}
			// code が期待と違うときは失敗させる。
			if actualResponse.Error.Code != testCase.expectedCode {
				subTest.Errorf("code が違う: 期待 %q、実際 %q", testCase.expectedCode, actualResponse.Error.Code)
			}
			// message が期待と違うときは失敗させる。
			if actualResponse.Error.Message != testCase.expectedMessage {
				subTest.Errorf("message が違う: 期待 %q、実際 %q", testCase.expectedMessage, actualResponse.Error.Message)
			}
		})
	}
}

// TestErrorHandlerLogsInternalErrorWithoutLeaking は、500 になるエラーの原因が、
// ログには出て、レスポンスには出ないことを確認する。
func TestErrorHandlerLogsInternalErrorWithoutLeaking(testingT *testing.T) {
	// ログの出力を受け取る入れ物を用意する。
	var logOutput bytes.Buffer
	// 内部の情報(IP アドレスとポート)を含む、利用者に見せてはいけない原因のエラーを作る。
	secretCause := errors.New("dial tcp 10.0.0.5:5432: connection refused")
	// 原因のエラーを返すルートを持つサーバーを組み立てる。
	echoServer := newServerReturning(logger.New(&logOutput, "backend", slog.LevelInfo), secretCause)

	// エラーを返すルートにリクエストを送る。
	recorder := sendRequest(echoServer, http.MethodGet, "/fail")

	// レスポンスに内部の情報が出ていたら、漏れているので失敗させる。
	if strings.Contains(recorder.Body.String(), "10.0.0.5") {
		testingT.Errorf("レスポンスに内部の情報が含まれている: %s", recorder.Body.String())
	}
	// ログに原因が出ていなかったら、調査できないので失敗させる。
	if !strings.Contains(logOutput.String(), "10.0.0.5") {
		testingT.Errorf("ログに原因が出ていない: %s", logOutput.String())
	}
}

// TestErrorHandlerDoesNotLogClientErrors は、404 などのクライアントのエラーは、
// エラーのログを出さないことを確認する(利用者の操作のたびにエラーログが増えないようにするため)。
func TestErrorHandlerDoesNotLogClientErrors(testingT *testing.T) {
	// ログの出力を受け取る入れ物を用意する。
	var logOutput bytes.Buffer
	// 404 になるエラーを返すルートを持つサーバーを組み立てる。
	echoServer := newServerReturning(logger.New(&logOutput, "backend", slog.LevelInfo), apperror.New(apperror.KindNotFound, "無い"))

	// エラーを返すルートにリクエストを送る。レスポンスは使わないので捨てる。
	sendRequest(echoServer, http.MethodGet, "/fail")

	// 何かログが出ていたら、クライアントのエラーを記録してしまっているので失敗させる。
	if logOutput.Len() != 0 {
		testingT.Errorf("クライアントのエラーなのに、ログが出ている: %s", logOutput.String())
	}
}
