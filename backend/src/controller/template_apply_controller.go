package controller

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/jst"
	"launchs/shared/model"
	"launchs/shared/templateplan"
)

// TemplateApplyUseCase は、TemplateApplyController が使う、テンプレートの実行のユースケース。
type TemplateApplyUseCase interface {
	Apply(requestContext context.Context, userID string, projectID string, templateID string, input map[string]string) (string, map[string]string, error)
	Get(requestContext context.Context, userID string, projectID string, applyID string) (model.TemplateApply, []templateplan.StepState, error)
}

// ApplyTemplateRequest は、テンプレートから作るリクエスト。
type ApplyTemplateRequest struct {
	// Values は、変数の値(変数名 → 文字列)。省略した変数は、既定値か、自動生成になる。
	Values map[string]string `json:"values" validate:"max=50"`
}

// ApplyTemplateResponse は、テンプレートから作る受け付けのレスポンス。
type ApplyTemplateResponse struct {
	// ApplyID は、実行の ID。進み具合を取るときに使う。
	ApplyID string `json:"applyId"`

	// Values は、全ての変数の値(自動生成を含む。画面が、パスワードを見せるため)。
	Values map[string]string `json:"values"`
}

// TemplateStepResponse は、ステップ 1 つの進み具合。
type TemplateStepResponse struct {
	// Kind は、volume、configmap、deploy、service、ingress のどれか。
	Kind string `json:"kind"`

	// Name は、テンプレートの中の部品の name。
	Name string `json:"name"`

	// Status は、waiting、creating、done、failed のどれか。
	Status string `json:"status"`

	// ResourceID は、作った部品の ID(作り始めたら入る。まだなら空)。
	ResourceID string `json:"resourceId"`

	// Error は、失敗の理由(失敗したときだけ。それ以外は null)。
	Error *string `json:"error"`
}

// TemplateApplyResponse は、テンプレートの実行の進み具合のレスポンス。
type TemplateApplyResponse struct {
	// ApplyID は、実行の ID。
	ApplyID string `json:"applyId"`

	// TemplateID は、使ったテンプレートの id。
	TemplateID string `json:"templateId"`

	// Status は、running、succeeded、failed のどれか。
	Status string `json:"status"`

	// Values は、全ての変数の値。
	Values map[string]string `json:"values"`

	// Steps は、ステップごとの進み具合(作る順)。
	Steps []TemplateStepResponse `json:"steps"`

	// Error は、失敗の理由(失敗したときだけ。それ以外は null)。
	Error *string `json:"error"`

	// CreatedAt と UpdatedAt は、日時(JST)。
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// TemplateApplyController は、テンプレートから作る API のハンドラー。
type TemplateApplyController struct {
	templateApplyUseCase TemplateApplyUseCase
}

// NewTemplateApplyController は、TemplateApplyController を作る。
//
// 引数:
//   - templateApplyUseCase: テンプレートの実行のユースケース。
//
// 戻り値:
//   - 作成した TemplateApplyController。
func NewTemplateApplyController(templateApplyUseCase TemplateApplyUseCase) *TemplateApplyController {
	// ユースケースを持たせて返す。
	return &TemplateApplyController{templateApplyUseCase: templateApplyUseCase}
}

// Apply は、POST /projects/:projectId/templates/:templateId/apply のハンドラー。受け付けて、すぐ応答する(作成は、非同期)。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 実行の ID と、全ての変数の値(200)。値が不正なら 400、プロジェクトかテンプレートが無ければ 404、
//     プロジェクトが active でない、クォータを超えるなら 409。
func (templateApplyController *TemplateApplyController) Apply(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// リクエストの形を確かめる(本文が無くても、よい。全て既定になる)。
	var request ApplyTemplateRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 受け付ける。
	applyID, values, applyError := templateApplyController.templateApplyUseCase.Apply(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("templateId"), request.Values)
	if applyError != nil {
		return applyError
	}
	return echoContext.JSON(http.StatusOK, ApplyTemplateResponse{ApplyID: applyID, Values: values})
}

// Get は、GET /projects/:projectId/template-applies/:applyId のハンドラー。進み具合を返す。
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - 進み具合(200)。無ければ(他人のものを含む)404。
func (templateApplyController *TemplateApplyController) Get(echoContext *echo.Context) error {
	// 認証されたユーザーを取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 取得する。
	apply, states, getError := templateApplyController.templateApplyUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("projectId"), echoContext.Param("applyId"))
	if getError != nil {
		return getError
	}
	// レスポンスにする(失敗の理由は、空なら null)。
	steps := make([]TemplateStepResponse, 0, len(states))
	for _, state := range states {
		step := TemplateStepResponse{Kind: state.Kind, Name: state.Name, Status: state.Status, ResourceID: state.ResourceID}
		if state.Error != "" {
			message := state.Error
			step.Error = &message
		}
		steps = append(steps, step)
	}
	return echoContext.JSON(http.StatusOK, TemplateApplyResponse{
		ApplyID: apply.ID, TemplateID: apply.TemplateID, Status: apply.Status, Values: valuesFromJSON(apply.Values), Steps: steps, Error: apply.Error,
		CreatedAt: jst.Format(apply.CreatedAt), UpdatedAt: jst.Format(apply.UpdatedAt),
	})
}

// valuesFromJSON は、保存した変数の値(JSON のオブジェクト)を、読み込む。読めなければ、空。
//
// 引数:
//   - raw: JSON のオブジェクト。
//
// 戻り値:
//   - 変数名 → 値。
func valuesFromJSON(raw []byte) map[string]string {
	// 読み込む。読めなければ、空として扱う。
	values := map[string]string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &values)
	}
	return values
}
