package httpmiddleware

import (
	"log/slog"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

// NewAccessLogMiddleware は、1リクエストにつき1行のアクセスログを出すミドルウェアを作る。
//
// ログには、リクエスト ID、メソッド、パス、ステータス、処理にかかった時間(ミリ秒)を出す。
// リクエスト ID を出すため、リクエスト ID のミドルウェアより内側(あとに)登録すること。
//
// エラーのときも、エラーハンドラーが決めた最終的なステータスをログに出すよう、
// このミドルウェアの中でエラーハンドラーを呼ぶ。
//
// 引数:
//   - appLogger: アクセスログを出すロガー。
//
// 戻り値:
//   - Echo に登録するミドルウェア。
func NewAccessLogMiddleware(appLogger *slog.Logger) echo.MiddlewareFunc {
	// Echo のリクエストロガーを、ログに出したい項目を選んで、設定して返す。
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		// ログに出す項目(処理時間、メソッド、パス、ステータス、リクエスト ID)を、取得する対象に選ぶ。
		LogLatency:   true,
		LogMethod:    true,
		LogURIPath:   true,
		LogStatus:    true,
		LogRequestID: true,
		// エラーのときも、エラーハンドラーを先に呼び、最終的なステータスをログに出す。
		HandleError: true,
		// 1リクエストが終わるたびに呼ばれ、集めた値を1行のログにする関数を渡す。
		LogValuesFunc: func(echoContext *echo.Context, values middleware.RequestLoggerValues) error {
			// 集めた値を、項目名を付けて、info のログとして1行で出す。処理時間は、読みやすいようにミリ秒にする。
			appLogger.Info("リクエストを処理した",
				"request_id", values.RequestID,
				"method", values.Method,
				"path", values.URIPath,
				"status", values.Status,
				"latency_ms", values.Latency.Milliseconds(),
			)
			// ログを出す処理は失敗しないので、エラーは返さない(返すとリクエストの処理に影響する)。
			return nil
		},
	})
}
