package cluster

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"launchs/worker/src/activity"
)

// testRevisionConfigMapName は、テスト用の、版の ConfigMap の名前。
const testRevisionConfigMapName = "configmap-0a1b2c3d-0000-4000-8000-000000000001-r3"

// TestKubernetesConfigMapRevisionOperator は、版の ConfigMap を immutable で作り(テキストとバイナリ)、すでにあれば何もせず、
// ラベルで一覧でき、削除でき(無くても成功)、版の形でない名前は拒否することを確認する。
func TestKubernetesConfigMapRevisionOperator(testingT *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	operator := NewKubernetesConfigMapRevisionOperator(fakeClientset)
	configMaps := fakeClientset.CoreV1().ConfigMaps(validNamespaceName)

	testingT.Run("immutable で作る。テキストは data、バイナリは binaryData。ラベルが付く", func(subTest *testing.T) {
		if applyError := operator.ApplyRevisionConfigMap(context.Background(), validNamespaceName, testRevisionConfigMapName, map[string]string{"a": "1"}, map[string][]byte{"b": {0, 1}}); applyError != nil {
			subTest.Fatal(applyError)
		}
		created, getError := configMaps.Get(context.Background(), testRevisionConfigMapName, metav1.GetOptions{})
		if getError != nil || created.Immutable == nil || !*created.Immutable || created.Data["a"] != "1" || len(created.BinaryData["b"]) != 2 ||
			created.Labels[configMapRevisionLabel] != "true" || created.Labels[projectIDLabel] != validNamespaceName {
			subTest.Errorf("作られているはず: %+v %v", created, getError)
		}
	})
	testingT.Run("すでにあれば、何もしない(中身は変わらない)", func(subTest *testing.T) {
		if applyError := operator.ApplyRevisionConfigMap(context.Background(), validNamespaceName, testRevisionConfigMapName, map[string]string{"a": "changed"}, nil); applyError != nil {
			subTest.Fatal(applyError)
		}
		existing, _ := configMaps.Get(context.Background(), testRevisionConfigMapName, metav1.GetOptions{})
		if existing.Data["a"] != "1" {
			subTest.Errorf("変わらないはず: %v", existing.Data)
		}
	})
	testingT.Run("一覧は、ラベルの付いた版の ConfigMap だけ(deploy のファイルの ConfigMap は含まない)", func(subTest *testing.T) {
		if applyError := NewKubernetesConfigMapOperator(fakeClientset).ApplyConfigMap(context.Background(), validNamespaceName, testDeploymentName, map[string]string{"f0": "x"}); applyError != nil {
			subTest.Fatal(applyError)
		}
		names, listError := operator.ListRevisionConfigMaps(context.Background(), validNamespaceName)
		if listError != nil || len(names) != 1 || names[0] != testRevisionConfigMapName {
			subTest.Errorf("版の ConfigMap だけのはず: %v %v", names, listError)
		}
	})
	testingT.Run("版の形でない名前は、作らず、消さない", func(subTest *testing.T) {
		if operator.ApplyRevisionConfigMap(context.Background(), validNamespaceName, "kube-root-ca.crt", nil, nil) == nil {
			subTest.Error("作成が拒否されるはず")
		}
		if operator.DeleteRevisionConfigMap(context.Background(), validNamespaceName, testDeploymentName) == nil {
			subTest.Error("deploy のファイルの ConfigMap の削除が拒否されるはず")
		}
	})
	testingT.Run("削除でき、無くても成功する(冪等)", func(subTest *testing.T) {
		if deleteError := operator.DeleteRevisionConfigMap(context.Background(), validNamespaceName, testRevisionConfigMapName); deleteError != nil {
			subTest.Fatal(deleteError)
		}
		if deleteError := operator.DeleteRevisionConfigMap(context.Background(), validNamespaceName, testRevisionConfigMapName); deleteError != nil {
			subTest.Errorf("無くても成功するはず: %v", deleteError)
		}
	})
}

// TestKubernetesDeploymentOperatorConfigMapMounts は、ConfigMap のマウントが、マウントごとのボリューム(mode つき)と subPath のマウントになり、
// Recreate になり、ボリュームやファイルと共存することを確認する。
func TestKubernetesDeploymentOperatorConfigMapMounts(testingT *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	definition := newTestDefinition()
	definition.Mounts = []activity.MountDefinition{{ClaimName: "volume-1", MountPath: "/data"}}
	definition.Files = []activity.FileDefinition{{Key: "f0", Path: "/etc/file.conf"}}
	definition.ConfigMapMounts = []activity.ConfigMapMountDefinition{
		{ConfigMapName: testRevisionConfigMapName, Key: "app.conf", Path: "/etc/a.conf", Mode: 0o600},
		{ConfigMapName: testRevisionConfigMapName, Key: "app.conf", Path: "/etc/b.conf", Mode: 0o755},
	}
	if _, applyError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).ApplyDeployment(context.Background(), definition); applyError != nil {
		testingT.Fatal(applyError)
	}
	deployment, _ := fakeClientset.AppsV1().Deployments(validNamespaceName).Get(context.Background(), testDeploymentName, metav1.GetOptions{})
	podSpec := deployment.Spec.Template.Spec
	// ボリュームは、PVC、files、cm0、cm1 の 4 つ。cm0 と cm1 は、同じ ConfigMap で、mode が別。
	volumes := map[string]int32{}
	for _, volume := range podSpec.Volumes {
		if volume.ConfigMap != nil && volume.ConfigMap.Name == testRevisionConfigMapName {
			volumes[volume.Name] = *volume.ConfigMap.Items[0].Mode
			if volume.ConfigMap.Items[0].Key != "app.conf" || volume.ConfigMap.Items[0].Path != "app.conf" {
				testingT.Errorf("items の key と path が違う: %+v", volume.ConfigMap.Items)
			}
		}
	}
	if len(podSpec.Volumes) != 4 || volumes["cm0"] != 0o600 || volumes["cm1"] != 0o755 {
		testingT.Errorf("マウントごとに、mode の違うボリュームができるはず: %+v", podSpec.Volumes)
	}
	// コンテナのマウントは、subPath で、読み取り専用。
	found := map[string]bool{}
	for _, mount := range podSpec.Containers[0].VolumeMounts {
		if mount.Name == "cm0" && mount.MountPath == "/etc/a.conf" && mount.SubPath == "app.conf" && mount.ReadOnly {
			found["a"] = true
		}
		if mount.Name == "cm1" && mount.MountPath == "/etc/b.conf" && mount.SubPath == "app.conf" && mount.ReadOnly {
			found["b"] = true
		}
	}
	if !found["a"] || !found["b"] || len(podSpec.Containers[0].VolumeMounts) != 4 {
		testingT.Errorf("subPath のマウントが 2 つ増えるはず: %+v", podSpec.Containers[0].VolumeMounts)
	}
	if deployment.Spec.Strategy.Type != "Recreate" {
		testingT.Errorf("Recreate のはず: %s", deployment.Spec.Strategy.Type)
	}
	// ConfigMap のマウントだけでも、Recreate になる。
	only := newTestDefinition()
	only.Name = "deploy-0a1b2c3d-0000-4000-8000-000000000002"
	only.ConfigMapMounts = definition.ConfigMapMounts[:1]
	NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).ApplyDeployment(context.Background(), only)
	second, _ := fakeClientset.AppsV1().Deployments(validNamespaceName).Get(context.Background(), only.Name, metav1.GetOptions{})
	if second.Spec.Strategy.Type != "Recreate" {
		testingT.Errorf("ConfigMap のマウントだけでも Recreate のはず: %s", second.Spec.Strategy.Type)
	}
}
