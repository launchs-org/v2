package activity

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"go.temporal.io/sdk/temporal"
	"gorm.io/datatypes"

	"launchs/shared/model"
	"launchs/shared/workflow"
)

// newPrepareActivities は、テスト用の PrepareDeployActivities を作る。
//
// 引数:
//   - deploy: 起動する deploy。
//   - volumes: DB にある volume。
//   - volumeOperator: PVC を操作する役の代わり。
//   - configMapOperator: deploy のファイルの ConfigMap を操作する役の代わり。
//   - revisionOperator: 版の ConfigMap を操作する役の代わり。
//
// 戻り値:
//   - ログを捨てる PrepareDeployActivities。
func newPrepareActivities(deploy model.Deploy, volumes []model.Volume, volumeOperator *fakeVolumeOperator, configMapOperator *fakeConfigMapOperator, revisionOperator *fakeRevisionOperator) *PrepareDeployActivities {
	return NewPrepareDeployActivities(&fakeDeployStore{deploy: deploy}, &fakeVolumeStore{volumes: volumes}, volumeOperator, configMapOperator, newTestRevisionStore(), revisionOperator, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestPrepareDeployDependencies は、起動の前に、PVC、ファイルの ConfigMap、版の ConfigMap を用意し、何も無ければ何もしないことを確認する。
func TestPrepareDeployDependencies(testingT *testing.T) {
	input := workflow.PrepareDeployDependenciesInput{DeployID: "deploy-1"}
	testingT.Run("マウントもファイルも無ければ、何もしない", func(subTest *testing.T) {
		volumeOperator, configMapOperator, revisionOperator := &fakeVolumeOperator{}, &fakeConfigMapOperator{}, &fakeRevisionOperator{}
		activities := newPrepareActivities(newTestDeploy(), nil, volumeOperator, configMapOperator, revisionOperator)
		if prepareError := activities.PrepareDeployDependencies(context.Background(), input); prepareError != nil {
			subTest.Fatal(prepareError)
		}
		if len(volumeOperator.ensured) != 0 || len(configMapOperator.applied) != 0 || revisionOperator.applyCalls != 0 {
			subTest.Errorf("何も用意しないはず: %v %v %d", volumeOperator.ensured, configMapOperator.applied, revisionOperator.applyCalls)
		}
	})
	testingT.Run("マウント、ファイル、ConfigMap のマウントがあれば、PVC、ファイルの ConfigMap、版の ConfigMap を用意する", func(subTest *testing.T) {
		deploy := newTestDeploy()
		deploy.Mounts = datatypes.JSON(`[{"volumeId":"volume-1","mountPath":"/data"}]`)
		deploy.Files = datatypes.JSON(`[{"path":"/etc/b.conf","content":"B"},{"path":"/etc/a.conf","content":"A"}]`)
		deploy.ConfigMapMounts = datatypes.JSON(`[{"configMapId":"` + testConfigMapID + `","revision":1,"key":"app.conf","path":"/etc/c.conf","mode":"0644"}]`)
		volumeOperator, configMapOperator, revisionOperator := &fakeVolumeOperator{}, &fakeConfigMapOperator{}, &fakeRevisionOperator{}
		activities := newPrepareActivities(deploy, []model.Volume{{ID: "volume-1", SizeMB: 512}}, volumeOperator, configMapOperator, revisionOperator)
		if prepareError := activities.PrepareDeployDependencies(context.Background(), input); prepareError != nil {
			subTest.Fatal(prepareError)
		}
		if len(volumeOperator.ensured) != 1 || volumeOperator.ensured[0] != (VolumeClaimDefinition{Namespace: "project-1", Name: "volume-1", SizeMB: 512}) {
			subTest.Errorf("PVC を、現在の大きさで用意するはず: %+v", volumeOperator.ensured)
		}
		data := configMapOperator.applied["project-1/deploy-1"]
		if data["f0"] != "A" || data["f1"] != "B" {
			subTest.Errorf("ファイルの ConfigMap を、path の順のキーで作るはず: %v", data)
		}
		revision := "project-1/" + model.ConfigMapRevisionResourceName(testConfigMapID, 1)
		if revisionOperator.applyCalls != 1 || revisionOperator.applyData[revision]["app.conf"] != "a=1" {
			subTest.Errorf("版の ConfigMap を用意するはず: %d %v", revisionOperator.applyCalls, revisionOperator.applyData)
		}
	})
	testingT.Run("volume が DB に無ければ、リトライしないエラー", func(subTest *testing.T) {
		deploy := newTestDeploy()
		deploy.Mounts = datatypes.JSON(`[{"volumeId":"volume-missing","mountPath":"/data"}]`)
		activities := newPrepareActivities(deploy, nil, &fakeVolumeOperator{}, &fakeConfigMapOperator{}, &fakeRevisionOperator{})
		prepareError := activities.PrepareDeployDependencies(context.Background(), input)
		var applicationError *temporal.ApplicationError
		if !errors.As(prepareError, &applicationError) || !applicationError.NonRetryable() {
			subTest.Errorf("リトライしないエラーのはず: %v", prepareError)
		}
	})
	testingT.Run("マウントが壊れていれば、リトライしないエラー", func(subTest *testing.T) {
		deploy := newTestDeploy()
		deploy.Mounts = datatypes.JSON(`{broken`)
		activities := newPrepareActivities(deploy, nil, &fakeVolumeOperator{}, &fakeConfigMapOperator{}, &fakeRevisionOperator{})
		prepareError := activities.PrepareDeployDependencies(context.Background(), input)
		var applicationError *temporal.ApplicationError
		if !errors.As(prepareError, &applicationError) || !applicationError.NonRetryable() {
			subTest.Errorf("リトライしないエラーのはず: %v", prepareError)
		}
	})
	testingT.Run("k8s への依頼の失敗は、エラーになる", func(subTest *testing.T) {
		deploy := newTestDeploy()
		deploy.Files = datatypes.JSON(`[{"path":"/etc/a","content":"A"}]`)
		activities := newPrepareActivities(deploy, nil, &fakeVolumeOperator{}, &fakeConfigMapOperator{applyError: errors.New("k8s down")}, &fakeRevisionOperator{})
		if prepareError := activities.PrepareDeployDependencies(context.Background(), input); prepareError == nil {
			subTest.Error("エラーになるはず")
		}
	})
}
