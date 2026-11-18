package activity

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"go.temporal.io/sdk/temporal"
	"gorm.io/datatypes"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// fakeFilesStore は、ファイルの反映の Activity のテスト用の、データベースの代わり。
type fakeFilesStore struct {
	deploy      model.Deploy
	commitCalls []bool
	commitError error
}

// FindForWorker は、決めた deploy を返す。
func (fakeStore *fakeFilesStore) FindForWorker(_ context.Context, _ string) (model.Deploy, error) {
	return fakeStore.deploy, nil
}

// FindSpec は、固定のスペックを返す。
func (fakeStore *fakeFilesStore) FindSpec(_ context.Context, specName string) (model.DeploySpec, error) {
	return model.DeploySpec{Name: specName, CPUMillicores: 250, MemoryMiB: 256}, nil
}

// CommitFilesResult は、成否を覚えて、決めたエラーを返す。
func (fakeStore *fakeFilesStore) CommitFilesResult(_ context.Context, _ string, succeeded bool) (bool, error) {
	fakeStore.commitCalls = append(fakeStore.commitCalls, succeeded)
	return true, fakeStore.commitError
}

// fakeConfigMapOperator は、ConfigMap を操作する役の、テスト用の代わり。
type fakeConfigMapOperator struct {
	// applied は、作られた(更新された)ConfigMap("namespace/名前" → データ)。
	applied map[string]map[string]string
	// deleted は、削除を頼まれた ConfigMap("namespace/名前")。
	deleted []string
	// calls は、操作を、呼ばれた順に覚える("apply" と "delete")。
	calls      []string
	applyError error
}

// ApplyConfigMap は、内容を覚えて、決めたエラーを返す。
func (fakeOperator *fakeConfigMapOperator) ApplyConfigMap(_ context.Context, namespaceName string, name string, data map[string]string) error {
	if fakeOperator.applied == nil {
		fakeOperator.applied = map[string]map[string]string{}
	}
	fakeOperator.applied[namespaceName+"/"+name] = data
	fakeOperator.calls = append(fakeOperator.calls, "apply")
	return fakeOperator.applyError
}

// DeleteConfigMap は、削除の依頼を覚える。
func (fakeOperator *fakeConfigMapOperator) DeleteConfigMap(_ context.Context, namespaceName string, name string) error {
	fakeOperator.deleted = append(fakeOperator.deleted, namespaceName+"/"+name)
	fakeOperator.calls = append(fakeOperator.calls, "delete")
	return nil
}

// newFilesActivities は、テスト用の ApplyDeployFilesActivities を作る。
//
// 引数:
//   - store: データベースの代わり。
//   - deploymentOperator: Deployment を操作する役の代わり。
//   - configMapOperator: ConfigMap を操作する役の代わり。
//
// 戻り値:
//   - ログを捨てる ApplyDeployFilesActivities。
func newFilesActivities(store *fakeFilesStore, deploymentOperator *fakeDeploymentOperator, configMapOperator *fakeConfigMapOperator) *ApplyDeployFilesActivities {
	return NewApplyDeployFilesActivities(store, deploymentOperator, configMapOperator, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// newFilesDeploy は、現在のファイルと、pending のファイルを持つ deploy を作る。
//
// 引数:
//   - current: 現在のファイル(JSON。空なら、無し)。
//   - pending: pending のファイル(JSON。空なら、無し)。
//
// 戻り値:
//   - 起動済み(active)の deploy。
func newFilesDeploy(current string, pending string) model.Deploy {
	deploy := newTestDeploy()
	deploy.Status = model.DeployStatusApplying
	if current != "" {
		deploy.Files = datatypes.JSON(current)
	}
	if pending != "" {
		deploy.PendingFiles = datatypes.JSON(pending)
	}
	return deploy
}

// TestApplyPendingFiles は、pending のファイルで、ConfigMap を作ってから、Deployment を更新することを確認する。
func TestApplyPendingFiles(testingT *testing.T) {
	testingT.Run("ConfigMap を先に作り、Deployment に、path の順のキーで、ファイルが付く", func(subTest *testing.T) {
		deploymentOperator := &fakeDeploymentOperator{appliedGeneration: 7}
		configMapOperator := &fakeConfigMapOperator{}
		activities := newFilesActivities(&fakeFilesStore{deploy: newFilesDeploy("", `[{"path":"/etc/b.conf","content":"B"},{"path":"/etc/a.conf","content":"A"}]`)}, deploymentOperator, configMapOperator)
		generation, applyError := activities.ApplyPendingFiles(context.Background(), workflow.ApplyPendingFilesInput{DeployID: "deploy-1"})
		if applyError != nil || generation != 7 {
			subTest.Fatalf("成功して、世代を返すはず: %d %v", generation, applyError)
		}
		// ConfigMap は、path の順に、f0(a.conf)、f1(b.conf)。
		data := configMapOperator.applied["project-1/deploy-1"]
		if data["f0"] != "A" || data["f1"] != "B" || len(data) != 2 {
			subTest.Errorf("キーは path の順のはず: %v", data)
		}
		// Deployment には、同じキーで、ファイルが付く。
		files := deploymentOperator.appliedDefinition.Files
		if len(files) != 2 || files[0] != (FileDefinition{Key: "f0", Path: "/etc/a.conf"}) || files[1] != (FileDefinition{Key: "f1", Path: "/etc/b.conf"}) {
			subTest.Errorf("Deployment のファイルが違う: %+v", files)
		}
		if len(configMapOperator.deleted) != 0 {
			subTest.Errorf("ConfigMap は消さないはず: %v", configMapOperator.deleted)
		}
	})
	testingT.Run("ファイルを全て外すときは、Deployment から外してから、ConfigMap を削除する", func(subTest *testing.T) {
		deploymentOperator := &fakeDeploymentOperator{}
		configMapOperator := &fakeConfigMapOperator{}
		activities := newFilesActivities(&fakeFilesStore{deploy: newFilesDeploy(`[{"path":"/etc/a.conf","content":"A"}]`, `[]`)}, deploymentOperator, configMapOperator)
		if _, applyError := activities.ApplyPendingFiles(context.Background(), workflow.ApplyPendingFilesInput{DeployID: "deploy-1"}); applyError != nil {
			subTest.Fatal(applyError)
		}
		if len(deploymentOperator.appliedDefinition.Files) != 0 || len(configMapOperator.applied) != 0 || len(configMapOperator.deleted) != 1 || configMapOperator.deleted[0] != "project-1/deploy-1" {
			subTest.Errorf("ファイルが外れ、ConfigMap が消えるはず: %+v %v %v", deploymentOperator.appliedDefinition.Files, configMapOperator.applied, configMapOperator.deleted)
		}
	})
	testingT.Run("pending が無ければ、リトライしないエラー", func(subTest *testing.T) {
		activities := newFilesActivities(&fakeFilesStore{deploy: newFilesDeploy(`[]`, "")}, &fakeDeploymentOperator{}, &fakeConfigMapOperator{})
		_, applyError := activities.ApplyPendingFiles(context.Background(), workflow.ApplyPendingFilesInput{DeployID: "deploy-1"})
		var applicationError *temporal.ApplicationError
		if !errors.As(applyError, &applicationError) || !applicationError.NonRetryable() {
			subTest.Errorf("リトライしないエラーのはず: %v", applyError)
		}
	})
	testingT.Run("ConfigMap の作成に失敗したら、Deployment は更新しない", func(subTest *testing.T) {
		deploymentOperator := &fakeDeploymentOperator{}
		activities := newFilesActivities(&fakeFilesStore{deploy: newFilesDeploy("", `[{"path":"/a","content":""}]`)}, deploymentOperator, &fakeConfigMapOperator{applyError: errors.New("k8s down")})
		if _, applyError := activities.ApplyPendingFiles(context.Background(), workflow.ApplyPendingFilesInput{DeployID: "deploy-1"}); applyError == nil || deploymentOperator.appliedDefinition != nil {
			subTest.Errorf("エラーで、Deployment は更新されないはず: %v %v", applyError, deploymentOperator.appliedDefinition)
		}
	})
}

// TestRollbackDeploymentFiles は、失敗したとき、現在のファイルに戻す(ConfigMap を元の内容に、または、無かったなら削除)ことを確認する。
func TestRollbackDeploymentFiles(testingT *testing.T) {
	testingT.Run("元のファイルがあれば、ConfigMap を元の内容に戻し、Deployment も戻す", func(subTest *testing.T) {
		deploymentOperator := &fakeDeploymentOperator{}
		configMapOperator := &fakeConfigMapOperator{}
		activities := newFilesActivities(&fakeFilesStore{deploy: newFilesDeploy(`[{"path":"/etc/old.conf","content":"OLD"}]`, `[{"path":"/etc/new.conf","content":"NEW"}]`)}, deploymentOperator, configMapOperator)
		if rollbackError := activities.RollbackDeploymentFiles(context.Background(), workflow.RollbackDeploymentFilesInput{DeployID: "deploy-1"}); rollbackError != nil {
			subTest.Fatal(rollbackError)
		}
		if configMapOperator.applied["project-1/deploy-1"]["f0"] != "OLD" || len(deploymentOperator.appliedDefinition.Files) != 1 || deploymentOperator.appliedDefinition.Files[0].Path != "/etc/old.conf" {
			subTest.Errorf("元のファイルに戻るはず: %v %+v", configMapOperator.applied, deploymentOperator.appliedDefinition.Files)
		}
	})
	testingT.Run("元のファイルが無ければ、Deployment からファイルを外し、ConfigMap を削除する", func(subTest *testing.T) {
		deploymentOperator := &fakeDeploymentOperator{}
		configMapOperator := &fakeConfigMapOperator{}
		activities := newFilesActivities(&fakeFilesStore{deploy: newFilesDeploy("", `[{"path":"/etc/new.conf","content":"NEW"}]`)}, deploymentOperator, configMapOperator)
		if rollbackError := activities.RollbackDeploymentFiles(context.Background(), workflow.RollbackDeploymentFilesInput{DeployID: "deploy-1"}); rollbackError != nil {
			subTest.Fatal(rollbackError)
		}
		if len(deploymentOperator.appliedDefinition.Files) != 0 || len(configMapOperator.deleted) != 1 || strings.Join(configMapOperator.calls, ",") != "delete" {
			subTest.Errorf("ファイルが外れ、ConfigMap が消えるはず: %+v %v", deploymentOperator.appliedDefinition.Files, configMapOperator.calls)
		}
	})
}

// TestCommitFilesResult は、結果の確定を、データベースに任せることを確認する。
func TestCommitFilesResult(testingT *testing.T) {
	store := &fakeFilesStore{deploy: newFilesDeploy("", "")}
	activities := newFilesActivities(store, &fakeDeploymentOperator{}, &fakeConfigMapOperator{})
	if commitError := activities.CommitFilesResult(context.Background(), workflow.CommitFilesResultInput{DeployID: "deploy-1", Succeeded: false}); commitError != nil || len(store.commitCalls) != 1 || store.commitCalls[0] {
		testingT.Errorf("失敗の確定が、1 回、渡るはず: %v %v", commitError, store.commitCalls)
	}
}

// TestNewDeploymentDefinitionKeepsFiles は、起動設定やマウントの適用でも、現在のファイルが、Deployment の内容から落ちないことを確認する。
func TestNewDeploymentDefinitionKeepsFiles(testingT *testing.T) {
	deploy := newFilesDeploy(`[{"path":"/etc/keep.conf","content":"x"}]`, "")
	definition, definitionError := newDeploymentDefinition(deploy, model.DeploySpec{CPUMillicores: 250, MemoryMiB: 256})
	if definitionError != nil || len(definition.Files) != 1 || definition.Files[0].Path != "/etc/keep.conf" || definition.Files[0].Key != "f0" {
		testingT.Errorf("現在のファイルが、Files に入るはず: %+v %v", definition.Files, definitionError)
	}
	// ファイルが無い deploy では、空。
	empty, _ := newDeploymentDefinition(newFilesDeploy("", ""), model.DeploySpec{})
	if len(empty.Files) != 0 {
		testingT.Errorf("ファイル無しは空のはず: %+v", empty.Files)
	}
}
