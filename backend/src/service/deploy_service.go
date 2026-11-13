package service

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// DeployStore は、DeployService が必要とする、deploy の保存先の操作。
//
// 実体は shared/repository の DeployRepository。テストでは fake に差し替える。
type DeployStore interface {
	FindSpec(requestContext context.Context, specName string) (model.DeploySpec, error)
	Create(requestContext context.Context, deploy *model.Deploy) error
	FindByID(requestContext context.Context, userID string, deployID string) (model.Deploy, error)
	DeleteUnstarted(requestContext context.Context, deployID string) error
}

// DeployWorkflowStarter は、deploy の起動(k8s への作成)を行う処理(Workflow)を開始する。
//
// 実体は TemporalDeployWorkflowStarter。テストでは fake に差し替える。
type DeployWorkflowStarter interface {
	// StartDeployCreation は、deploy の起動の処理を開始する。完了は待たない。
	StartDeployCreation(requestContext context.Context, deployID string) error
}

// DeployService は、deploy に関する操作のビジネスルールを担当する。
type DeployService struct {
	deployStore           DeployStore
	deployWorkflowStarter DeployWorkflowStarter
}

// NewDeployService は、DeployService を作る。
//
// 引数:
//   - deployStore: deploy の保存先。
//   - deployWorkflowStarter: deploy の起動の処理を開始するもの。
//
// 戻り値:
//   - 作成した DeployService。
func NewDeployService(deployStore DeployStore, deployWorkflowStarter DeployWorkflowStarter) *DeployService {
	// 保存先と、起動の開始を持たせた DeployService を返す。
	return &DeployService{deployStore: deployStore, deployWorkflowStarter: deployWorkflowStarter}
}

// Create は、プロジェクトの配下に deploy を作り、起動の処理を開始する。
//
// 処理の流れ:
//  1. スペックの名前が、マスターデータにあるかを確かめる(無ければ invalid_request)
//  2. deploy を pending 状態で保存する(プロジェクトが無い・他人のものなら not_found、削除中なら conflict)
//  3. 起動の Workflow を開始する(完了は待たない。起動が確認できると、status が active になる)
//  4. 手順 3 に失敗したら、手順 2 の保存を取り消す(起動されない deploy を残さない)
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: deploy を作るプロジェクトの ID。
//   - imageRef: 起動するイメージ。
//   - specName: スペックの名前(small、medium、large)。
//
// 戻り値:
//   - 作成した deploy の ID。
//   - 上記のエラー。Workflow を開始できなかったときは internal_error(保存は取り消してある)。
func (deployService *DeployService) Create(requestContext context.Context, userID string, projectID string, imageRef string, specName string) (string, error) {
	// 1. スペックの名前が、マスターデータにあることを確かめる。
	if _, specError := deployService.deployStore.FindSpec(requestContext, specName); specError != nil {
		return "", specError
	}

	// 2. deploy を pending 状態で保存する。起動設定は空(環境変数なし、コマンドとコマンド引数はイメージの既定)。
	deploy := &model.Deploy{
		ID:        model.DeployIDPrefix + uuid.NewString(),
		ProjectID: projectID,
		UserID:    userID,
		ImageRef:  imageRef,
		Spec:      specName,
		Replicas:  1,
		Status:    model.DeployStatusPending,
		Env:       datatypes.JSON(`{}`),
	}
	if createError := deployService.deployStore.Create(requestContext, deploy); createError != nil {
		return "", createError
	}

	// 3. 起動の Workflow を開始する。成功したら、deploy の ID を返す。
	startError := deployService.deployWorkflowStarter.StartDeployCreation(requestContext, deploy.ID)
	if startError == nil {
		return deploy.ID, nil
	}

	// 4. 開始できなかったので、保存を取り消す。リクエストが中断されていても取り消せるよう、context を切り離す。
	cancelContext := context.WithoutCancel(requestContext)
	if cancelError := deployService.deployStore.DeleteUnstarted(cancelContext, deploy.ID); cancelError != nil {
		// 取り消しにも失敗したときは、両方の原因を残す(pending の deploy が残る)。
		return "", apperror.Wrap(apperror.KindInternal, "deploy の起動を開始できませんでした", joinErrors(startError, cancelError))
	}
	return "", apperror.Wrap(apperror.KindInternal, "deploy の起動を開始できませんでした", startError)
}

// Get は、ユーザーの deploy を1件取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - deployID: 取得する deploy の ID。
//
// 戻り値:
//   - deploy。
//   - 無い、または他のユーザーのものなら not_found。
func (deployService *DeployService) Get(requestContext context.Context, userID string, deployID string) (model.Deploy, error) {
	// ユーザーと deploy ID の両方で探す。他のユーザーのものは、見つからない扱いになる。
	return deployService.deployStore.FindByID(requestContext, userID, deployID)
}
