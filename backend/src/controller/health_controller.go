// Package controller は、バックエンドの HTTP の入出力を担当する。
//
// リクエストの受け取りと検証、レスポンスの組み立てだけを書き、ビジネスルールは service に任せる。
package controller

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// HealthCheckResponse は、ヘルスチェックのレスポンスのボディ。
type HealthCheckResponse struct {
	// Status は、サーバーの状態。動いていれば "ok"。
	Status string `json:"status"`
}

// HealthController は、サーバーが動いているかを返すエンドポイントを担当する。
//
// k8s の死活監視(liveness / readiness probe)などの、運用のために使う。
type HealthController struct{}

// NewHealthController は、HealthController を作る。
//
// 戻り値:
//   - 作成した HealthController。
func NewHealthController() *HealthController {
	return &HealthController{}
}

// Check は、サーバーが動いていることを返す。
//
// 認証は不要で、データベースなどの外部には問い合わせない(サーバーのプロセスが
// リクエストを受けられるかだけを返す)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - レスポンスを書き込むときのエラー。
func (healthController *HealthController) Check(echoContext *echo.Context) error {
	return echoContext.JSON(http.StatusOK, HealthCheckResponse{Status: "ok"})
}
