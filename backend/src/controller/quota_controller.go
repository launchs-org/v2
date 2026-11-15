package controller

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/backend/src/service"
	"launchs/shared/apperror"
	"launchs/shared/model"
)

// QuotaUseCase は、QuotaController が呼ぶ、上限と使用量の取得。
//
// 実体は service の QuotaService。テストでは fake に差し替える。
type QuotaUseCase interface {
	Get(requestContext context.Context, userID string) (service.QuotaReport, error)
	GetForUser(requestContext context.Context, targetUserID string) (service.AdminQuotaReport, error)
	SetOverrides(requestContext context.Context, targetUserID string, overrides model.QuotaOverrides) (service.AdminQuotaReport, error)
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

	// DeploysBySpec は、スペックの名前ごとの、deploy の数。
	DeploysBySpec map[string]QuotaItem `json:"deploysBySpec"`

	// MaxReplicas は、1つの deploy のレプリカ数(スケール)の最大。クォータとは別で、全体で同じ。
	MaxReplicas int `json:"maxReplicas"`

	// Volumes は、volume の数。
	Volumes QuotaItem `json:"volumes"`

	// VolumeTotalMB は、volume の大きさの合計(MB)。
	VolumeTotalMB QuotaItem `json:"volumeTotalMB"`

	// MaxVolumeSizeMB は、volume 1つの大きさの上限(MB)。
	MaxVolumeSizeMB int `json:"maxVolumeSizeMB"`
}

// QuotaOverridesBody は、ユーザーごとの上限の上書き(管理者が設定する)。リクエストにも、レスポンスにも使う。
//
// null(または省略)の項目は、既定を使う。PUT は、一式の置き換えなので、省略した項目は、既定に戻る。
type QuotaOverridesBody struct {
	// Projects は、プロジェクトの数の上限。
	Projects *int `json:"projects"`

	// Deploys は、deploy の合計の数の上限。
	Deploys *int `json:"deploys"`

	// Volumes は、volume の数の上限。
	Volumes *int `json:"volumes"`

	// VolumeTotalMB は、volume の大きさの合計の上限(MB)。
	VolumeTotalMB *int `json:"volumeTotalMB"`

	// DeploysBySpec は、スペックの名前ごとの、deploy の数の上限。キーが無いスペックは、スペックの既定。
	DeploysBySpec map[string]int `json:"deploysBySpec"`
}

// AdminQuotaResponse は、管理者が見る、あるユーザーの上限・使用量と、設定されている上書き。
type AdminQuotaResponse struct {
	QuotaResponse

	// Overrides は、管理者が設定した上書き。
	Overrides QuotaOverridesBody `json:"overrides"`
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

// newQuotaResponse は、上限と使用量を、レスポンスにする。
//
// 引数:
//   - report: 上限と使用量。
//
// 戻り値:
//   - レスポンス。
func newQuotaResponse(report service.QuotaReport) QuotaResponse {
	// スペックごとの上限と使用量を作る。
	deploysBySpec := make(map[string]QuotaItem, len(report.Limits.MaxDeploysBySpec))
	for specName, limit := range report.Limits.MaxDeploysBySpec {
		deploysBySpec[specName] = QuotaItem{Limit: limit, Used: report.Usage.DeploysBySpec[specName]}
	}
	return QuotaResponse{
		Projects:        QuotaItem{Limit: report.Limits.MaxProjects, Used: report.Usage.Projects},
		Deploys:         QuotaItem{Limit: report.Limits.MaxDeploys, Used: report.Usage.Deploys},
		DeploysBySpec:   deploysBySpec,
		MaxReplicas:     model.MaxReplicas,
		Volumes:         QuotaItem{Limit: report.Limits.MaxVolumes, Used: report.Usage.Volumes},
		VolumeTotalMB:   QuotaItem{Limit: report.Limits.MaxVolumeTotalMB, Used: report.Usage.VolumeTotalMB},
		MaxVolumeSizeMB: report.MaxVolumeSizeMB,
	}
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
	return echoContext.JSON(http.StatusOK, newQuotaResponse(report))
}

// newAdminQuotaResponse は、管理者が見る、上限・使用量・上書きを、レスポンスにする。
//
// 引数:
//   - adminReport: 上限・使用量・上書き。
//
// 戻り値:
//   - レスポンス。
func newAdminQuotaResponse(adminReport service.AdminQuotaReport) AdminQuotaResponse {
	// 上書きを、レスポンスの形に写す。
	overrides := adminReport.Overrides
	return AdminQuotaResponse{
		QuotaResponse: newQuotaResponse(adminReport.Report),
		Overrides: QuotaOverridesBody{
			Projects: overrides.MaxProjects, Deploys: overrides.MaxDeploys, Volumes: overrides.MaxVolumes, VolumeTotalMB: overrides.MaxVolumeTotalMB,
			DeploysBySpec: overrides.MaxDeploysBySpec,
		},
	}
}

// GetForUser は、GET /admin/users/:userId/quota のハンドラー。指定したユーザーの上限と使用量と、上書きを返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 上限・使用量・上書き(200)。管理者でなければ 403(ミドルウェア)、userId の形が不正なら 400。
func (quotaController *QuotaController) GetForUser(echoContext *echo.Context) error {
	// 対象のユーザーの情報を取得して、レスポンスにする。
	adminReport, getError := quotaController.quotaUseCase.GetForUser(echoContext.Request().Context(), echoContext.Param("userId"))
	if getError != nil {
		return getError
	}
	return echoContext.JSON(http.StatusOK, newAdminQuotaResponse(adminReport))
}

// PutForUser は、PUT /admin/users/:userId/quota のハンドラー。指定したユーザーの上限の上書きを、一式で置き換える。
//
// ボディに無い項目は、既定に戻る(一式の置き換え)。知らない項目名は、書き間違いで既定に戻ってしまうのを防ぐため、400 にする。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 置き換えた後の、上限・使用量・上書き(200)。管理者でなければ 403(ミドルウェア)、値や項目名が不正なら 400。
func (quotaController *QuotaController) PutForUser(echoContext *echo.Context) error {
	// 操作する管理者を取り出す(記録のため)。
	actorID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ボディを、知らない項目名を拒否して読む。
	var body QuotaOverridesBody
	decoder := json.NewDecoder(echoContext.Request().Body)
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(&body); decodeError != nil {
		return apperror.Wrap(apperror.KindInvalidRequest, "リクエストのボディを読み取れませんでした", decodeError)
	}
	// 置き換える。
	targetUserID := echoContext.Param("userId")
	overrides := model.QuotaOverrides{MaxProjects: body.Projects, MaxDeploys: body.Deploys, MaxVolumes: body.Volumes, MaxVolumeTotalMB: body.VolumeTotalMB, MaxDeploysBySpec: body.DeploysBySpec}
	adminReport, setError := quotaController.quotaUseCase.SetOverrides(echoContext.Request().Context(), targetUserID, overrides)
	if setError != nil {
		return setError
	}
	// 管理操作を、ログに残す(誰が、誰の上限を、どう変えたか)。
	echoContext.Logger().Info("管理操作", "actor", actorID, "action", "quota_set", "target", targetUserID, "overrides", body)
	return echoContext.JSON(http.StatusOK, newAdminQuotaResponse(adminReport))
}
