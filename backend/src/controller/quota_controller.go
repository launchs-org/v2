package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/backend/src/service"
)

// QuotaUseCase は、QuotaController が呼ぶ、上限と使用量の取得。
//
// 実体は service の QuotaService。テストでは fake に差し替える。
type QuotaUseCase interface {
	Get(requestContext context.Context, userID string) (service.QuotaReport, error)
}

// QuotaItem は、上限のある項目1つの、上限と使用量。
type QuotaItem struct {
	// Limit は、上限。
	Limit int `json:"limit"`

	// Used は、いまの使用量。
	Used int `json:"used"`
}

// QuotaResponse は、自分の上限と使用量のレスポンスのボディ。
type QuotaResponse struct {
	// Projects は、プロジェクトの数。
	Projects QuotaItem `json:"projects"`

	// Deploys は、deploy の数。
	Deploys QuotaItem `json:"deploys"`

	// CPUMillicores は、deploy の CPU の合計(ミリコア)。
	CPUMillicores QuotaItem `json:"cpuMillicores"`

	// MemoryMiB は、deploy のメモリの合計(MiB)。
	MemoryMiB QuotaItem `json:"memoryMiB"`

	// Volumes は、volume の数。
	Volumes QuotaItem `json:"volumes"`

	// VolumeTotalMB は、volume の大きさの合計(MB)。
	VolumeTotalMB QuotaItem `json:"volumeTotalMB"`

	// MaxVolumeSizeMB は、volume 1つの大きさの上限(MB)。
	MaxVolumeSizeMB int `json:"maxVolumeSizeMB"`
}

// QuotaController は、/quota の HTTP の入出力を担当する。
type QuotaController struct {
	quotaUseCase QuotaUseCase
}

// NewQuotaController は、QuotaController を作る。
//
// 引数:
//   - quotaUseCase: 上限と使用量の取得。
//
// 戻り値:
//   - 作成した QuotaController。
func NewQuotaController(quotaUseCase QuotaUseCase) *QuotaController {
	// 依存するものを持たせて返す。
	return &QuotaController{quotaUseCase: quotaUseCase}
}

// Get は、GET /quota のハンドラー。自分の上限と使用量を返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 上限と使用量のレスポンス(200)。
func (quotaController *QuotaController) Get(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 上限と使用量を取得して、レスポンスにする。
	report, getError := quotaController.quotaUseCase.Get(echoContext.Request().Context(), userID)
	if getError != nil {
		return getError
	}
	return echoContext.JSON(http.StatusOK, QuotaResponse{
		Projects:        QuotaItem{Limit: report.Limits.MaxProjects, Used: report.Usage.Projects},
		Deploys:         QuotaItem{Limit: report.Limits.MaxDeploys, Used: report.Usage.Deploys},
		CPUMillicores:   QuotaItem{Limit: report.Limits.MaxCPUMillicores, Used: report.Usage.CPUMillicores},
		MemoryMiB:       QuotaItem{Limit: report.Limits.MaxMemoryMiB, Used: report.Usage.MemoryMiB},
		Volumes:         QuotaItem{Limit: report.Limits.MaxVolumes, Used: report.Usage.Volumes},
		VolumeTotalMB:   QuotaItem{Limit: report.Limits.MaxVolumeTotalMB, Used: report.Usage.VolumeTotalMB},
		MaxVolumeSizeMB: report.MaxVolumeSizeMB,
	})
}
