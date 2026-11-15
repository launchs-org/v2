package cluster

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"launchs/worker/src/activity"
)

// testVolumeName は、テスト用の PVC の名前(volume の ID の形)。
const testVolumeName = "volume-0a1b2c3d-0000-4000-8000-000000000001"

// newTestVolumeOperator は、待つ間隔と時間を短くした KubernetesVolumeOperator を作る。
//
// 引数:
//   - fakeClientset: 偽の k8s のクライアント。
//   - settings: StorageClass とアクセスモード。
//
// 戻り値:
//   - 作成した KubernetesVolumeOperator。
func newTestVolumeOperator(fakeClientset *fake.Clientset, settings VolumeSettings) *KubernetesVolumeOperator {
	// 本物と同じ作り方で作って、待つ間隔と時間だけ短くする。
	operator := NewKubernetesVolumeOperator(fakeClientset, settings)
	operator.expandPollInterval = time.Millisecond
	operator.expandTimeout = 50 * time.Millisecond
	return operator
}

// TestKubernetesVolumeOperatorEnsure は、PVC を、大きさ・アクセスモード・StorageClass つきで作り、
// すでにあれば何も変えず、不正な名前を k8s に触れずに拒否することを確認する。
func TestKubernetesVolumeOperatorEnsure(testingT *testing.T) {
	ctx := context.Background()
	definition := activity.VolumeClaimDefinition{Namespace: validNamespaceName, Name: testVolumeName, SizeMB: 1024}

	testingT.Run("PVC を作り、大きさ(MiB)・アクセスモード・StorageClass が設定される", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		if ensureError := newTestVolumeOperator(fakeClientset, VolumeSettings{StorageClass: "longhorn", AccessMode: "ReadWriteMany"}).EnsureVolumeClaim(ctx, definition); ensureError != nil {
			subTest.Fatalf("作れるはず: %v", ensureError)
		}
		claim, _ := fakeClientset.CoreV1().PersistentVolumeClaims(validNamespaceName).Get(ctx, testVolumeName, metav1.GetOptions{})
		storage := claim.Spec.Resources.Requests[corev1.ResourceStorage]
		if storage.Value() != 1024*1024*1024 || claim.Spec.AccessModes[0] != corev1.ReadWriteMany || claim.Spec.StorageClassName == nil || *claim.Spec.StorageClassName != "longhorn" {
			subTest.Errorf("内容が違う: %+v", claim.Spec)
		}
		if claim.Labels[projectIDLabel] != validNamespaceName {
			subTest.Errorf("プロジェクトのラベルが付くはず: %v", claim.Labels)
		}
	})
	testingT.Run("StorageClass が空なら指定せず(既定を使う)、アクセスモードの既定は ReadWriteOnce", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		_ = newTestVolumeOperator(fakeClientset, VolumeSettings{}).EnsureVolumeClaim(ctx, definition)
		claim, _ := fakeClientset.CoreV1().PersistentVolumeClaims(validNamespaceName).Get(ctx, testVolumeName, metav1.GetOptions{})
		if claim.Spec.StorageClassName != nil || claim.Spec.AccessModes[0] != corev1.ReadWriteOnce {
			subTest.Errorf("既定のはず: %+v", claim.Spec)
		}
	})
	testingT.Run("すでにあれば、成功にして、大きさは変えない", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		operator := newTestVolumeOperator(fakeClientset, VolumeSettings{})
		_ = operator.EnsureVolumeClaim(ctx, definition)
		bigger := definition
		bigger.SizeMB = 4096
		if ensureError := operator.EnsureVolumeClaim(ctx, bigger); ensureError != nil {
			subTest.Fatalf("成功するはず: %v", ensureError)
		}
		claim, _ := fakeClientset.CoreV1().PersistentVolumeClaims(validNamespaceName).Get(ctx, testVolumeName, metav1.GetOptions{})
		if storage := claim.Spec.Resources.Requests[corev1.ResourceStorage]; storage.Value() != 1024*1024*1024 {
			subTest.Errorf("大きさは変わらないはず: %v", storage.Value())
		}
	})
	testingT.Run("不正な namespace・名前は、k8s に触れずに拒否する。k8s のエラーは包んで返す", func(subTest *testing.T) {
		for name, mutate := range map[string]func(*activity.VolumeClaimDefinition){
			"namespace": func(changed *activity.VolumeClaimDefinition) { changed.Namespace = "kube-system" },
			"名前":        func(changed *activity.VolumeClaimDefinition) { changed.Name = "evil" },
		} {
			fakeClientset := fake.NewSimpleClientset()
			changed := definition
			mutate(&changed)
			if ensureError := newTestVolumeOperator(fakeClientset, VolumeSettings{}).EnsureVolumeClaim(ctx, changed); ensureError == nil || len(fakeClientset.Actions()) != 0 {
				subTest.Errorf("%s: 拒否して、k8s に触れないはず: %v", name, ensureError)
			}
		}
		invalid := definition
		invalid.Name = "evil"
		if ensureError := newTestVolumeOperator(fake.NewSimpleClientset(), VolumeSettings{}).EnsureVolumeClaim(ctx, invalid); !errors.Is(ensureError, activity.ErrInvalidVolumeName) {
			subTest.Errorf("ErrInvalidVolumeName のはず: %v", ensureError)
		}
		failing := fake.NewSimpleClientset()
		failing.PrependReactor("create", "persistentvolumeclaims", func(clienttesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("api down") })
		if ensureError := newTestVolumeOperator(failing, VolumeSettings{}).EnsureVolumeClaim(ctx, definition); ensureError == nil {
			subTest.Error("エラーになるはず")
		}
	})
}

// newExistingClaim は、要求が requestedMB、実際の容量が capacityMB の PVC を作る。
//
// 引数:
//   - requestedMB: 要求の大きさ(MB)。
//   - capacityMB: 実際の容量(MB)。
//
// 戻り値:
//   - PVC。
func newExistingClaim(requestedMB int, capacityMB int) *corev1.PersistentVolumeClaim {
	// 要求と、実際の容量を設定する。
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: testVolumeName, Namespace: validNamespaceName},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: storageQuantity(requestedMB)}},
		},
		Status: corev1.PersistentVolumeClaimStatus{Capacity: corev1.ResourceList{corev1.ResourceStorage: storageQuantity(capacityMB)}},
	}
}

// TestKubernetesVolumeOperatorExpand は、PVC の拡張(要求を広げ、容量が増えるのを待つ)と、
// 無い PVC・縮小・時間切れ・不正な名前の扱いを確認する。
func TestKubernetesVolumeOperatorExpand(testingT *testing.T) {
	ctx := context.Background()
	definition := activity.VolumeClaimDefinition{Namespace: validNamespaceName, Name: testVolumeName, SizeMB: 2048}
	heartbeats := 0
	heartbeat := func() { heartbeats++ }

	testingT.Run("要求を広げ、容量が増えたら成功する(待つ間、heartbeat を送る)", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset(newExistingClaim(1024, 1024))
		// 更新されたら、実際の容量も増えたことにする(Longhorn が拡張を終えた状態)。
		fakeClientset.PrependReactor("update", "persistentvolumeclaims", func(action clienttesting.Action) (bool, runtime.Object, error) {
			claim := action.(clienttesting.UpdateAction).GetObject().(*corev1.PersistentVolumeClaim)
			claim.Status.Capacity = corev1.ResourceList{corev1.ResourceStorage: claim.Spec.Resources.Requests[corev1.ResourceStorage]}
			return false, claim, nil
		})
		heartbeats = 0
		if expandError := newTestVolumeOperator(fakeClientset, VolumeSettings{}).ExpandVolumeClaim(ctx, definition, heartbeat); expandError != nil {
			subTest.Fatalf("成功するはず: %v", expandError)
		}
		claim, _ := fakeClientset.CoreV1().PersistentVolumeClaims(validNamespaceName).Get(ctx, testVolumeName, metav1.GetOptions{})
		if storage := claim.Spec.Resources.Requests[corev1.ResourceStorage]; storage.Value() != 2048*1024*1024 || heartbeats == 0 {
			subTest.Errorf("広がり、heartbeat を送るはず: %v %d", storage.Value(), heartbeats)
		}
	})
	testingT.Run("PVC が無ければ(まだマウントされていない)、何もせず成功する", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		if expandError := newTestVolumeOperator(fakeClientset, VolumeSettings{}).ExpandVolumeClaim(ctx, definition, heartbeat); expandError != nil {
			subTest.Errorf("成功するはず: %v", expandError)
		}
		for _, action := range fakeClientset.Actions() {
			if action.GetVerb() != "get" {
				subTest.Errorf("読む以外は、しないはず: %v", action)
			}
		}
	})
	testingT.Run("すでにその大きさ以上なら、広げない(縮めない)。容量が足りていれば、すぐ成功する", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset(newExistingClaim(4096, 4096))
		if expandError := newTestVolumeOperator(fakeClientset, VolumeSettings{}).ExpandVolumeClaim(ctx, definition, heartbeat); expandError != nil {
			subTest.Fatalf("成功するはず: %v", expandError)
		}
		claim, _ := fakeClientset.CoreV1().PersistentVolumeClaims(validNamespaceName).Get(ctx, testVolumeName, metav1.GetOptions{})
		if storage := claim.Spec.Resources.Requests[corev1.ResourceStorage]; storage.Value() != 4096*1024*1024 {
			subTest.Errorf("縮めないはず: %v", storage.Value())
		}
	})
	testingT.Run("容量が増えないまま時間が過ぎたら、ErrVolumeExpandTimeout", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset(newExistingClaim(1024, 1024))
		if expandError := newTestVolumeOperator(fakeClientset, VolumeSettings{}).ExpandVolumeClaim(ctx, definition, heartbeat); !errors.Is(expandError, activity.ErrVolumeExpandTimeout) {
			subTest.Errorf("時間切れのはず: %v", expandError)
		}
	})
	testingT.Run("キャンセルされたら、待たずに終わる", func(subTest *testing.T) {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		operator := newTestVolumeOperator(fake.NewSimpleClientset(newExistingClaim(1024, 1024)), VolumeSettings{})
		operator.expandTimeout = time.Minute
		operator.expandPollInterval = time.Hour
		if expandError := operator.ExpandVolumeClaim(canceled, definition, heartbeat); expandError == nil {
			subTest.Error("エラーになるはず")
		}
	})
	testingT.Run("拡張を k8s が拒否したら(StorageClass が拡張に非対応など)、エラーを返す。不正な名前は k8s に触れず拒否", func(subTest *testing.T) {
		rejecting := fake.NewSimpleClientset(newExistingClaim(1024, 1024))
		rejecting.PrependReactor("update", "persistentvolumeclaims", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(corev1.Resource("persistentvolumeclaims"), testVolumeName, errors.New("only dynamically provisioned pvc can be resized"))
		})
		if expandError := newTestVolumeOperator(rejecting, VolumeSettings{}).ExpandVolumeClaim(ctx, definition, heartbeat); expandError == nil {
			subTest.Error("エラーになるはず")
		}
		untouched := fake.NewSimpleClientset()
		invalid := definition
		invalid.Namespace = "default"
		if expandError := newTestVolumeOperator(untouched, VolumeSettings{}).ExpandVolumeClaim(ctx, invalid, heartbeat); expandError == nil || len(untouched.Actions()) != 0 {
			subTest.Errorf("拒否して、k8s に触れないはず: %v", expandError)
		}
	})
}

// TestKubernetesVolumeOperatorDelete は、PVC の削除(冪等、不正な名前の拒否)を確認する。
func TestKubernetesVolumeOperatorDelete(testingT *testing.T) {
	ctx := context.Background()
	fakeClientset := fake.NewSimpleClientset(newExistingClaim(1024, 1024))
	operator := newTestVolumeOperator(fakeClientset, VolumeSettings{})
	// 削除でき、もう一度削除しても成功する。
	if deleteError := operator.DeleteVolumeClaim(ctx, validNamespaceName, testVolumeName); deleteError != nil {
		testingT.Fatalf("削除できるはず: %v", deleteError)
	}
	if _, getError := fakeClientset.CoreV1().PersistentVolumeClaims(validNamespaceName).Get(ctx, testVolumeName, metav1.GetOptions{}); !apierrors.IsNotFound(getError) {
		testingT.Errorf("消えているはず: %v", getError)
	}
	if again := operator.DeleteVolumeClaim(ctx, validNamespaceName, testVolumeName); again != nil {
		testingT.Errorf("無くても成功するはず: %v", again)
	}
	// 不正な namespace と名前は、拒否する。
	if operator.DeleteVolumeClaim(ctx, "default", testVolumeName) == nil || operator.DeleteVolumeClaim(ctx, validNamespaceName, "x") == nil {
		testingT.Error("不正な値は拒否するはず")
	}
}

// TestKubernetesDeploymentOperatorMounts は、マウントを持つ Deployment に、PVC のボリュームとマウントが入り、
// 更新方式が Recreate になる(マウントが無ければ、既定のまま)ことを確認する。
func TestKubernetesDeploymentOperatorMounts(testingT *testing.T) {
	ctx := context.Background()
	fakeClientset := fake.NewSimpleClientset()
	operator := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{})
	definition := newTestDefinition()
	definition.Mounts = []activity.MountDefinition{{ClaimName: testVolumeName, MountPath: "/data"}}
	if _, applyError := operator.ApplyDeployment(ctx, definition); applyError != nil {
		testingT.Fatalf("作れるはず: %v", applyError)
	}
	deployment, _ := fakeClientset.AppsV1().Deployments(definition.Namespace).Get(ctx, definition.Name, metav1.GetOptions{})
	podSpec := deployment.Spec.Template.Spec
	if len(podSpec.Volumes) != 1 || podSpec.Volumes[0].PersistentVolumeClaim == nil || podSpec.Volumes[0].PersistentVolumeClaim.ClaimName != testVolumeName ||
		len(podSpec.Containers[0].VolumeMounts) != 1 || podSpec.Containers[0].VolumeMounts[0].MountPath != "/data" || podSpec.Containers[0].VolumeMounts[0].Name != podSpec.Volumes[0].Name {
		testingT.Errorf("ボリュームとマウントが入るはず: %+v", podSpec)
	}
	if deployment.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		testingT.Errorf("Recreate のはず: %q", deployment.Spec.Strategy.Type)
	}
	// マウントを外すと、ボリュームも更新方式も、なくなる。
	definition.Mounts = nil
	if _, applyError := operator.ApplyDeployment(ctx, definition); applyError != nil {
		testingT.Fatalf("更新できるはず: %v", applyError)
	}
	updated, _ := fakeClientset.AppsV1().Deployments(definition.Namespace).Get(ctx, definition.Name, metav1.GetOptions{})
	if len(updated.Spec.Template.Spec.Volumes) != 0 || updated.Spec.Strategy.Type != "" {
		testingT.Errorf("ボリュームも Recreate も無くなるはず: %+v", updated.Spec)
	}
}
