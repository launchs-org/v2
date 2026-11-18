package activity

import (
	"context"
	"log/slog"

	"go.temporal.io/sdk/temporal"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// PrepareDeployStore は、起動前の準備の Activity が使う、deploy に関するデータベースの操作。
type PrepareDeployStore interface {
	// FindForWorker は、持ち主を問わずに、deploy を1件取得する。
	FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error)
}

// PrepareDeployActivities は、deploy を起動する前に、必要なものを k8s に用意する Activity をまとめたもの。
type PrepareDeployActivities struct {
	deployStore       PrepareDeployStore
	volumeStore       MountedVolumeStore
	volumeOperator    VolumeOperator
	configMapOperator ConfigMapOperator
	revisionStore     ConfigMapRevisionStore
	revisionOperator  ConfigMapRevisionOperator
	appLogger         *slog.Logger
}

// NewPrepareDeployActivities は、PrepareDeployActivities を作る。
//
// 引数:
//   - deployStore: deploy に関するデータベースの操作。
//   - volumeStore: volume に関するデータベースの操作。
//   - volumeOperator: k8s の PVC を操作するもの。
//   - configMapOperator: k8s の、deploy のファイルの ConfigMap を操作するもの。
//   - revisionStore: ConfigMap の版に関するデータベースの操作。
//   - revisionOperator: k8s の、版の ConfigMap を操作するもの。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した PrepareDeployActivities。
func NewPrepareDeployActivities(deployStore PrepareDeployStore, volumeStore MountedVolumeStore, volumeOperator VolumeOperator, configMapOperator ConfigMapOperator, revisionStore ConfigMapRevisionStore, revisionOperator ConfigMapRevisionOperator, appLogger *slog.Logger) *PrepareDeployActivities {
	// 受け取ったものを持たせる。
	return &PrepareDeployActivities{deployStore: deployStore, volumeStore: volumeStore, volumeOperator: volumeOperator, configMapOperator: configMapOperator, revisionStore: revisionStore, revisionOperator: revisionOperator, appLogger: appLogger}
}

// PrepareDeployDependencies は、deploy の、現在のマウントとファイルに必要なものを、起動(Deployment の作成)の前に、k8s に用意する。
//
// 用意するもの:
//   - マウントする volume の PVC(無ければ、現在の大きさで作る)
//   - deploy のファイルの ConfigMap(名前は deploy の ID)
//   - ConfigMap のマウントが使う、版の ConfigMap(immutable)
//
// マウントもファイルも無い、普通の deploy では、何もしない。テンプレートから作る deploy は、最初から、これらを持っているので、
// 最初の起動から、ファイルが読める(あとから、マウントして、Pod を作り直さない)。
// どれも冪等(すでにあれば、そのまま)。namespace は、この前に用意されている。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 起動する deploy の ID。
//
// 戻り値:
//   - deploy を読めなかったとき、値が壊れている・volume や版が無いとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (prepareDeployActivities *PrepareDeployActivities) PrepareDeployDependencies(requestContext context.Context, input workflow.PrepareDeployDependenciesInput) error {
	// DB から deploy を読む。
	deploy, findError := prepareDeployActivities.deployStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}
	// 現在のマウント、ファイル、ConfigMap のマウントを読む(壊れていたら、何度やっても同じなので、リトライしない)。
	mounts, mountsError := model.ParseMounts(deploy.Mounts)
	files, filesError := model.ParseFiles(deploy.Files)
	configMapMounts, configMapMountsError := model.ParseConfigMapMounts(deploy.ConfigMapMounts)
	if mountsError != nil || filesError != nil || configMapMountsError != nil {
		return temporal.NewNonRetryableApplicationError("deploy のマウントまたはファイルが壊れています", "InvalidDeploy", nil)
	}
	// 何も無ければ、用意するものが無い。
	if len(mounts) == 0 && len(files) == 0 && len(configMapMounts) == 0 {
		return nil
	}
	prepareDeployActivities.appLogger.Info("起動の前に、マウントとファイルに必要なものを用意する", "deploy_id", input.DeployID, "activity", workflow.PrepareDeployDependenciesActivityName,
		"volumes", len(mounts), "files", len(files), "configmap_mounts", len(configMapMounts))
	// 1. volume の PVC。
	if claimError := ensureVolumeClaims(requestContext, prepareDeployActivities.volumeStore, prepareDeployActivities.volumeOperator, deploy.ProjectID, mounts); claimError != nil {
		return claimError
	}
	// 2. deploy のファイルの ConfigMap(ファイルがあるときだけ)。
	if len(files) > 0 {
		if configMapError := prepareDeployActivities.configMapOperator.ApplyConfigMap(requestContext, deploy.ProjectID, deploy.ID, newConfigMapData(files)); configMapError != nil {
			return markNonRetryable(configMapError)
		}
	}
	// 3. ConfigMap のマウントが使う、版の ConfigMap。
	return ensureRevisionConfigMaps(requestContext, prepareDeployActivities.revisionStore, prepareDeployActivities.revisionOperator, deploy.ProjectID, configMapMounts)
}
