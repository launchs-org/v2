package activity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"gorm.io/datatypes"

	"launchs/shared/logger"
	"launchs/shared/model"
	"launchs/shared/workflow"
)

// fakeVolumeStore は、テスト用の VolumeStore と MountedVolumeStore。決めた volume を返し、呼び出しを覚える。
type fakeVolumeStore struct {
	volume        model.Volume
	volumes       []model.Volume
	findError     error
	commits       []string
	deletedRecord []string
	failedMarks   []string
	storeError    error
}

// FindForWorker は、決めた volume とエラーを返す。
func (fakeStore *fakeVolumeStore) FindForWorker(_ context.Context, _ string) (model.Volume, error) {
	return fakeStore.volume, fakeStore.findError
}

// FindManyForWorker は、決めた volume の一覧とエラーを返す。
func (fakeStore *fakeVolumeStore) FindManyForWorker(_ context.Context, _ string, _ []string) ([]model.Volume, error) {
	return fakeStore.volumes, fakeStore.findError
}

// CommitSizeApply は、結果を覚えて、決めたエラーを返す。
func (fakeStore *fakeVolumeStore) CommitSizeApply(_ context.Context, volumeID string, succeeded bool) (bool, error) {
	fakeStore.commits = append(fakeStore.commits, fmt.Sprintf("%s|%v", volumeID, succeeded))
	return true, fakeStore.storeError
}

// DeleteRecord は、ID を覚える。
func (fakeStore *fakeVolumeStore) DeleteRecord(_ context.Context, volumeID string) error {
	fakeStore.deletedRecord = append(fakeStore.deletedRecord, volumeID)
	return fakeStore.storeError
}

// MarkDeleteFailed は、ID を覚える。
func (fakeStore *fakeVolumeStore) MarkDeleteFailed(_ context.Context, volumeID string) error {
	fakeStore.failedMarks = append(fakeStore.failedMarks, volumeID)
	return fakeStore.storeError
}

// fakeVolumeOperator は、テスト用の VolumeOperator。呼び出しを覚え、決めたエラーを返す。
type fakeVolumeOperator struct {
	ensured       []VolumeClaimDefinition
	expanded      []VolumeClaimDefinition
	deletedClaims []string
	heartbeats    int
	err           error
}

// EnsureVolumeClaim は、渡された内容を覚えて、決めたエラーを返す。
func (fakeOperator *fakeVolumeOperator) EnsureVolumeClaim(_ context.Context, definition VolumeClaimDefinition) error {
	fakeOperator.ensured = append(fakeOperator.ensured, definition)
	return fakeOperator.err
}

// ExpandVolumeClaim は、渡された内容を覚え、生きていることを1回伝えて、決めたエラーを返す。
func (fakeOperator *fakeVolumeOperator) ExpandVolumeClaim(_ context.Context, definition VolumeClaimDefinition, heartbeat func()) error {
	fakeOperator.expanded = append(fakeOperator.expanded, definition)
	heartbeat()
	fakeOperator.heartbeats++
	return fakeOperator.err
}

// DeleteVolumeClaim は、namespace と名前を覚えて、決めたエラーを返す。
func (fakeOperator *fakeVolumeOperator) DeleteVolumeClaim(_ context.Context, namespaceName string, claimName string) error {
	fakeOperator.deletedClaims = append(fakeOperator.deletedClaims, namespaceName+"/"+claimName)
	return fakeOperator.err
}

// newTestVolume は、現在 1000MB で、pending が 2000MB の volume を作る。
func newTestVolume() model.Volume {
	pending := 2000
	return model.Volume{ID: "volume-1", ProjectID: "project-1", SizeMB: 1000, PendingSizeMB: &pending, Status: model.VolumeStatusApplying}
}

// newTestVolumeActivities は、テスト用の VolumeActivities を作る(ログは捨てる)。
func newTestVolumeActivities(store *fakeVolumeStore, operator *fakeVolumeOperator) *VolumeActivities {
	return NewVolumeActivities(store, operator, logger.New(&bytes.Buffer{}, "worker", slog.LevelInfo))
}

// runActivity は、Temporal の Activity のテスト環境で、Activity を実行する。
//
// 引数:
//   - activityFunction: 実行する Activity。
//   - input: 入力。
//
// 戻り値:
//   - Activity のエラー。
func runActivity(activityFunction any, input any) error {
	// 環境を作って、登録し、実行する。
	testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	testEnvironment.RegisterActivity(activityFunction)
	_, runError := testEnvironment.ExecuteActivity(activityFunction, input)
	return runError
}

// TestExpandVolumeClaim は、pending の大きさで PVC の拡張を頼み(待機中は生きていることを伝える)、
// 適用する大きさが無い・時間切れ・名前の不正はリトライしないエラーにすることを確認する。
func TestExpandVolumeClaim(testingT *testing.T) {
	input := workflow.ExpandVolumeClaimInput{VolumeID: "volume-1"}
	testingT.Run("pending の大きさで拡張を頼む", func(subTest *testing.T) {
		operator := &fakeVolumeOperator{}
		activities := newTestVolumeActivities(&fakeVolumeStore{volume: newTestVolume()}, operator)
		if runError := runActivity(activities.ExpandVolumeClaim, input); runError != nil {
			subTest.Fatalf("成功するはず: %v", runError)
		}
		if len(operator.expanded) != 1 || operator.expanded[0] != (VolumeClaimDefinition{Namespace: "project-1", Name: "volume-1", SizeMB: 2000}) || operator.heartbeats != 1 {
			subTest.Errorf("pending の大きさが渡るはず: %+v", operator.expanded)
		}
	})
	testingT.Run("pending が無いなら、リトライしないエラー(k8s には触れない)", func(subTest *testing.T) {
		volume := newTestVolume()
		volume.PendingSizeMB = nil
		operator := &fakeVolumeOperator{}
		runError := runActivity(newTestVolumeActivities(&fakeVolumeStore{volume: volume}, operator).ExpandVolumeClaim, input)
		var applicationError *temporal.ApplicationError
		if !errors.As(runError, &applicationError) || !applicationError.NonRetryable() || len(operator.expanded) != 0 {
			subTest.Errorf("リトライしないエラーのはず: %v", runError)
		}
	})
	for name, operatorError := range map[string]error{"時間切れ": ErrVolumeExpandTimeout, "名前の不正": ErrInvalidVolumeName} {
		testingT.Run(name+"は、リトライしないエラー", func(subTest *testing.T) {
			activities := newTestVolumeActivities(&fakeVolumeStore{volume: newTestVolume()}, &fakeVolumeOperator{err: fmt.Errorf("%w: x", operatorError)})
			runError := runActivity(activities.ExpandVolumeClaim, input)
			var applicationError *temporal.ApplicationError
			if !errors.As(runError, &applicationError) || !applicationError.NonRetryable() {
				subTest.Errorf("リトライしないはず: %v", runError)
			}
		})
	}
	testingT.Run("volume を読めない、k8s への依頼が失敗したら、リトライされるエラー", func(subTest *testing.T) {
		if runError := runActivity(newTestVolumeActivities(&fakeVolumeStore{findError: errors.New("db down")}, &fakeVolumeOperator{}).ExpandVolumeClaim, input); runError == nil {
			subTest.Error("エラーになるはず")
		}
		activities := newTestVolumeActivities(&fakeVolumeStore{volume: newTestVolume()}, &fakeVolumeOperator{err: errors.New("k8s down")})
		runError := runActivity(activities.ExpandVolumeClaim, input)
		var applicationError *temporal.ApplicationError
		if runError == nil || (errors.As(runError, &applicationError) && applicationError.NonRetryable()) {
			subTest.Errorf("リトライされるエラーのはず: %v", runError)
		}
	})
}

// TestVolumeCommitAndDelete は、結果の確定、PVC とレコードの削除、削除失敗の記録を、保存先と k8s に任せることを確認する。
func TestVolumeCommitAndDelete(testingT *testing.T) {
	store := &fakeVolumeStore{volume: newTestVolume()}
	operator := &fakeVolumeOperator{}
	activities := newTestVolumeActivities(store, operator)
	// 結果の確定(成功と失敗)。
	_ = runActivity(activities.CommitVolumeSizeResult, workflow.CommitVolumeSizeResultInput{VolumeID: "volume-1", Succeeded: true})
	_ = runActivity(activities.CommitVolumeSizeResult, workflow.CommitVolumeSizeResultInput{VolumeID: "volume-1", Succeeded: false})
	if len(store.commits) != 2 || store.commits[0] != "volume-1|true" || store.commits[1] != "volume-1|false" {
		testingT.Errorf("確定が渡るはず: %v", store.commits)
	}
	// PVC の削除は、volume の namespace と名前で行う。
	if deleteError := runActivity(activities.DeleteVolumeClaim, workflow.DeleteVolumeClaimInput{VolumeID: "volume-1"}); deleteError != nil || len(operator.deletedClaims) != 1 || operator.deletedClaims[0] != "project-1/volume-1" {
		testingT.Errorf("namespace と名前で削除するはず: %v %v", deleteError, operator.deletedClaims)
	}
	// レコードの削除と、削除失敗の記録。
	_ = runActivity(activities.DeleteVolumeRecord, workflow.DeleteVolumeRecordInput{VolumeID: "volume-1"})
	_ = runActivity(activities.MarkVolumeDeleteFailed, workflow.MarkVolumeDeleteFailedInput{VolumeID: "volume-1"})
	if len(store.deletedRecord) != 1 || len(store.failedMarks) != 1 {
		testingT.Errorf("保存先に任せるはず: %v %v", store.deletedRecord, store.failedMarks)
	}
	// volume を読めないときは、PVC の削除はエラーになる。
	broken := newTestVolumeActivities(&fakeVolumeStore{findError: errors.New("db down")}, operator)
	if deleteError := runActivity(broken.DeleteVolumeClaim, workflow.DeleteVolumeClaimInput{VolumeID: "volume-1"}); deleteError == nil {
		testingT.Error("エラーになるはず")
	}
}

// fakeMountsStore は、テスト用の MountsApplyStore。決めた deploy を返し、確定を覚える。
type fakeMountsStore struct {
	deploy  model.Deploy
	commits []string
}

// FindForWorker は、決めた deploy を返す。
func (fakeStore *fakeMountsStore) FindForWorker(_ context.Context, _ string) (model.Deploy, error) {
	return fakeStore.deploy, nil
}

// FindSpec は、small 相当の大きさを返す。
func (fakeStore *fakeMountsStore) FindSpec(_ context.Context, specName string) (model.DeploySpec, error) {
	return model.DeploySpec{Name: specName, CPUMillicores: 250, MemoryMiB: 256}, nil
}

// CommitMountsResult は、結果を覚える。
func (fakeStore *fakeMountsStore) CommitMountsResult(_ context.Context, deployID string, succeeded bool) (bool, error) {
	fakeStore.commits = append(fakeStore.commits, fmt.Sprintf("%s|%v", deployID, succeeded))
	return true, nil
}

// newMountsDeploy は、現在は /old に volume-old、pending は /data に volume-1 をマウントする deploy を作る。pending の起動設定も持つ。
func newMountsDeploy() model.Deploy {
	deploy := newPendingConfigDeploy()
	deploy.Status = model.DeployStatusApplying
	deploy.Mounts = datatypes.JSON(`[{"volumeId":"volume-old","mountPath":"/old"}]`)
	deploy.PendingMounts = datatypes.JSON(`[{"volumeId":"volume-1","mountPath":"/data"}]`)
	return deploy
}

// TestApplyDeployMountsActivities は、namespace と PVC を用意して pending のマウントで Deployment を作り(起動設定は現在の値)、
// 巻き戻しは現在のマウントで作ることと、不正な入力をリトライしないエラーにすることを確認する。
func TestApplyDeployMountsActivities(testingT *testing.T) {
	input := workflow.ApplyPendingMountsInput{DeployID: "deploy-1"}
	rollbackInput := workflow.RollbackDeploymentMountsInput{DeployID: "deploy-1"}
	newActivities := func(deploy model.Deploy, volumes []model.Volume) (*ApplyDeployMountsActivities, *fakeDeploymentOperator, *fakeVolumeOperator, *fakeMountsStore) {
		deploymentOperator := &fakeDeploymentOperator{appliedGeneration: 7}
		volumeOperator := &fakeVolumeOperator{}
		store := &fakeMountsStore{deploy: deploy}
		activities := NewApplyDeployMountsActivities(store, &fakeVolumeStore{volumes: volumes}, deploymentOperator, volumeOperator, logger.New(&bytes.Buffer{}, "worker", slog.LevelInfo))
		return activities, deploymentOperator, volumeOperator, store
	}
	testingT.Run("namespace と PVC(volume の現在の大きさ)を用意し、pending のマウントと、現在の起動設定で Deployment を作る", func(subTest *testing.T) {
		activities, deploymentOperator, volumeOperator, _ := newActivities(newMountsDeploy(), []model.Volume{{ID: "volume-1", ProjectID: "project-1", SizeMB: 1500}})
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		testEnvironment.RegisterActivity(activities.ApplyPendingMounts)
		result, runError := testEnvironment.ExecuteActivity(activities.ApplyPendingMounts, input)
		if runError != nil {
			subTest.Fatalf("成功するはず: %v", runError)
		}
		var generation int64
		_ = result.Get(&generation)
		definition := deploymentOperator.appliedDefinition
		if generation != 7 || len(deploymentOperator.ensuredNamespaces) != 1 || deploymentOperator.ensuredNamespaces[0] != "project-1" {
			subTest.Fatalf("世代と namespace が違う: %d %v", generation, deploymentOperator.ensuredNamespaces)
		}
		if len(volumeOperator.ensured) != 1 || volumeOperator.ensured[0] != (VolumeClaimDefinition{Namespace: "project-1", Name: "volume-1", SizeMB: 1500}) {
			subTest.Errorf("PVC を用意するはず: %+v", volumeOperator.ensured)
		}
		if len(definition.Mounts) != 1 || definition.Mounts[0] != (MountDefinition{ClaimName: "volume-1", MountPath: "/data"}) || definition.Env["PORT"] != "8080" {
			subTest.Errorf("pending のマウントと現在の環境変数のはず: %+v", definition)
		}
	})
	testingT.Run("全て外す(空の配列)なら、PVC は用意せず、マウント無しで Deployment を作る", func(subTest *testing.T) {
		deploy := newMountsDeploy()
		deploy.PendingMounts = datatypes.JSON(`[]`)
		activities, deploymentOperator, volumeOperator, _ := newActivities(deploy, nil)
		if runError := runActivity(activities.ApplyPendingMounts, input); runError != nil {
			subTest.Fatalf("成功するはず: %v", runError)
		}
		if len(volumeOperator.ensured) != 0 || len(deploymentOperator.appliedDefinition.Mounts) != 0 {
			subTest.Errorf("マウント無しのはず: %+v", deploymentOperator.appliedDefinition)
		}
	})
	testingT.Run("巻き戻しは、現在のマウントで作る", func(subTest *testing.T) {
		activities, deploymentOperator, _, _ := newActivities(newMountsDeploy(), nil)
		if runError := runActivity(activities.RollbackDeploymentMounts, rollbackInput); runError != nil {
			subTest.Fatalf("成功するはず: %v", runError)
		}
		mounts := deploymentOperator.appliedDefinition.Mounts
		if len(mounts) != 1 || mounts[0] != (MountDefinition{ClaimName: "volume-old", MountPath: "/old"}) {
			subTest.Errorf("現在のマウントのはず: %+v", mounts)
		}
	})
	testingT.Run("pending が無い、壊れている、volume が無いなら、リトライしないエラー", func(subTest *testing.T) {
		noPending := newMountsDeploy()
		noPending.PendingMounts = nil
		broken := newMountsDeploy()
		broken.PendingMounts = datatypes.JSON(`{`)
		for name, testCase := range map[string]struct {
			deploy  model.Deploy
			volumes []model.Volume
		}{"pending が無い": {noPending, nil}, "壊れている": {broken, nil}, "volume が無い": {newMountsDeploy(), nil}} {
			activities, deploymentOperator, _, _ := newActivities(testCase.deploy, testCase.volumes)
			runError := runActivity(activities.ApplyPendingMounts, input)
			var applicationError *temporal.ApplicationError
			if !errors.As(runError, &applicationError) || !applicationError.NonRetryable() || deploymentOperator.appliedDefinition != nil {
				subTest.Errorf("%s: リトライしないエラーのはず: %v", name, runError)
			}
		}
	})
	testingT.Run("結果の確定は、保存先に任せる", func(subTest *testing.T) {
		activities, _, _, store := newActivities(newMountsDeploy(), nil)
		_ = runActivity(activities.CommitMountsResult, workflow.CommitMountsResultInput{DeployID: "deploy-1", Succeeded: true})
		_ = runActivity(activities.CommitMountsResult, workflow.CommitMountsResultInput{DeployID: "deploy-1", Succeeded: false})
		if len(store.commits) != 2 || store.commits[0] != "deploy-1|true" || store.commits[1] != "deploy-1|false" {
			subTest.Errorf("確定が渡るはず: %v", store.commits)
		}
	})
}
