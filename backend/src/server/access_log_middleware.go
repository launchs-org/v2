package server

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
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogLatency:   true,
		LogMethod:    true,
		LogURIPath:   true,
		LogStatus:    true,
		LogRequestID: true,
		HandleError:  true,
		LogValuesFunc: func(echoContext *echo.Context, values middleware.RequestLoggerValues) error {
			appLogger.Info("リクエストを処理した",
				"request_id", values.RequestID,
				"method", values.Method,
				"path", values.URIPath,
				"status", values.Status,
				"latency_ms", values.Latency.Milliseconds(),
			)
			return nil
		},
	})
}
