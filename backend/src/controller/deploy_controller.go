package controller

import (
	"context"
	"launchs/backend/src/middlewares"
	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
	"net/http"

	"github.com/labstack/echo/v5"
)

// DeployUseCase は、DeployController が呼ぶ、deploy のビジネスルール。
//
// 実体は service の DeployService。テストでは fake に差し替える。
type DeployUseCase interface {
	Create(requestContext context.Context, userID string, projectID string, imageRef string, specName string) (string, error)
	Get(requestContext context.Context, userID string, deployID string) (model.Deploy, error)
	List(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.Deploy, *pagination.Position, error)
	ListPods(requestContext context.Context, userID string, deployID string) ([]model.DeployPod, error)
	UpdateConfig(requestContext context.Context, userID string, deployID string, env map[string]string, command *string, args []string) error
	DiscardConfig(requestContext context.Context, userID string, deployID string) error
	ApplyConfig(requestContext context.Context, userID string, deployID string) error
	UpdateMounts(requestContext context.Context, userID string, deployID string, mounts []model.MountSpec) error
	DiscardMounts(requestContext context.Context, userID string, deployID string) error
	ApplyMounts(requestContext context.Context, userID string, deployID string) error
	UpdateFiles(requestContext context.Context, userID string, deployID string, files []model.FileSpec) error
	DiscardFiles(requestContext context.Context, userID string, deployID string) error
	ApplyFiles(requestContext context.Context, userID string, deployID string) error
	Scale(requestContext context.Context, userID string, deployID string, replicas int) error
	Delete(requestContext context.Context, userID string, deployID string) error
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

// DeployController は、/deploy の HTTP の入出力を担当する。
type DeployController struct {
	deployUseCase    DeployUseCase
	deployLogUseCase DeployLogUseCase
	serviceUseCase   ServiceUseCase
}

// NewDeployController は、DeployController を作る。
//
// 引数:
//   - deployUseCase: deploy のビジネスルール。
//   - deployLogUseCase: ログの取得のビジネスルール。
//   - serviceUseCase: service のビジネスルール(deploy の詳細に、service を含めるため)。
//
// 戻り値:
//   - 作成した DeployController。
func NewDeployController(deployUseCase DeployUseCase, deployLogUseCase DeployLogUseCase, serviceUseCase ServiceUseCase) *DeployController {
	// ビジネスルールを持たせた DeployController を返す。
	return &DeployController{deployUseCase: deployUseCase, deployLogUseCase: deployLogUseCase, serviceUseCase: serviceUseCase}
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
	// Pod の一覧を読む(watcher が DB に書いた観測値)。
	pods, podsError := deployController.deployUseCase.ListPods(echoContext.Request().Context(), userID, deploy.ID)
	if podsError != nil {
		return podsError
	}
	// モデルを、レスポンスの形にして返す。
	response := newDeployResponse(deploy)
	response.Pods = newPodResponses(pods)
	// ログのある Pod の一覧を読む(logcollector が DB に書いたストリーム)。
	logStreams, logPodsError := deployController.deployLogUseCase.ListLogPods(echoContext.Request().Context(), userID, deploy.ID)
	if logPodsError != nil {
		return logPodsError
	}
	response.LogPods = newLogPodResponses(logStreams)
	// service を読む。まだ作っていない(not_found)なら、null のままにする。他のエラーは、そのまま返す。
	deployService, serviceError := deployController.serviceUseCase.Get(echoContext.Request().Context(), userID, deploy.ID)
	if serviceError != nil && apperror.KindOf(serviceError) != apperror.KindNotFound {
		return serviceError
	}
	if serviceError == nil {
		serviceResponse := newServiceResponse(deployService)
		response.Service = &serviceResponse
	}
	return echoContext.JSON(http.StatusOK, response)
}
