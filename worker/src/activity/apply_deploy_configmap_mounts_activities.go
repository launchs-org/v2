package activity

import (
	"context"
	"encoding/base64"
	"log/slog"

	"go.temporal.io/sdk/temporal"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// ConfigMapMountsApplyStore は、ConfigMap のマウントの反映の Activity が使う、deploy に関するデータベースの操作。
type ConfigMapMountsApplyStore interface {
	// FindForWorker は、持ち主を問わずに、deploy を1件取得する。
	FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error)

	// FindSpec は、スペックの名前から、大きさを取得する。
	FindSpec(requestContext context.Context, specName string) (model.DeploySpec, error)

	// CommitConfigMapMountsResult は、ConfigMap のマウントの反映の結果を確定する(applying の deploy だけ)。確定できたかを返す。
	CommitConfigMapMountsResult(requestContext context.Context, deployID string, succeeded bool) (bool, error)
}

// ConfigMapRevisionStore は、ConfigMap の版を読む、データベースの操作。
type ConfigMapRevisionStore interface {
	// FindRevisionForWorker は、ConfigMap の版を1件取得する。
	FindRevisionForWorker(requestContext context.Context, configMapID string, revision int) (model.ConfigMapRevision, error)

	// ListInUseResourceNames は、プロジェクトの deploy が使っている(現在と pending)、k8s の ConfigMap の名前を返す。
	ListInUseResourceNames(requestContext context.Context, projectID string) (map[string]bool, error)
}

// ApplyDeployConfigMapMountsActivities は、ConfigMap のマウントの反映の Workflow が使う Activity をまとめたもの。
type ApplyDeployConfigMapMountsActivities struct {
	mountsStore        ConfigMapMountsApplyStore
	revisionStore      ConfigMapRevisionStore
	deploymentOperator DeploymentOperator
	revisionOperator   ConfigMapRevisionOperator
	appLogger          *slog.Logger
}

// NewApplyDeployConfigMapMountsActivities は、ApplyDeployConfigMapMountsActivities を作る。
//
// 引数:
//   - mountsStore: deploy に関するデータベースの操作。
//   - revisionStore: ConfigMap の版に関するデータベースの操作。
//   - deploymentOperator: k8s の Deployment を操作するもの。
//   - revisionOperator: k8s の、版の ConfigMap を操作するもの。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した ApplyDeployConfigMapMountsActivities。
func NewApplyDeployConfigMapMountsActivities(mountsStore ConfigMapMountsApplyStore, revisionStore ConfigMapRevisionStore, deploymentOperator DeploymentOperator, revisionOperator ConfigMapRevisionOperator, appLogger *slog.Logger) *ApplyDeployConfigMapMountsActivities {
	// 受け取ったものを持たせる。
	return &ApplyDeployConfigMapMountsActivities{mountsStore: mountsStore, revisionStore: revisionStore, deploymentOperator: deploymentOperator, revisionOperator: revisionOperator, appLogger: appLogger}
}

// splitConfigMapFiles は、版のファイルを、k8s の ConfigMap の data(テキスト)と binaryData(バイナリ)に振り分ける。
//
// 引数:
//   - files: 版のファイル。
//
// 戻り値:
//   - テキストのファイル(キー → 内容)と、バイナリのファイル(キー → バイト列)。
//   - base64 を読めないときのエラー(検査済みなので、通常は起きない)。
func splitConfigMapFiles(files []model.ConfigMapFile) (map[string]string, map[string][]byte, error) {
	// encoding ごとに振り分ける。
	data := map[string]string{}
	binaryData := map[string][]byte{}
	for _, file := range files {
		if file.Encoding == model.ConfigMapEncodingBase64 {
			decoded, decodeError := base64.StdEncoding.DecodeString(file.Content)
			if decodeError != nil {
				return nil, nil, decodeError
			}
			binaryData[file.Key] = decoded
			continue
		}
		data[file.Key] = file.Content
	}
	return data, binaryData, nil
}

// ApplyPendingConfigMapMounts は、namespace を用意し、pending のマウントが使う版ごとに、k8s の ConfigMap を作り、Deployment を更新する。
//
// 起動引数や環境変数、ボリューム、ファイルは、現在の値を使う(pending の他の設定は使わない)。
// 版の ConfigMap は、更新しない(immutable)ので、すでにあれば、そのまま使う。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 更新する deploy の ID。
//
// 戻り値:
//   - 更新後の Deployment の世代(起動を待つときの、最小の世代として使う)。
//   - deploy を読めなかったとき、適用するマウントが無い・壊れている、版が無いとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (applyActivities *ApplyDeployConfigMapMountsActivities) ApplyPendingConfigMapMounts(requestContext context.Context, input workflow.ApplyPendingConfigMapMountsInput) (int64, error) {
	// どの Activity が何のために動いているかを、ログに残す(ファイルの内容は、出さない)。
	applyActivities.appLogger.Info("pending の ConfigMap のマウントで Deployment を更新する", "deploy_id", input.DeployID, "activity", workflow.ApplyPendingConfigMapMountsActivityName)
	// DB から deploy とスペックを読む。
	deploy, findError := applyActivities.mountsStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return 0, findError
	}
	spec, specError := applyActivities.mountsStore.FindSpec(requestContext, deploy.Spec)
	if specError != nil {
		return 0, specError
	}
	// 適用するマウントが無いのは、何度やっても同じなので、リトライしない。
	if !deploy.HasPendingConfigMapMounts() {
		return 0, temporal.NewNonRetryableApplicationError("適用する ConfigMap のマウントがありません", "NoPendingConfigMapMounts", nil)
	}
	pendingMounts, parseError := model.ParseConfigMapMounts(deploy.PendingConfigMapMounts)
	if parseError != nil {
		return 0, temporal.NewNonRetryableApplicationError("ConfigMap のマウントが壊れています", "InvalidConfigMapMounts", parseError)
	}
	// namespace を用意する(まだ deploy が起動していないプロジェクトでは、無いことがある)。
	if namespaceError := applyActivities.deploymentOperator.EnsureNamespace(requestContext, deploy.ProjectID); namespaceError != nil {
		return 0, markNonRetryable(namespaceError)
	}
	// 使う版ごとに、1度だけ、k8s の ConfigMap を用意する(Deployment が、ConfigMap を参照できるように、先に作る)。
	if revisionError := ensureRevisionConfigMaps(requestContext, applyActivities.revisionStore, applyActivities.revisionOperator, deploy.ProjectID, pendingMounts); revisionError != nil {
		return 0, revisionError
	}
	// pending のマウントを使った Deployment の内容を組み立てて、k8s を操作するものに任せる。
	pendingDeploy := deploy
	pendingDeploy.ConfigMapMounts = deploy.PendingConfigMapMounts
	definition, definitionError := newDeploymentDefinition(pendingDeploy, spec)
	if definitionError != nil {
		return 0, temporal.NewNonRetryableApplicationError(definitionError.Error(), "InvalidDeployment", definitionError)
	}
	return applyActivities.deploymentOperator.ApplyDeployment(requestContext, definition)
}

// ensureRevisionConfigMaps は、マウントが使う版ごとに、k8s の ConfigMap(immutable)を、1 度だけ用意する(あれば、そのまま)。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - revisionStore: ConfigMap の版を読む、データベースの操作。
//   - revisionOperator: k8s の、版の ConfigMap を操作するもの。
//   - projectID: プロジェクトの ID(namespace の名前)。
//   - mounts: ConfigMap のマウント。
//
// 戻り値:
//   - 版が DB に無い・壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func ensureRevisionConfigMaps(requestContext context.Context, revisionStore ConfigMapRevisionStore, revisionOperator ConfigMapRevisionOperator, projectID string, mounts []model.ConfigMapMountSpec) error {
	// 使う版ごとに、1 度だけ、用意する。
	prepared := map[string]bool{}
	for _, mount := range mounts {
		resourceName := model.ConfigMapRevisionResourceName(mount.ConfigMapID, mount.Revision)
		if prepared[resourceName] {
			continue
		}
		prepared[resourceName] = true
		// 版の中身を DB から読む(版が無いのは、何度やっても同じなので、リトライしない)。
		revision, revisionError := revisionStore.FindRevisionForWorker(requestContext, mount.ConfigMapID, mount.Revision)
		if revisionError != nil {
			return temporal.NewNonRetryableApplicationError("ConfigMap の版を読めません: "+resourceName, "ConfigMapRevisionNotFound", revisionError)
		}
		files, filesError := model.ParseConfigMapFiles(revision.Files)
		if filesError != nil {
			return temporal.NewNonRetryableApplicationError("ConfigMap の版が壊れています: "+resourceName, "InvalidConfigMapRevision", filesError)
		}
		data, binaryData, splitError := splitConfigMapFiles(files)
		if splitError != nil {
			return temporal.NewNonRetryableApplicationError("ConfigMap の版のバイナリが壊れています: "+resourceName, "InvalidConfigMapRevision", splitError)
		}
		// k8s の ConfigMap を作る。
		if applyError := revisionOperator.ApplyRevisionConfigMap(requestContext, projectID, resourceName, data, binaryData); applyError != nil {
			return markNonRetryable(applyError)
		}
	}
	return nil
}

// RollbackDeploymentConfigMapMounts は、反映に失敗したとき、Deployment を、現在の(動いていた)ConfigMap のマウントに戻す。
//
// 現在の版の k8s の ConfigMap は、使われなくなるまで消さないので、残っている。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 元に戻す deploy の ID。
//
// 戻り値:
//   - deploy を読めなかったとき、値が壊れているとき(リトライしない)、k8s への依頼に失敗したときのエラー。
func (applyActivities *ApplyDeployConfigMapMountsActivities) RollbackDeploymentConfigMapMounts(requestContext context.Context, input workflow.RollbackDeploymentConfigMapMountsInput) error {
	// 運用で気づけるよう、警告で残す。
	applyActivities.appLogger.Warn("反映に失敗したので、Deployment を元の ConfigMap のマウントに戻す", "deploy_id", input.DeployID, "activity", workflow.RollbackDeploymentConfigMapMountsActivityName)
	// DB から deploy とスペックを読み、現在の値で、Deployment の内容を組み立てる。
	deploy, findError := applyActivities.mountsStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}
	spec, specError := applyActivities.mountsStore.FindSpec(requestContext, deploy.Spec)
	if specError != nil {
		return specError
	}
	definition, definitionError := newDeploymentDefinition(deploy, spec)
	if definitionError != nil {
		return temporal.NewNonRetryableApplicationError(definitionError.Error(), "InvalidDeployment", definitionError)
	}
	// k8s を操作するものに任せる。
	if _, applyError := applyActivities.deploymentOperator.ApplyDeployment(requestContext, definition); applyError != nil {
		return markNonRetryable(applyError)
	}
	return nil
}

// CommitConfigMapMountsResult は、ConfigMap のマウントの反映の結果を DB に確定する。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 結果を確定する deploy の ID と、成功したか。
//
// 戻り値:
//   - 確定できなかったときのデータベースのエラー。
func (applyActivities *ApplyDeployConfigMapMountsActivities) CommitConfigMapMountsResult(requestContext context.Context, input workflow.CommitConfigMapMountsResultInput) error {
	// 結果をログに残す(失敗は、運用で気づけるよう警告にする)。
	if input.Succeeded {
		applyActivities.appLogger.Info("ConfigMap のマウントの反映に成功した", "deploy_id", input.DeployID, "activity", workflow.CommitConfigMapMountsResultActivityName)
	} else {
		applyActivities.appLogger.Warn("ConfigMap のマウントの反映に失敗した", "deploy_id", input.DeployID, "activity", workflow.CommitConfigMapMountsResultActivityName)
	}
	// 確定を、保存先に任せる(確定済みなら何もしない)。
	_, commitError := applyActivities.mountsStore.CommitConfigMapMountsResult(requestContext, input.DeployID, input.Succeeded)
	return commitError
}

// CleanupConfigMapRevisions は、プロジェクトのどの deploy も使っていない版の、k8s の ConfigMap を削除する。
//
// 使われているかは、DB の、全ての deploy の現在と pending のマウントで決める(他の deploy が、これから使う版は、消さない)。
// 消せなくても、実害は小さい(残るだけ)ので、次の反映のときに、また消す。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 反映した deploy の ID(プロジェクトを知るために使う)。
//
// 戻り値:
//   - deploy を読めなかったとき、一覧や削除に失敗したときのエラー。
func (applyActivities *ApplyDeployConfigMapMountsActivities) CleanupConfigMapRevisions(requestContext context.Context, input workflow.CleanupConfigMapRevisionsInput) error {
	// deploy から、プロジェクトを知る。
	deploy, findError := applyActivities.mountsStore.FindForWorker(requestContext, input.DeployID)
	if findError != nil {
		return findError
	}
	// どの deploy も使っていない版を、調べる。
	inUse, inUseError := applyActivities.revisionStore.ListInUseResourceNames(requestContext, deploy.ProjectID)
	if inUseError != nil {
		return inUseError
	}
	existing, listError := applyActivities.revisionOperator.ListRevisionConfigMaps(requestContext, deploy.ProjectID)
	if listError != nil {
		return markNonRetryable(listError)
	}
	// 使われていないものを、消す。
	for _, name := range existing {
		if inUse[name] {
			continue
		}
		applyActivities.appLogger.Info("使われていない版の ConfigMap を削除する", "deploy_id", input.DeployID, "configmap", name, "activity", workflow.CleanupConfigMapRevisionsActivityName)
		if deleteError := applyActivities.revisionOperator.DeleteRevisionConfigMap(requestContext, deploy.ProjectID, name); deleteError != nil {
			return markNonRetryable(deleteError)
		}
	}
	return nil
}
