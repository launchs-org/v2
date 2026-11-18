package httpmiddleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"launchs/shared/apperror"
	"launchs/shared/logger"
)

// newServerWithAccessLog は、リクエスト ID とアクセスログのミドルウェアを持ち、
// 成功するルート(GET /ok)と、404 になるルート(GET /missing)を持つサーバーを作る(テスト用)。
//
// 引数:
//   - appLogger: アクセスログとエラーハンドラーが使うロガー。
//
// 戻り値:
//   - 組み立てた Echo のサーバー。
func newServerWithAccessLog(appLogger *slog.Logger) *echo.Echo {
	// エラーを共通ルールの形式に変換するエラーハンドラーを設定したサーバーを作る。
	echoServer := echo.NewWithConfig(echo.Config{HTTPErrorHandler: NewErrorHandler(appLogger)})
	// リクエスト ID を付けるミドルウェアを、アクセスログより先(外側)に登録する。ログに ID を出すため。
	echoServer.Use(middleware.RequestID())
	// 検証の対象のアクセスログのミドルウェアを登録する。
	echoServer.Use(NewAccessLogMiddleware(appLogger))
	// 成功(200)するルートを登録する。
	echoServer.GET("/ok", func(echoContext *echo.Context) error {
		// 本文 ok の 200 を返す。
		return echoContext.String(http.StatusOK, "ok")
	})
	// not_found のエラーを返す(404 になる)ルートを登録する。
	echoServer.GET("/missing", func(echoContext *echo.Context) error {
		// エラーを返し、エラーハンドラーが 404 に変換するようにする。
		return apperror.New(apperror.KindNotFound, "無い")
	})
	// 組み立てたサーバーを返す。
	return echoServer
}

// parseSingleLogLine は、ログの出力が、JSON の1行だけであることを確認して、その中身を返す(テスト用)。
//
// 引数:
//   - testingT: テストの実行を表す値。
//   - logOutput: ロガーの出力。
//
// 戻り値:
//   - 1行の JSON を読み込んだ中身。
func parseSingleLogLine(testingT *testing.T, logOutput *bytes.Buffer) map[string]any {
	// 失敗したときに、この関数ではなく、呼び出した側の行を示すようにする。
	testingT.Helper()

	// 出力を、前後の空白を除いてから、行ごとに分ける。
	logLines := strings.Split(strings.TrimSpace(logOutput.String()), "\n")
	// 1リクエストにつき1行のはずなので、行数が違うときは失敗させる。
	if len(logLines) != 1 {
		testingT.Fatalf("ログは1リクエストにつき1行のはず: %d 行\n%s", len(logLines), logOutput.String())
	}

	// JSON を読み込む先の変数を用意する。
	var logLine map[string]any
	// 1行を JSON として読み込み、読めなかったときは失敗させる。
	if unmarshalError := json.Unmarshal([]byte(logLines[0]), &logLine); unmarshalError != nil {
		testingT.Fatalf("ログが JSON ではない: %v\n%s", unmarshalError, logLines[0])
	}
	// 読み込んだ中身を返す。
	return logLine
}

// TestAccessLogMiddlewareLogsOneLinePerRequest は、リクエストごとに1行のログが出て、
// リクエスト ID、メソッド、パス、ステータス、処理時間が含まれることを確認する。
func TestAccessLogMiddlewareLogsOneLinePerRequest(testingT *testing.T) {
	// 成功とエラーのそれぞれのパスと、期待するステータスを並べる。
	testCases := []struct {
		name           string
		path           string
		expectedStatus int
	}{
		{"成功したリクエスト", "/ok", http.StatusOK},
		{"エラーになったリクエストも、最終的なステータスを出す", "/missing", http.StatusNotFound},
	}

	// ケースごとに、サブテストを走らせる。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// ログの出力を受け取る入れ物を用意する。
			var logOutput bytes.Buffer
			// 入れ物に JSON で書き出すロガーで、サーバーを組み立てる。
			echoServer := newServerWithAccessLog(logger.New(&logOutput, "backend", slog.LevelInfo))

			// ケースのパスへの GET リクエストを作る。
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			// リクエスト ID を指定し、ログに同じ値が出ることを確認できるようにする。
			request.Header.Set(RequestIDHeaderName, "request-abc")
			// ネットワークを使わずに、サーバーへリクエストを直接渡す。レスポンスは使わないので捨てる。
			echoServer.ServeHTTP(httptest.NewRecorder(), request)

			// ログが1行であることを確認して、その中身を取り出す。
			logLine := parseSingleLogLine(subTest, &logOutput)
			// 各項目が期待どおりでなければ、失敗させる。リクエスト ID は、送った値と同じはず。
			if logLine["request_id"] != "request-abc" {
				subTest.Errorf("request_id が違う: %v", logLine["request_id"])
			}
			if logLine["method"] != http.MethodGet {
				subTest.Errorf("method が違う: %v", logLine["method"])
			}
			if logLine["path"] != testCase.path {
				subTest.Errorf("path が違う: 期待 %q、実際 %v", testCase.path, logLine["path"])
			}
			if logLine["status"] != float64(testCase.expectedStatus) {
				subTest.Errorf("status が違う: 期待 %d、実際 %v", testCase.expectedStatus, logLine["status"])
			}
			// 処理時間は、値が毎回変わるので、項目があることだけを確認する。
			if _, hasLatency := logLine["latency_ms"]; !hasLatency {
				subTest.Errorf("latency_ms が無い: %v", logLine)
			}
		})
	}
}
