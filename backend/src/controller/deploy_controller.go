package controller

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/jst"
	"launchs/shared/model"
)

// DeployUseCase は、DeployController が呼ぶ、deploy のビジネスルール。
//
// 実体は service の DeployService。テストでは fake に差し替える。
type DeployUseCase interface {
	Create(requestContext context.Context, userID string, projectID string, imageRef string, specName string) (string, error)
	Get(requestContext context.Context, userID string, deployID string) (model.Deploy, error)
	UpdateConfig(requestContext context.Context, userID string, deployID string, env map[string]string, command *string, args []string) error
	DiscardConfig(requestContext context.Context, userID string, deployID string) error
	ApplyConfig(requestContext context.Context, userID string, deployID string) error
	Scale(requestContext context.Context, userID string, deployID string, replicas int) error
}

// ScaleDeployRequest は、スケーリングのリクエストのボディ。
type ScaleDeployRequest struct {
	// Replicas は、新しいレプリカ数。0〜10(0 は停止相当)。ポインタなのは、0 と未指定を区別するため。
	Replicas *int `json:"replicas" validate:"required,min=0,max=10"`
}

// ScaleDeployResponse は、スケーリングのレスポンスのボディ。
type ScaleDeployResponse struct {
	// DeployID は、スケーリングした deploy の ID。
	DeployID string `json:"deployId"`

	// Replicas は、新しいレプリカ数。
	Replicas int `json:"replicas"`
}

// UpdateDeployConfigRequest は、起動設定の更新のリクエストのボディ。3つとも、常に全件を送って置き換える。
type UpdateDeployConfigRequest struct {
	// Env は、環境変数。必須(空にするなら {})。100個まで、名前は128文字まで、値は4096文字まで。
	Env map[string]string `json:"env" validate:"required,max=100,dive,keys,envname,max=128,endkeys,max=4096"`

	// Command は、起動コマンド。null か省略ならイメージの既定。1024文字まで。
	Command *string `json:"command" validate:"omitempty,min=1,max=1024"`

	// Args は、起動引数。null か省略ならイメージの既定。100個まで、1つは4096文字まで。
	Args []string `json:"args" validate:"omitempty,max=100,dive,max=4096"`
}

// CreateDeployRequest は、deploy 作成のリクエストのボディ。
type CreateDeployRequest struct {
	// ProjectID は、deploy を作るプロジェクトの ID。
	ProjectID string `json:"projectId" validate:"required"`

	// ImageRef は、起動するイメージ。空白を含まず、512文字まで。
	ImageRef string `json:"imageRef" validate:"required,max=512,imageref"`

	// Spec は、スペックの名前。使える名前は、DB のマスターデータ(small、medium、large)で決まる。
	Spec string `json:"spec" validate:"required,max=32"`
}

// CreateDeployResponse は、deploy 作成のレスポンスのボディ。
type CreateDeployResponse struct {
	// DeployID は、作成した deploy の ID。
	DeployID string `json:"deployId"`
}

// PodResponse は、deploy の Pod 1つのレスポンス。
type PodResponse struct {
	// PodID は、Pod の ID。
	PodID string `json:"podId"`

	// Status は、Pod の状態(pending、running、terminating のどれか)。
	Status string `json:"status"`
}

// DeployConfigResponse は、deploy の起動設定のレスポンス(現在の値と、まだ適用していない値)。
type DeployConfigResponse struct {
	// Env は、現在コンテナに反映されている環境変数。
	Env json.RawMessage `json:"env"`

	// PendingEnv は、まだ適用していない環境変数。変更が無ければ null。
	PendingEnv json.RawMessage `json:"pending_env"`

	// Command は、現在の起動コマンド。null ならイメージの既定。
	Command *string `json:"command"`

	// PendingCommand は、まだ適用していない起動コマンド。
	PendingCommand *string `json:"pending_command"`

	// Args は、現在の起動引数。null ならイメージの既定。
	Args json.RawMessage `json:"args"`

	// PendingArgs は、まだ適用していない起動引数。
	PendingArgs json.RawMessage `json:"pending_args"`
}

// DeployVolumesResponse は、deploy のボリュームのマウント設定のレスポンス。
type DeployVolumesResponse struct {
	// Mounts は、現在のマウント。
	Mounts []any `json:"mounts"`

	// PendingMounts は、まだ適用していないマウント。変更が無ければ null。
	PendingMounts []any `json:"pending_mounts"`
}

// DeployResponse は、deploy 詳細のレスポンスのボディ。
type DeployResponse struct {
	// DeployID は、deploy の ID。
	DeployID string `json:"deployId"`

	// Status は、deploy の状態。
	Status string `json:"status"`

	// Spec は、スペックの名前。
	Spec string `json:"spec"`

	// Replicas は、希望するレプリカ数。
	Replicas int `json:"replicas"`

	// Pods は、現在稼働している Pod の一覧。いまは、k8s から読む仕組みが無いので、常に空。
	Pods []PodResponse `json:"pods"`

	// ImageRef は、起動しているイメージ。
	ImageRef string `json:"imageRef"`

	// CreatedAt は、作成日時(JST)。
	CreatedAt string `json:"createdAt"`

	// Config は、起動設定。
	Config DeployConfigResponse `json:"config"`

	// Volumes は、ボリュームのマウント設定。ボリュームの機能ができるまでは、空のマウントを返す。
	Volumes DeployVolumesResponse `json:"volumes"`

	// Service は、deploy に紐づく service。service の機能ができるまでは、常に null(未作成)。
	Service any `json:"service"`
}

// DeployController は、/deploy の HTTP の入出力を担当する。
type DeployController struct {
	deployUseCase DeployUseCase
}

// NewDeployController は、DeployController を作る。
//
// 引数:
//   - deployUseCase: deploy のビジネスルール。
//
// 戻り値:
//   - 作成した DeployController。
func NewDeployController(deployUseCase DeployUseCase) *DeployController {
	// ビジネスルールを持たせた DeployController を返す。
	return &DeployController{deployUseCase: deployUseCase}
}

// Create は、deploy を作成して、起動を開始する(POST /deploy)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - ボディが不正なら invalid_request、プロジェクトが無い・他人のものなら not_found、
//     プロジェクトが削除中なら conflict、未認証なら unauthorized。
func (deployController *DeployController) Create(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ボディを読み込み、validate タグで検証する。
	var request CreateDeployRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}

	// 作成を、ビジネスルールに任せる。
	deployID, createError := deployController.deployUseCase.Create(echoContext.Request().Context(), userID, request.ProjectID, request.ImageRef, request.Spec)
	if createError != nil {
		return createError
	}
	// 作成した deploy の ID を返す。
	return echoContext.JSON(http.StatusOK, CreateDeployResponse{DeployID: deployID})
}

// Get は、deploy の詳細を返す(GET /deploy/:deployId)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い、または他のユーザーのものなら not_found、未認証なら unauthorized。
func (deployController *DeployController) Get(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}

	// ビジネスルールに、deploy の取得を任せる。
	deploy, getError := deployController.deployUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("deployId"))
	if getError != nil {
		return getError
	}
	// モデルを、レスポンスの形にして返す。
	return echoContext.JSON(http.StatusOK, newDeployResponse(deploy))
}

// newDeployResponse は、モデルを、レスポンスの形にする。
//
// JSON の列(環境変数など)は、中身を解釈せず、そのまま出す。値が無い列は null になる。
//
// 引数:
//   - deploy: deploy のモデル。
//
// 戻り値:
//   - レスポンスのボディ。
func newDeployResponse(deploy model.Deploy) DeployResponse {
	return DeployResponse{
		DeployID: deploy.ID,
		Status:   deploy.Status,
		Spec:     deploy.Spec,
		Replicas: deploy.Replicas,
		// Pod の一覧は、k8s から読む仕組みができるまで、空の配列で返す(null にしない)。
		Pods:      []PodResponse{},
		ImageRef:  deploy.ImageRef,
		CreatedAt: jst.Format(deploy.CreatedAt),
		Config: DeployConfigResponse{
			Env:            jsonOrNull(deploy.Env),
			PendingEnv:     jsonOrNull(deploy.PendingEnv),
			Command:        deploy.Command,
			PendingCommand: deploy.PendingCommand,
			Args:           jsonOrNull(deploy.Args),
			PendingArgs:    jsonOrNull(deploy.PendingArgs),
		},
		Volumes: DeployVolumesResponse{Mounts: []any{}, PendingMounts: nil},
		Service: nil,
	}
}

// jsonOrNull は、JSON の列の値を、レスポンスにそのまま出せる形にする。値が無ければ JSON の null にする。
//
// 引数:
//   - column: JSON の列の値(NULL のときは長さ 0)。
//
// 戻り値:
//   - そのままの JSON。無ければ null。
func jsonOrNull(column []byte) json.RawMessage {
	// 値が無いとき(DB の NULL)は、JSON の null にする。
	if len(column) == 0 {
		return json.RawMessage("null")
	}
	// 値があるときは、そのまま出す。
	return json.RawMessage(column)
}

// GetConfig は、deploy の起動設定を返す(GET /deploy/:deployId/config)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い、または他のユーザーのものなら not_found、未認証なら unauthorized。
func (deployController *DeployController) GetConfig(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// deploy を取得する。
	deploy, getError := deployController.deployUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("deployId"))
	if getError != nil {
		return getError
	}
	// 詳細のうち、起動設定の部分だけを返す。
	return echoContext.JSON(http.StatusOK, newDeployResponse(deploy).Config)
}

// UpdateConfig は、起動設定を、まだ適用しない値として保存する(PUT /deploy/:deployId/config)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - ボディが不正なら invalid_request、無い・他人のものなら not_found、
//     反映中や削除の途中なら conflict、未認証なら unauthorized。
func (deployController *DeployController) UpdateConfig(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ボディを読み込み、validate タグで検証する。
	var request UpdateDeployConfigRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 保存を、ビジネスルールに任せる。
	updateError := deployController.deployUseCase.UpdateConfig(echoContext.Request().Context(), userID, echoContext.Param("deployId"), request.Env, request.Command, request.Args)
	if updateError != nil {
		return updateError
	}
	// 空のオブジェクトを返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// DiscardConfig は、まだ適用していない起動設定を破棄する(POST /deploy/:deployId/config/discard)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い・他人のものなら not_found、反映中・削除の途中・破棄する値が無いなら conflict、未認証なら unauthorized。
func (deployController *DeployController) DiscardConfig(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 破棄を、ビジネスルールに任せる。
	if discardError := deployController.deployUseCase.DiscardConfig(echoContext.Request().Context(), userID, echoContext.Param("deployId")); discardError != nil {
		return discardError
	}
	// 空のオブジェクトを返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// ApplyConfig は、まだ適用していない起動設定の反映を開始する(POST /deploy/:deployId/config/apply)。
//
// 反映の完了は待たず、すぐに応答する。結果は、deploy の status(applying → active か failed)で確かめる。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い・他人のものなら not_found、適用する値が無い・反映中・起動中・削除の途中なら conflict、未認証なら unauthorized。
func (deployController *DeployController) ApplyConfig(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 反映の開始を、ビジネスルールに任せる。
	if applyError := deployController.deployUseCase.ApplyConfig(echoContext.Request().Context(), userID, echoContext.Param("deployId")); applyError != nil {
		return applyError
	}
	// 空のオブジェクトを返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// Scale は、deploy のレプリカ数を変える(POST /deploy/:deployId/scale)。
//
// 反映の完了は待たず、すぐに応答する。結果は、deploy の status(applying → active か scaled_zero、失敗は failed)で確かめる。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - ボディが不正なら invalid_request、無い・他人のものなら not_found、
//     反映中・起動中・削除の途中なら conflict、未認証なら unauthorized。
func (deployController *DeployController) Scale(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ボディを読み込み、validate タグで検証する。
	var request ScaleDeployRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// スケーリングを、ビジネスルールに任せる。
	deployID := echoContext.Param("deployId")
	if scaleError := deployController.deployUseCase.Scale(echoContext.Request().Context(), userID, deployID, *request.Replicas); scaleError != nil {
		return scaleError
	}
	// deploy の ID と、新しい replicas を返す。
	return echoContext.JSON(http.StatusOK, ScaleDeployResponse{DeployID: deployID, Replicas: *request.Replicas})
}
