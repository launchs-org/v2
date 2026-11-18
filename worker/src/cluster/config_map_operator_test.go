package cluster

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"launchs/worker/src/activity"
)

// TestKubernetesConfigMapOperator は、ConfigMap を作れ、すでにあれば内容を置き換え(消えたキーは無くなる)、
// 削除でき(無くても成功)、deploy の ID の形でない名前は拒否することを確認する。
func TestKubernetesConfigMapOperator(testingT *testing.T) {
	// fake のクライアントと、役を用意する。
	fakeClientset := fake.NewSimpleClientset()
	operator := NewKubernetesConfigMapOperator(fakeClientset)
	configMaps := fakeClientset.CoreV1().ConfigMaps(validNamespaceName)

	testingT.Run("無ければ作る。deploy とプロジェクトのラベルが付く", func(subTest *testing.T) {
		if applyError := operator.ApplyConfigMap(context.Background(), validNamespaceName, testDeploymentName, map[string]string{"f0": "A", "f1": "B"}); applyError != nil {
			subTest.Fatal(applyError)
		}
		created, getError := configMaps.Get(context.Background(), testDeploymentName, metav1.GetOptions{})
		if getError != nil || created.Data["f0"] != "A" || created.Data["f1"] != "B" || created.Labels[deployIDLabel] != testDeploymentName || created.Labels[projectIDLabel] != validNamespaceName {
			subTest.Errorf("作られているはず: %+v %v", created, getError)
		}
	})
	testingT.Run("すでにあれば、置き換える(消えたキーは無くなる)", func(subTest *testing.T) {
		if applyError := operator.ApplyConfigMap(context.Background(), validNamespaceName, testDeploymentName, map[string]string{"f0": "A2"}); applyError != nil {
			subTest.Fatal(applyError)
		}
		updated, _ := configMaps.Get(context.Background(), testDeploymentName, metav1.GetOptions{})
		if updated.Data["f0"] != "A2" || len(updated.Data) != 1 {
			subTest.Errorf("置き換わるはず: %v", updated.Data)
		}
	})
	testingT.Run("deploy の ID の形でない名前は、作らず、消さない", func(subTest *testing.T) {
		if operator.ApplyConfigMap(context.Background(), validNamespaceName, "kube-root-ca.crt", map[string]string{"a": "b"}) == nil {
			subTest.Error("作成が拒否されるはず")
		}
		if operator.DeleteConfigMap(context.Background(), validNamespaceName, "kube-root-ca.crt") == nil {
			subTest.Error("削除が拒否されるはず")
		}
	})
	testingT.Run("削除でき、無くても成功する(冪等)", func(subTest *testing.T) {
		if deleteError := operator.DeleteConfigMap(context.Background(), validNamespaceName, testDeploymentName); deleteError != nil {
			subTest.Fatal(deleteError)
		}
		if _, getError := configMaps.Get(context.Background(), testDeploymentName, metav1.GetOptions{}); !apierrors.IsNotFound(getError) {
			subTest.Errorf("消えているはず: %v", getError)
		}
		if deleteError := operator.DeleteConfigMap(context.Background(), validNamespaceName, testDeploymentName); deleteError != nil {
			subTest.Errorf("2 回目も成功するはず: %v", deleteError)
		}
	})
}

// TestBuildDeploymentWithFiles は、Deployment の Pod に、ファイルが、ConfigMap の subPath のマウントで付き、
// ボリューム(PVC)のマウントと共存し、更新の方式が Recreate になることを確認する。
func TestBuildDeploymentWithFiles(testingT *testing.T) {
	operator := NewKubernetesDeploymentOperator(fake.NewSimpleClientset(), OperatorSettings{})
	definition := newTestDefinition()
	definition.Mounts = []activity.MountDefinition{{ClaimName: "volume-aaaa", MountPath: "/data"}}
	definition.Files = []activity.FileDefinition{{Key: "f0", Path: "/data/init.sql"}, {Key: "f1", Path: "/etc/app/app.conf"}}
	deployment := operator.buildDeployment(definition)

	testingT.Run("ファイルごとに、subPath のマウントが付き、読み取り専用", func(subTest *testing.T) {
		mounts := deployment.Spec.Template.Spec.Containers[0].VolumeMounts
		// PVC のマウントが 1 つ、ファイルのマウントが 2 つ。
		if len(mounts) != 3 {
			subTest.Fatalf("マウントは 3 つのはず: %+v", mounts)
		}
		fileMount := mounts[1]
		if fileMount.Name != "files" || fileMount.MountPath != "/data/init.sql" || fileMount.SubPath != "f0" || !fileMount.ReadOnly {
			subTest.Errorf("ファイルのマウントが違う: %+v", fileMount)
		}
		// PVC のマウントは、そのまま(同じ Pod で、共存する)。
		if mounts[0].Name != "volume-aaaa" || mounts[0].MountPath != "/data" || mounts[0].SubPath != "" {
			subTest.Errorf("PVC のマウントは、そのままのはず: %+v", mounts[0])
		}
	})
	testingT.Run("ConfigMap のボリュームは、deploy の ID の名前で、キーごとの items を持つ", func(subTest *testing.T) {
		volumes := deployment.Spec.Template.Spec.Volumes
		if len(volumes) != 2 || volumes[1].Name != "files" || volumes[1].ConfigMap == nil || volumes[1].ConfigMap.Name != testDeploymentName || len(volumes[1].ConfigMap.Items) != 2 {
			subTest.Fatalf("ConfigMap のボリュームが違う: %+v", volumes)
		}
		if volumes[1].ConfigMap.Items[0].Key != "f0" || volumes[1].ConfigMap.Items[0].Path != "f0" || *volumes[1].ConfigMap.DefaultMode != 0o644 {
			subTest.Errorf("items が違う: %+v", volumes[1].ConfigMap)
		}
	})
	testingT.Run("ファイルだけ(ボリュームなし)でも、Recreate になる", func(subTest *testing.T) {
		onlyFiles := newTestDefinition()
		onlyFiles.Files = []activity.FileDefinition{{Key: "f0", Path: "/etc/a"}}
		if built := operator.buildDeployment(onlyFiles); built.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
			subTest.Errorf("Recreate のはず: %v", built.Spec.Strategy.Type)
		}
	})
	testingT.Run("ファイルが無ければ、files のボリュームは付かない", func(subTest *testing.T) {
		plain := operator.buildDeployment(newTestDefinition())
		if len(plain.Spec.Template.Spec.Volumes) != 0 || plain.Spec.Strategy.Type == appsv1.RecreateDeploymentStrategyType {
			subTest.Errorf("何も付かないはず: %+v %v", plain.Spec.Template.Spec.Volumes, plain.Spec.Strategy.Type)
		}
	})
}

// TestKubernetesDeploymentOperatorDeleteRemovesConfigMap は、deploy の Deployment を消すとき、同じ名前の ConfigMap も消えることを確認する。
func TestKubernetesDeploymentOperatorDeleteRemovesConfigMap(testingT *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	operator := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{})
	// Deployment の有無にかかわらず、ConfigMap は、消える。
	if applyError := NewKubernetesConfigMapOperator(fakeClientset).ApplyConfigMap(context.Background(), validNamespaceName, testDeploymentName, map[string]string{"f0": "A"}); applyError != nil {
		testingT.Fatal(applyError)
	}
	if deleteError := operator.DeleteDeployment(context.Background(), validNamespaceName, testDeploymentName); deleteError != nil {
		testingT.Fatal(deleteError)
	}
	if _, getError := fakeClientset.CoreV1().ConfigMaps(validNamespaceName).Get(context.Background(), testDeploymentName, metav1.GetOptions{}); !apierrors.IsNotFound(getError) {
		testingT.Errorf("ConfigMap も消えているはず: %v", getError)
	}
}
