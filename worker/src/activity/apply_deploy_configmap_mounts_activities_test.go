package activity

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"testing"

	"go.temporal.io/sdk/temporal"
	"gorm.io/datatypes"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/workflow"
)

// 使うテスト用の ConfigMap の ID(UUID の形)。
const testConfigMapID = "configmap-0a1b2c3d-0000-4000-8000-000000000001"

// fakeConfigMapMountsStore は、ConfigMap のマウントの反映の Activity のテスト用の、deploy のデータベースの代わり。
type fakeConfigMapMountsStore struct {
	deploy      model.Deploy
	commitCalls []bool
}

// FindForWorker は、決めた deploy を返す。
func (fakeStore *fakeConfigMapMountsStore) FindForWorker(_ context.Context, _ string) (model.Deploy, error) {
	return fakeStore.deploy, nil
}

// FindSpec は、固定のスペックを返す。
func (fakeStore *fakeConfigMapMountsStore) FindSpec(_ context.Context, specName string) (model.DeploySpec, error) {
	return model.DeploySpec{Name: specName, CPUMillicores: 250, MemoryMiB: 256}, nil
}

// CommitConfigMapMountsResult は、成否を覚える。
func (fakeStore *fakeConfigMapMountsStore) CommitConfigMapMountsResult(_ context.Context, _ string, succeeded bool) (bool, error) {
	fakeStore.commitCalls = append(fakeStore.commitCalls, succeeded)
	return true, nil
}

// fakeRevisionStore は、ConfigMap の版のデータベースの代わり。
type fakeRevisionStore struct {
	// revisions は、"名前(<id>-r<版>)" → 版の行。
	revisions map[string]model.ConfigMapRevision
	inUse     map[string]bool
}

// FindRevisionForWorker は、決めた版を返す。無ければ not_found。
func (fakeStore *fakeRevisionStore) FindRevisionForWorker(_ context.Context, configMapID string, revision int) (model.ConfigMapRevision, error) {
	found, exists := fakeStore.revisions[model.ConfigMapRevisionResourceName(configMapID, revision)]
	if !exists {
		return model.ConfigMapRevision{}, apperror.New(apperror.KindNotFound, "無い")
	}
	return found, nil
}

// ListInUseResourceNames は、決めた集合を返す。
func (fakeStore *fakeRevisionStore) ListInUseResourceNames(_ context.Context, _ string) (map[string]bool, error) {
	return fakeStore.inUse, nil
}

// fakeRevisionOperator は、版の ConfigMap を操作する役の、テスト用の代わり。
type fakeRevisionOperator struct {
	applyData   map[string]map[string]string
	applyBinary map[string]map[string][]byte
	applyCalls  int
	existing    []string
	deleted     []string
	applyError  error
}

// ApplyRevisionConfigMap は、内容を覚えて、決めたエラーを返す。
func (fakeOperator *fakeRevisionOperator) ApplyRevisionConfigMap(_ context.Context, namespaceName string, name string, data map[string]string, binaryData map[string][]byte) error {
	if fakeOperator.applyData == nil {
		fakeOperator.applyData = map[string]map[string]string{}
		fakeOperator.applyBinary = map[string]map[string][]byte{}
	}
	fakeOperator.applyData[namespaceName+"/"+name] = data
	fakeOperator.applyBinary[namespaceName+"/"+name] = binaryData
	fakeOperator.applyCalls++
	return fakeOperator.applyError
}

// ListRevisionConfigMaps は、決めた一覧を返す。
func (fakeOperator *fakeRevisionOperator) ListRevisionConfigMaps(_ context.Context, _ string) ([]string, error) {
	return fakeOperator.existing, nil
}

// DeleteRevisionConfigMap は、削除の依頼を覚える。
func (fakeOperator *fakeRevisionOperator) DeleteRevisionConfigMap(_ context.Context, _ string, name string) error {
	fakeOperator.deleted = append(fakeOperator.deleted, name)
	return nil
}

// newConfigMapMountsActivities は、テスト用の ApplyDeployConfigMapMountsActivities を作る。
//
// 引数:
//   - store: deploy のデータベースの代わり。
//   - revisionStore: 版のデータベースの代わり。
//   - deploymentOperator: Deployment を操作する役の代わり。
//   - revisionOperator: 版の ConfigMap を操作する役の代わり。
//
// 戻り値:
//   - ログを捨てる ApplyDeployConfigMapMountsActivities。
func newConfigMapMountsActivities(store *fakeConfigMapMountsStore, revisionStore *fakeRevisionStore, deploymentOperator *fakeDeploymentOperator, revisionOperator *fakeRevisionOperator) *ApplyDeployConfigMapMountsActivities {
	return NewApplyDeployConfigMapMountsActivities(store, revisionStore, deploymentOperator, revisionOperator, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// newTestRevisionStore は、版 1(テキストとバイナリ)と版 2(テキスト)を持つ、版のデータベースの代わりを作る。
//
// 戻り値:
//   - 版のデータベースの代わり。
func newTestRevisionStore() *fakeRevisionStore {
	binary := base64.StdEncoding.EncodeToString([]byte{0, 1, 2})
	return &fakeRevisionStore{revisions: map[string]model.ConfigMapRevision{
		model.ConfigMapRevisionResourceName(testConfigMapID, 1): {ConfigMapID: testConfigMapID, Revision: 1, Files: datatypes.JSON(`[{"key":"app.conf","encoding":"text","content":"a=1"},{"key":"blob.bin","encoding":"base64","content":"` + binary + `"}]`)},
		model.ConfigMapRevisionResourceName(testConfigMapID, 2): {ConfigMapID: testConfigMapID, Revision: 2, Files: datatypes.JSON(`[{"key":"app.conf","encoding":"text","content":"a=2"}]`)},
	}}
}

// newConfigMapMountsDeploy は、現在の ConfigMap のマウントと、pending のマウントを持つ deploy を作る。
//
// 引数:
//   - current: 現在のマウント(JSON。空なら、無し)。
//   - pending: pending のマウント(JSON。空なら、無し)。
//
// 戻り値:
//   - 反映中(applying)の deploy。
func newConfigMapMountsDeploy(current string, pending string) model.Deploy {
	deploy := newTestDeploy()
	deploy.Status = model.DeployStatusApplying
	if current != "" {
		deploy.ConfigMapMounts = datatypes.JSON(current)
	}
	if pending != "" {
		deploy.PendingConfigMapMounts = datatypes.JSON(pending)
	}
	return deploy
}

// TestApplyPendingConfigMapMounts は、版ごとに ConfigMap を作ってから、Deployment を更新することを確認する。
func TestApplyPendingConfigMapMounts(testingT *testing.T) {
	pending := `[{"configMapId":"` + testConfigMapID + `","revision":1,"key":"app.conf","path":"/etc/b.conf","mode":"0600"},` +
		`{"configMapId":"` + testConfigMapID + `","revision":1,"key":"blob.bin","path":"/etc/a.bin","mode":"0644"},` +
		`{"configMapId":"` + testConfigMapID + `","revision":2,"key":"app.conf","path":"/etc/c.conf","mode":"0644"}]`
	testingT.Run("使う版ごとに 1 度だけ ConfigMap を作る(テキストは data、バイナリは binaryData)。Deployment には path の順で付く", func(subTest *testing.T) {
		deploymentOperator := &fakeDeploymentOperator{appliedGeneration: 5}
		revisionOperator := &fakeRevisionOperator{}
		activities := newConfigMapMountsActivities(&fakeConfigMapMountsStore{deploy: newConfigMapMountsDeploy("", pending)}, newTestRevisionStore(), deploymentOperator, revisionOperator)
		generation, applyError := activities.ApplyPendingConfigMapMounts(context.Background(), workflow.ApplyPendingConfigMapMountsInput{DeployID: "deploy-1"})
		if applyError != nil || generation != 5 {
			subTest.Fatalf("成功して、世代を返すはず: %d %v", generation, applyError)
		}
		// 版 1 と版 2 の 2 つだけ(版 1 を 2 つのマウントが使うが、1 度だけ作る)。
		if revisionOperator.applyCalls != 2 {
			subTest.Errorf("版ごとに 1 度だけ作るはず: %d", revisionOperator.applyCalls)
		}
		revision1 := "project-1/" + model.ConfigMapRevisionResourceName(testConfigMapID, 1)
		if revisionOperator.applyData[revision1]["app.conf"] != "a=1" || len(revisionOperator.applyData[revision1]) != 1 ||
			string(revisionOperator.applyBinary[revision1]["blob.bin"]) != "\x00\x01\x02" || len(revisionOperator.applyBinary[revision1]) != 1 {
			subTest.Errorf("テキストは data、バイナリは binaryData のはず: %v %v", revisionOperator.applyData[revision1], revisionOperator.applyBinary[revision1])
		}
		// Deployment のマウントは、path の順で、mode が数値になる。
		mounts := deploymentOperator.appliedDefinition.ConfigMapMounts
		name1, name2 := model.ConfigMapRevisionResourceName(testConfigMapID, 1), model.ConfigMapRevisionResourceName(testConfigMapID, 2)
		if len(mounts) != 3 || mounts[0] != (ConfigMapMountDefinition{ConfigMapName: name1, Key: "blob.bin", Path: "/etc/a.bin", Mode: 0o644}) ||
			mounts[1] != (ConfigMapMountDefinition{ConfigMapName: name1, Key: "app.conf", Path: "/etc/b.conf", Mode: 0o600}) ||
			mounts[2] != (ConfigMapMountDefinition{ConfigMapName: name2, Key: "app.conf", Path: "/etc/c.conf", Mode: 0o644}) {
			subTest.Errorf("Deployment のマウントが違う: %+v", mounts)
		}
		if len(deploymentOperator.ensuredNamespaces) != 1 || deploymentOperator.ensuredNamespaces[0] != "project-1" {
			subTest.Errorf("namespace を用意するはず: %v", deploymentOperator.ensuredNamespaces)
		}
	})
	testingT.Run("全て外すときは、ConfigMap を作らず、Deployment からマウントが外れる", func(subTest *testing.T) {
		deploymentOperator := &fakeDeploymentOperator{}
		revisionOperator := &fakeRevisionOperator{}
		activities := newConfigMapMountsActivities(&fakeConfigMapMountsStore{deploy: newConfigMapMountsDeploy(pending, `[]`)}, newTestRevisionStore(), deploymentOperator, revisionOperator)
		if _, applyError := activities.ApplyPendingConfigMapMounts(context.Background(), workflow.ApplyPendingConfigMapMountsInput{DeployID: "deploy-1"}); applyError != nil {
			subTest.Fatal(applyError)
		}
		if revisionOperator.applyCalls != 0 || len(deploymentOperator.appliedDefinition.ConfigMapMounts) != 0 {
			subTest.Errorf("何も作らず、マウントが外れるはず: %d %+v", revisionOperator.applyCalls, deploymentOperator.appliedDefinition.ConfigMapMounts)
		}
	})
	testingT.Run("他の設定(環境変数など)は、現在の値を使う", func(subTest *testing.T) {
		deploymentOperator := &fakeDeploymentOperator{}
		deploy := newConfigMapMountsDeploy("", pending)
		deploy.PendingEnv = datatypes.JSON(`{"NEW":"1"}`)
		activities := newConfigMapMountsActivities(&fakeConfigMapMountsStore{deploy: deploy}, newTestRevisionStore(), deploymentOperator, &fakeRevisionOperator{})
		if _, applyError := activities.ApplyPendingConfigMapMounts(context.Background(), workflow.ApplyPendingConfigMapMountsInput{DeployID: "deploy-1"}); applyError != nil {
			subTest.Fatal(applyError)
		}
		if deploymentOperator.appliedDefinition.Env["PORT"] != "8080" || deploymentOperator.appliedDefinition.Env["NEW"] != "" {
			subTest.Errorf("現在の環境変数を使うはず: %v", deploymentOperator.appliedDefinition.Env)
		}
	})
	for name, testCase := range map[string]struct {
		store    *fakeConfigMapMountsStore
		revision *fakeRevisionStore
	}{
		"pending が無い":    {&fakeConfigMapMountsStore{deploy: newConfigMapMountsDeploy("[]", "")}, newTestRevisionStore()},
		"pending が壊れている": {&fakeConfigMapMountsStore{deploy: newConfigMapMountsDeploy("", `{broken`)}, newTestRevisionStore()},
		"版が DB に無い":      {&fakeConfigMapMountsStore{deploy: newConfigMapMountsDeploy("", pending)}, &fakeRevisionStore{revisions: map[string]model.ConfigMapRevision{}}},
		"版のファイルが壊れている":   {&fakeConfigMapMountsStore{deploy: newConfigMapMountsDeploy("", pending)}, &fakeRevisionStore{revisions: map[string]model.ConfigMapRevision{model.ConfigMapRevisionResourceName(testConfigMapID, 1): {Files: datatypes.JSON(`{x`)}}}},
		"バイナリの base64 が壊れている": {&fakeConfigMapMountsStore{deploy: newConfigMapMountsDeploy("", pending)}, &fakeRevisionStore{revisions: map[string]model.ConfigMapRevision{
			model.ConfigMapRevisionResourceName(testConfigMapID, 1): {Files: datatypes.JSON(`[{"key":"app.conf","encoding":"base64","content":"!!"}]`)},
			model.ConfigMapRevisionResourceName(testConfigMapID, 2): {Files: datatypes.JSON(`[]`)}}}},
	} {
		testingT.Run(name+"ときは、リトライしないエラー", func(subTest *testing.T) {
			activities := newConfigMapMountsActivities(testCase.store, testCase.revision, &fakeDeploymentOperator{}, &fakeRevisionOperator{})
			_, applyError := activities.ApplyPendingConfigMapMounts(context.Background(), workflow.ApplyPendingConfigMapMountsInput{DeployID: "deploy-1"})
			var applicationError *temporal.ApplicationError
			if !errors.As(applyError, &applicationError) || !applicationError.NonRetryable() {
				subTest.Errorf("リトライしないエラーのはず: %v", applyError)
			}
		})
	}
	testingT.Run("k8s への依頼の失敗は、Deployment を更新せずに、エラーになる", func(subTest *testing.T) {
		deploymentOperator := &fakeDeploymentOperator{}
		activities := newConfigMapMountsActivities(&fakeConfigMapMountsStore{deploy: newConfigMapMountsDeploy("", pending)}, newTestRevisionStore(), deploymentOperator, &fakeRevisionOperator{applyError: errors.New("k8s down")})
		if _, applyError := activities.ApplyPendingConfigMapMounts(context.Background(), workflow.ApplyPendingConfigMapMountsInput{DeployID: "deploy-1"}); applyError == nil {
			subTest.Error("エラーになるはず")
		}
		if deploymentOperator.appliedDefinition != nil {
			subTest.Error("ConfigMap を作れなければ、Deployment は更新しないはず")
		}
	})
}

// TestRollbackAndCommitConfigMapMounts は、元に戻すとき、現在のマウントが使われること、確定が結果を渡すことを確認する。
func TestRollbackAndCommitConfigMapMounts(testingT *testing.T) {
	current := `[{"configMapId":"` + testConfigMapID + `","revision":1,"key":"app.conf","path":"/etc/app.conf","mode":"0644"}]`
	pending := `[{"configMapId":"` + testConfigMapID + `","revision":2,"key":"app.conf","path":"/etc/app.conf","mode":"0644"}]`
	testingT.Run("元に戻すときは、現在の版のマウントで Deployment を更新する", func(subTest *testing.T) {
		deploymentOperator := &fakeDeploymentOperator{}
		activities := newConfigMapMountsActivities(&fakeConfigMapMountsStore{deploy: newConfigMapMountsDeploy(current, pending)}, newTestRevisionStore(), deploymentOperator, &fakeRevisionOperator{})
		if rollbackError := activities.RollbackDeploymentConfigMapMounts(context.Background(), workflow.RollbackDeploymentConfigMapMountsInput{DeployID: "deploy-1"}); rollbackError != nil {
			subTest.Fatal(rollbackError)
		}
		mounts := deploymentOperator.appliedDefinition.ConfigMapMounts
		if len(mounts) != 1 || mounts[0].ConfigMapName != model.ConfigMapRevisionResourceName(testConfigMapID, 1) {
			subTest.Errorf("現在の版 1 に戻るはず: %+v", mounts)
		}
	})
	testingT.Run("確定は、成否を保存先に渡す", func(subTest *testing.T) {
		store := &fakeConfigMapMountsStore{deploy: newConfigMapMountsDeploy(current, pending)}
		activities := newConfigMapMountsActivities(store, newTestRevisionStore(), &fakeDeploymentOperator{}, &fakeRevisionOperator{})
		activities.CommitConfigMapMountsResult(context.Background(), workflow.CommitConfigMapMountsResultInput{DeployID: "deploy-1", Succeeded: true})
		activities.CommitConfigMapMountsResult(context.Background(), workflow.CommitConfigMapMountsResultInput{DeployID: "deploy-1", Succeeded: false})
		if len(store.commitCalls) != 2 || !store.commitCalls[0] || store.commitCalls[1] {
			subTest.Errorf("成功、失敗の順に渡るはず: %v", store.commitCalls)
		}
	})
}

// TestCleanupConfigMapRevisions は、どの deploy も使っていない版だけを消すことを確認する。
func TestCleanupConfigMapRevisions(testingT *testing.T) {
	testingT.Run("使われていない版だけを削除する", func(subTest *testing.T) {
		used := model.ConfigMapRevisionResourceName(testConfigMapID, 2)
		unused := model.ConfigMapRevisionResourceName(testConfigMapID, 1)
		revisionOperator := &fakeRevisionOperator{existing: []string{unused, used}}
		activities := newConfigMapMountsActivities(&fakeConfigMapMountsStore{deploy: newConfigMapMountsDeploy("", "")}, &fakeRevisionStore{inUse: map[string]bool{used: true}}, &fakeDeploymentOperator{}, revisionOperator)
		if cleanupError := activities.CleanupConfigMapRevisions(context.Background(), workflow.CleanupConfigMapRevisionsInput{DeployID: "deploy-1"}); cleanupError != nil {
			subTest.Fatal(cleanupError)
		}
		if len(revisionOperator.deleted) != 1 || revisionOperator.deleted[0] != unused {
			subTest.Errorf("使われていない版だけが消えるはず: %v", revisionOperator.deleted)
		}
	})
}

// TestNewDeploymentDefinitionKeepsConfigMapMounts は、他の適用(起動設定など)でも、現在の ConfigMap のマウントが落ちないことを確認する。
func TestNewDeploymentDefinitionKeepsConfigMapMounts(testingT *testing.T) {
	deploy := newConfigMapMountsDeploy(`[{"configMapId":"`+testConfigMapID+`","revision":3,"key":"k","path":"/p","mode":"0640"}]`, "")
	definition, definitionError := newDeploymentDefinition(deploy, model.DeploySpec{Name: "small", CPUMillicores: 250, MemoryMiB: 256})
	if definitionError != nil || len(definition.ConfigMapMounts) != 1 || definition.ConfigMapMounts[0] != (ConfigMapMountDefinition{ConfigMapName: testConfigMapID + "-r3", Key: "k", Path: "/p", Mode: 0o640}) {
		testingT.Errorf("現在のマウントが入るはず: %+v %v", definition.ConfigMapMounts, definitionError)
	}
	// 壊れたマウントは、エラーにする。
	deploy.ConfigMapMounts = datatypes.JSON(`{broken`)
	if _, brokenError := newDeploymentDefinition(deploy, model.DeploySpec{}); brokenError == nil {
		testingT.Error("壊れたマウントはエラーになるはず")
	}
}
