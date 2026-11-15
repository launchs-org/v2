package activity

import (
	"context"
	"log/slog"

	"go.temporal.io/sdk/temporal"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// MountsApplyStore は、マウントの反映の Activity が使う、データベースの操作。
type MountsApplyStore interface {
	// FindForWorker は、持ち主を問わずに、deploy を1件取得する。
	FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error)

	// FindSpec は、スペックの名前から、大きさを取得する。
	FindSpec(requestContext context.Context, specName string) (model.DeploySpec, error)

	// CommitMountsResult は、マウントの反映の結果を確定する(applying の deploy だけ)。確定できたかを返す。
	CommitMountsResult(requestContext context.Context, deployID string, succeeded bool) (bool, error)
}

// MountedVolumeStore は、マウントする volume の大きさを調べるための、データベースの操作。
type MountedVolumeStore interface {
	// FindManyForWorker は、同じプロジェクトの volume を、ID の一覧でまとめて取得する。
	FindManyForWorker(requestContext context.Context, projectID string, volumeIDs []string) ([]model.Volume, error)
}

// ApplyDeployMountsActivities は、マウントの反映の Workflow が使う Activity をまとめたもの。
type ApplyDeployMountsActivities struct {
	mountsStore        MountsApplyStore
	volumeStore        MountedVolumeStore
	deploymentOperator DeploymentOperator
	volumeOperator     VolumeOperator
	appLogger          *slog.Logger
}

// NewApplyDeployMountsActivities は、ApplyDeployMountsActivities を作る。
//
// 引数:
//   - mountsStore: deploy に関するデータベースの操作。
//   - volumeStore: volume に関するデータベースの操作。
//   - deploymentOperator: k8s の Deployment を操作するもの。
//   - volumeOperator: k8s の PVC を操作するもの。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した ApplyDeployMountsActivities。
func NewApplyDeployMountsActivities(mountsStore MountsApplyStore, volumeStore MountedVolumeStore, deploymentOperator DeploymentOperator, volumeOperator VolumeOperator, appLogger *slog.Logger) *ApplyDeployMountsActivities {
	// 受け取ったものを持たせる。
	return &ApplyDeployMountsActivities{
		mountsStore: mountsStore, volumeStore: volumeStore, deploymentOperator: deploymentOperator, volumeOperator: volumeOperator, appLogger: appLogger,
	}
}

// ApplyPendingMounts は、namespace と PVC を用意して、pending のマウントで Deployment を更新する。
//
// PVC は、無ければ、volume の現在の大きさで作る(あれば、そのまま)。環境変数や起動引数は、現在の値を使う(pending の起動設定は使わない)。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 更新する deploy の ID。
//
// 戻り値:
//   - 更新後の Deployment の世代(起動を待つときの、最小の世代として使う)。
//   - deploy を読めなかったとき、適用するマウントが無い・壊れている・volume が無いとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (applyDeployMountsActivities *ApplyDeployMountsActivities) ApplyPendingMounts(requestContext context.Context, input workflow.ApplyPendingMountsInput) (int64, error) {
	// どの Activity が何のために動いているかを、ログに残す。
	applyDeployMountsActivities.appLogger.Info("pending のマウントで Deployment を更新する", "deploy_id", input.DeployID, "activity", workflow.ApplyPendingMountsActivityName)
	// DB から deploy とスペックを読む。
	deploy, findError := applyDeployMountsActivities.mountsStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return 0, findError
	}
	spec, specError := applyDeployMountsActivities.mountsStore.FindSpec(requestContext, deploy.Spec)
	if specError != nil {
		return 0, specError
	}
	// 適用するマウントが無いのは、何度やっても同じなので、リトライしない。
	if !deploy.HasPendingMounts() {
		return 0, temporal.NewNonRetryableApplicationError("適用するマウントがありません", "NoPendingMounts", nil)
	}
	pendingMounts, parseError := model.ParseMounts(deploy.PendingMounts)
	if parseError != nil {
		return 0, temporal.NewNonRetryableApplicationError("マウントが壊れています", "InvalidMounts", parseError)
	}
	// namespace を用意する(まだ deploy が起動していないプロジェクトでは、無いことがある)。
	if namespaceError := applyDeployMountsActivities.deploymentOperator.EnsureNamespace(requestContext, deploy.ProjectID); namespaceError != nil {
		return 0, markNonRetryable(namespaceError)
	}
	// マウントする volume の大きさを調べ、PVC を用意する。
	if claimError := applyDeployMountsActivities.ensureClaims(requestContext, deploy.ProjectID, pendingMounts); claimError != nil {
		return 0, claimError
	}
	// pending のマウントを使った Deployment の内容を組み立てて、k8s を操作するものに任せる。
	pendingDeploy := deploy
	pendingDeploy.Mounts = deploy.PendingMounts
	definition, definitionError := newDeploymentDefinition(pendingDeploy, spec)
	if definitionError != nil {
		return 0, temporal.NewNonRetryableApplicationError(definitionError.Error(), "InvalidDeployment", definitionError)
	}
	return applyDeployMountsActivities.deploymentOperator.ApplyDeployment(requestContext, definition)
}

// ensureClaims は、マウントする volume の PVC を、無ければ作る。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - projectID: プロジェクトの ID。
//   - mounts: マウントの構成。
//
// 戻り値:
//   - volume が見つからないとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (applyDeployMountsActivities *ApplyDeployMountsActivities) ensureClaims(requestContext context.Context, projectID string, mounts []model.MountSpec) error {
	// マウントが無ければ、用意するものが無い。
	if len(mounts) == 0 {
		return nil
	}
	// volume の ID を集めて、まとめて読む。
	volumeIDs := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		volumeIDs = append(volumeIDs, mount.VolumeID)
	}
	volumes, findError := applyDeployMountsActivities.volumeStore.FindManyForWorker(requestContext, projectID, volumeIDs)
	if findError != nil {
		return findError
	}
	// ID から引けるようにする。
	volumesByID := map[string]model.Volume{}
	for _, volume := range volumes {
		volumesByID[volume.ID] = volume
	}
	// マウントごとに、PVC を用意する。
	for _, mount := range mounts {
		// volume が無いのは、何度やっても同じなので、リトライしない。
		volume, exists := volumesByID[mount.VolumeID]
		if !exists {
			return temporal.NewNonRetryableApplicationError("volume が見つかりません: "+mount.VolumeID, "VolumeNotFound", nil)
		}
		// 無ければ、現在の大きさで作る。
		claimError := applyDeployMountsActivities.volumeOperator.EnsureVolumeClaim(requestContext, VolumeClaimDefinition{Namespace: projectID, Name: volume.ID, SizeMB: volume.SizeMB})
		if claimError != nil {
			return markNonRetryable(claimError)
		}
	}
	return nil
}

// RollbackDeploymentMounts は、反映に失敗したとき、Deployment を、現在の(動いていた)マウントに戻す。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 元に戻す deploy の ID。
//
// 戻り値:
//   - deploy を読めなかったとき、値が壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (applyDeployMountsActivities *ApplyDeployMountsActivities) RollbackDeploymentMounts(requestContext context.Context, input workflow.RollbackDeploymentMountsInput) error {
	// 運用で気づけるよう、警告で残す。
	applyDeployMountsActivities.appLogger.Warn("反映に失敗したので、Deployment を元のマウントに戻す", "deploy_id", input.DeployID, "activity", workflow.RollbackDeploymentMountsActivityName)
	// DB から deploy とスペックを読み、現在の値で、Deployment の内容を組み立てる。
	deploy, findError := applyDeployMountsActivities.mountsStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}
	spec, specError := applyDeployMountsActivities.mountsStore.FindSpec(requestContext, deploy.Spec)
	if specError != nil {
		return specError
	}
	definition, definitionError := newDeploymentDefinition(deploy, spec)
	if definitionError != nil {
		return temporal.NewNonRetryableApplicationError(definitionError.Error(), "InvalidDeployment", definitionError)
	}
	// k8s を操作するものに任せる。
	_, applyError := applyDeployMountsActivities.deploymentOperator.ApplyDeployment(requestContext, definition)
	return markNonRetryable(applyError)
}

// CommitMountsResult は、マウントの反映の結果を DB に確定する。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 結果を確定する deploy の ID と、成功したか。
//
// 戻り値:
//   - 確定できなかったときのデータベースのエラー。
func (applyDeployMountsActivities *ApplyDeployMountsActivities) CommitMountsResult(requestContext context.Context, input workflow.CommitMountsResultInput) error {
	// 結果をログに残す(失敗は、運用で気づけるよう警告にする)。
	if input.Succeeded {
		applyDeployMountsActivities.appLogger.Info("マウントの反映に成功した", "deploy_id", input.DeployID, "activity", workflow.CommitMountsResultActivityName)
	} else {
		applyDeployMountsActivities.appLogger.Warn("マウントの反映に失敗した", "deploy_id", input.DeployID, "activity", workflow.CommitMountsResultActivityName)
	}
	// 確定を、保存先に任せる(確定済みなら何もしない)。
	_, commitError := applyDeployMountsActivities.mountsStore.CommitMountsResult(requestContext, input.DeployID, input.Succeeded)
	return commitError
}
