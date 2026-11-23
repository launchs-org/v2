package cluster

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"launchs/worker/src/activity"
)

// testDeploymentName は、テスト用の Deployment の名前(deploy の ID の形)。
const testDeploymentName = "deploy-0a1b2c3d-0000-4000-8000-000000000001"

// newTestDefinition は、テスト用の Deployment の内容を作る。
//
// 戻り値:
//   - small 相当の大きさの内容(namespace は validNamespaceName)。
func newTestDefinition() activity.DeploymentDefinition {
	return activity.DeploymentDefinition{
		Namespace:     validNamespaceName,
		Name:          testDeploymentName,
		Image:         "registry.example/app:1",
		Replicas:      2,
		CPUMillicores: 250,
		MemoryMiB:     256,
		Env:           map[string]string{"PORT": "8080", "A_FIRST": "1"},
	}
}

// TestKubernetesDeploymentOperatorEnsureNamespace は、namespace を作れ、すでにあっても成功し、
// プロジェクトの形でない名前は拒否され、k8s の障害はエラーになることを確認する。
func TestKubernetesDeploymentOperatorEnsureNamespace(testingT *testing.T) {
	// 無い・すでにある・不正な名前・k8s の障害の場合。
	testCases := []struct {
		name               string
		existing           []runtime.Object
		namespaceName      string
		reactorError       error
		expectError        bool
		expectInvalidName  bool
		expectNamespaceNow bool
	}{
		{name: "無ければ作る", namespaceName: validNamespaceName, expectNamespaceNow: true},
		{name: "すでにあっても成功する(冪等)", existing: []runtime.Object{newNamespace(validNamespaceName)}, namespaceName: validNamespaceName, expectNamespaceNow: true},
		{name: "プロジェクトの形でない名前は拒否する", namespaceName: "kube-system", expectError: true, expectInvalidName: true},
		{name: "k8s の障害はエラーになる", namespaceName: validNamespaceName, reactorError: apierrors.NewInternalError(errors.New("boom")), expectError: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// fake のクライアントを作り、必要なら、作成の操作が決めたエラーを返すようにする。
			fakeClientset := fake.NewSimpleClientset(testCase.existing...)
			if testCase.reactorError != nil {
				fakeClientset.PrependReactor("create", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, testCase.reactorError
				})
			}

			// namespace の用意を頼む。
			ensureError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).EnsureNamespace(context.Background(), testCase.namespaceName)

			// エラーの有無、不正な名前のエラーかどうか、namespace があるかを確認する。
			if (ensureError != nil) != testCase.expectError {
				subTest.Fatalf("エラーの有無が違う: %v", ensureError)
			}
			if errors.Is(ensureError, activity.ErrInvalidNamespaceName) != testCase.expectInvalidName {
				subTest.Errorf("不正な名前のエラーかどうかが違う: %v", ensureError)
			}
			_, getError := fakeClientset.CoreV1().Namespaces().Get(context.Background(), testCase.namespaceName, metav1.GetOptions{})
			if (getError == nil) != testCase.expectNamespaceNow {
				subTest.Errorf("namespace の有無が違う: %v", getError)
			}
		})
	}
}

// TestKubernetesDeploymentOperatorApplyDeployment は、Deployment を、スペックの大きさ・環境変数・
// ラベル・安全の設定つきで作り、すでにあれば更新し、不正な namespace には何も作らないことを確認する。
func TestKubernetesDeploymentOperatorApplyDeployment(testingT *testing.T) {
	// 作った Deployment の中身を確認する。
	testingT.Run("内容どおりの Deployment を作る", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		definition := newTestDefinition()
		command := "/app/server"
		definition.Command = &command
		definition.Args = []string{"--config", "/app/config.yaml"}

		_, applyError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{RuntimeClass: "gvisor"}).ApplyDeployment(context.Background(), definition)
		if applyError != nil {
			subTest.Fatalf("エラーにならないはず: %v", applyError)
		}

		deployment, getError := fakeClientset.AppsV1().Deployments(validNamespaceName).Get(context.Background(), testDeploymentName, metav1.GetOptions{})
		if getError != nil {
			subTest.Fatalf("Deployment が作られているはず: %v", getError)
		}
		// レプリカ数、ラベル、selector。
		if *deployment.Spec.Replicas != 2 || deployment.Labels[deployIDLabel] != testDeploymentName || deployment.Labels[projectIDLabel] != validNamespaceName {
			subTest.Errorf("レプリカ数かラベルが違う: %+v", deployment.ObjectMeta)
		}
		if deployment.Spec.Selector.MatchLabels[deployIDLabel] != testDeploymentName || deployment.Spec.Template.Labels[deployIDLabel] != testDeploymentName {
			subTest.Errorf("selector と Pod のラベルが結びついていない")
		}
		// Pod には Datadog の差し込みを無効にするラベルが付き、Deployment 本体と selector には付かない。
		if deployment.Spec.Template.Labels[datadogInjectionLabel] != "false" || deployment.Labels[datadogInjectionLabel] != "" || deployment.Spec.Selector.MatchLabels[datadogInjectionLabel] != "" {
			subTest.Errorf("Datadog の差し込みを無効にするラベルが、Pod だけに付くはず: %+v", deployment.Spec.Template.Labels)
		}
		// コンテナの内容。
		podSpec := deployment.Spec.Template.Spec
		container := podSpec.Containers[0]
		if container.Name != "app" || container.Image != "registry.example/app:1" {
			subTest.Errorf("コンテナが違う: %+v", container)
		}
		if len(container.Command) != 1 || container.Command[0] != "/app/server" || len(container.Args) != 2 || container.Args[0] != "--config" {
			subTest.Errorf("コマンドと引数が違う: %v %v", container.Command, container.Args)
		}
		// 環境変数は、キーの順(A_FIRST が先)。
		if len(container.Env) != 2 || container.Env[0].Name != "A_FIRST" || container.Env[1].Name != "PORT" || container.Env[1].Value != "8080" {
			subTest.Errorf("環境変数が違う: %v", container.Env)
		}
		// CPU とメモリは、requests と limits が同じ(250m、256Mi)。
		if container.Resources.Limits.Cpu().MilliValue() != 250 || container.Resources.Requests.Cpu().MilliValue() != 250 ||
			container.Resources.Limits.Memory().Value() != 256*1024*1024 || container.Resources.Requests.Memory().Value() != 256*1024*1024 {
			subTest.Errorf("大きさが違う: %+v", container.Resources)
		}
		// RuntimeClass と、安全の設定。
		if podSpec.RuntimeClassName == nil || *podSpec.RuntimeClassName != "gvisor" {
			subTest.Errorf("RuntimeClass が指定されていない: %v", podSpec.RuntimeClassName)
		}
		if podSpec.AutomountServiceAccountToken == nil || *podSpec.AutomountServiceAccountToken || podSpec.EnableServiceLinks == nil || *podSpec.EnableServiceLinks {
			subTest.Errorf("ServiceAccount のトークンと Service 用の環境変数は、渡さない設定のはず")
		}
	})

	// 指定が無いものは、k8s に任せる(上書きしない)。
	testingT.Run("RuntimeClass・コマンド・引数が未指定なら、指定しない", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		if _, applyError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).ApplyDeployment(context.Background(), newTestDefinition()); applyError != nil {
			subTest.Fatalf("エラーにならないはず: %v", applyError)
		}
		deployment, _ := fakeClientset.AppsV1().Deployments(validNamespaceName).Get(context.Background(), testDeploymentName, metav1.GetOptions{})
		podSpec := deployment.Spec.Template.Spec
		if podSpec.RuntimeClassName != nil || podSpec.Containers[0].Command != nil || podSpec.Containers[0].Args != nil {
			subTest.Errorf("指定しないはず: %v %v %v", podSpec.RuntimeClassName, podSpec.Containers[0].Command, podSpec.Containers[0].Args)
		}
	})

	// すでにある Deployment は、更新される(冪等)。
	testingT.Run("すでにあれば、内容を更新する", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		operator := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{})
		if _, applyError := operator.ApplyDeployment(context.Background(), newTestDefinition()); applyError != nil {
			subTest.Fatalf("1回目: %v", applyError)
		}
		changedDefinition := newTestDefinition()
		changedDefinition.Image = "registry.example/app:2"
		changedDefinition.Replicas = 3
		if _, applyError := operator.ApplyDeployment(context.Background(), changedDefinition); applyError != nil {
			subTest.Fatalf("2回目: %v", applyError)
		}
		deployment, _ := fakeClientset.AppsV1().Deployments(validNamespaceName).Get(context.Background(), testDeploymentName, metav1.GetOptions{})
		if deployment.Spec.Template.Spec.Containers[0].Image != "registry.example/app:2" || *deployment.Spec.Replicas != 3 {
			subTest.Errorf("更新されているはず: %+v", deployment.Spec)
		}
	})

	// プロジェクトの形でない namespace には、何も作らない。
	testingT.Run("プロジェクトの形でない namespace には作らない", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		definition := newTestDefinition()
		definition.Namespace = "kube-system"

		_, applyError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).ApplyDeployment(context.Background(), definition)

		if !errors.Is(applyError, activity.ErrInvalidNamespaceName) {
			subTest.Errorf("不正な名前のエラーのはず: %v", applyError)
		}
		if deployments, _ := fakeClientset.AppsV1().Deployments("kube-system").List(context.Background(), metav1.ListOptions{}); len(deployments.Items) != 0 {
			subTest.Errorf("何も作られていないはず")
		}
	})

	// k8s の障害は、エラーになる。
	testingT.Run("k8s の障害はエラーになる", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		fakeClientset.PrependReactor("create", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewInternalError(errors.New("boom"))
		})
		if _, applyError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).ApplyDeployment(context.Background(), newTestDefinition()); applyError == nil {
			subTest.Errorf("エラーになるはず")
		}
	})
}

// TestKubernetesDeploymentOperatorDeploymentReady は、Ready の判定(設定の処理済み、希望数の Pod が使える状態)を確認する。
func TestKubernetesDeploymentOperatorDeploymentReady(testingT *testing.T) {
	// 希望するレプリカ数が2の Deployment を作る関数。状態は、テストごとに決める。
	newDeployment := func(generation int64, status appsv1.DeploymentStatus, desiredReplicas int32) *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: testDeploymentName, Namespace: validNamespaceName, Generation: generation},
			Spec:       appsv1.DeploymentSpec{Replicas: &desiredReplicas},
			Status:     status,
		}
	}
	// Ready になる・ならない場合。
	testCases := []struct {
		name          string
		existing      []runtime.Object
		reactorError  error
		expectedReady bool
		expectError   bool
	}{
		{name: "Deployment がまだ無ければ、Ready ではない", expectedReady: false},
		{name: "希望数が全て使える状態なら Ready", existing: []runtime.Object{newDeployment(1, appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2}, 2)}, expectedReady: true},
		{name: "使える Pod が足りなければ Ready ではない", existing: []runtime.Object{newDeployment(1, appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 2, UpdatedReplicas: 2, AvailableReplicas: 1}, 2)}, expectedReady: false},
		{name: "古い設定の Pod が残っていれば Ready ではない", existing: []runtime.Object{newDeployment(2, appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 3, UpdatedReplicas: 2, AvailableReplicas: 3}, 2)}, expectedReady: false},
		{name: "最新の設定を k8s が処理する前は Ready ではない", existing: []runtime.Object{newDeployment(2, appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2}, 2)}, expectedReady: false},
		{name: "希望数が 0 で、Pod が無ければ Ready", existing: []runtime.Object{newDeployment(1, appsv1.DeploymentStatus{ObservedGeneration: 1}, 0)}, expectedReady: true},
		{name: "k8s の障害はエラーになる", reactorError: apierrors.NewInternalError(errors.New("boom")), expectError: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// fake のクライアントを作り、必要なら、取得の操作が決めたエラーを返すようにする。
			fakeClientset := fake.NewSimpleClientset(testCase.existing...)
			if testCase.reactorError != nil {
				fakeClientset.PrependReactor("get", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, testCase.reactorError
				})
			}

			// Ready かを確かめる。
			ready, readyError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).DeploymentReady(context.Background(), validNamespaceName, testDeploymentName)

			// 結果とエラーの有無を確認する。
			if ready != testCase.expectedReady || (readyError != nil) != testCase.expectError {
				subTest.Errorf("期待 %v(エラー %v)、実際 %v(エラー %v)", testCase.expectedReady, testCase.expectError, ready, readyError)
			}
		})
	}
	// プロジェクトの形でない namespace は、問い合わせない。
	testingT.Run("プロジェクトの形でない namespace は拒否する", func(subTest *testing.T) {
		_, readyError := NewKubernetesDeploymentOperator(fake.NewSimpleClientset(), OperatorSettings{}).DeploymentReady(context.Background(), "kube-system", "x")
		if !errors.Is(readyError, activity.ErrInvalidNamespaceName) {
			subTest.Errorf("不正な名前のエラーのはず: %v", readyError)
		}
	})
}

// TestKubernetesDeploymentOperatorDeleteDeployment は、Deployment を削除でき、無くても成功し、
// プロジェクトの namespace と deploy の名前の形でないものは、k8s に触れずに拒否することを確認する。
func TestKubernetesDeploymentOperatorDeleteDeployment(testingT *testing.T) {
	testingT.Run("Deployment を削除できる", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		operator := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{})
		if _, applyError := operator.ApplyDeployment(context.Background(), newTestDefinition()); applyError != nil {
			subTest.Fatalf("作成できるはず: %v", applyError)
		}

		if deleteError := operator.DeleteDeployment(context.Background(), validNamespaceName, testDeploymentName); deleteError != nil {
			subTest.Fatalf("削除できるはず: %v", deleteError)
		}

		if _, getError := fakeClientset.AppsV1().Deployments(validNamespaceName).Get(context.Background(), testDeploymentName, metav1.GetOptions{}); !apierrors.IsNotFound(getError) {
			subTest.Errorf("Deployment が消えているはず: %v", getError)
		}
	})
	testingT.Run("すでに無くても成功する(冪等)", func(subTest *testing.T) {
		operator := NewKubernetesDeploymentOperator(fake.NewSimpleClientset(), OperatorSettings{})
		if deleteError := operator.DeleteDeployment(context.Background(), validNamespaceName, testDeploymentName); deleteError != nil {
			subTest.Errorf("成功するはず: %v", deleteError)
		}
	})
	testingT.Run("k8s への依頼の失敗は、エラーとして返す", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		fakeClientset.PrependReactor("delete", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("api down")
		})
		if deleteError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).DeleteDeployment(context.Background(), validNamespaceName, testDeploymentName); deleteError == nil {
			subTest.Errorf("エラーのはず")
		}
	})
	// 不正な名前は、k8s に何も依頼しない。
	for _, invalidCase := range []struct {
		name       string
		namespace  string
		deployment string
		expected   error
	}{
		{"プロジェクトの形でない namespace(kube-system)は拒否する", "kube-system", testDeploymentName, activity.ErrInvalidNamespaceName},
		{"deploy の形でない名前(coredns)は拒否する", validNamespaceName, "coredns", activity.ErrInvalidDeploymentName},
		{"空の名前は拒否する", validNamespaceName, "", activity.ErrInvalidDeploymentName},
	} {
		testingT.Run(invalidCase.name, func(subTest *testing.T) {
			fakeClientset := fake.NewSimpleClientset()
			deleteError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).DeleteDeployment(context.Background(), invalidCase.namespace, invalidCase.deployment)
			if !errors.Is(deleteError, invalidCase.expected) || len(fakeClientset.Actions()) != 0 {
				subTest.Errorf("拒否され、k8s は呼ばれないはず: %v %v", deleteError, fakeClientset.Actions())
			}
		})
	}
}

// TestKubernetesDeploymentOperatorApplyDeploymentGeneration は、作成と更新のどちらでも、k8s が返した
// Deployment の設定の世代(generation)を返すことを確認する。
func TestKubernetesDeploymentOperatorApplyDeploymentGeneration(testingT *testing.T) {
	// 作成のとき: k8s が、世代 1 の Deployment を返す(fake は世代を付けないので、返すオブジェクトを差し替える)。
	testingT.Run("作成のときは、作った Deployment の世代を返す", func(subTest *testing.T) {
		fakeClientset := fake.NewSimpleClientset()
		fakeClientset.PrependReactor("create", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
			created := action.(clienttesting.CreateAction).GetObject().(*appsv1.Deployment).DeepCopy()
			created.Generation = 1
			return true, created, nil
		})
		generation, applyError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).ApplyDeployment(context.Background(), newTestDefinition())
		if applyError != nil || generation != 1 {
			subTest.Errorf("世代 1 のはず: %d %v", generation, applyError)
		}
	})
	// 更新のとき: すでに Deployment があり、k8s が、世代 5 の Deployment を返す。
	testingT.Run("更新のときは、更新後の Deployment の世代を返す", func(subTest *testing.T) {
		existing := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: testDeploymentName, Namespace: validNamespaceName, Generation: 4}}
		fakeClientset := fake.NewSimpleClientset(existing)
		fakeClientset.PrependReactor("update", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
			updated := action.(clienttesting.UpdateAction).GetObject().(*appsv1.Deployment).DeepCopy()
			updated.Generation = 5
			return true, updated, nil
		})
		generation, applyError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).ApplyDeployment(context.Background(), newTestDefinition())
		if applyError != nil || generation != 5 {
			subTest.Errorf("世代 5 のはず: %d %v", generation, applyError)
		}
	})
}

// TestKubernetesDeploymentOperatorFSGroup は、fsGroup が指定されたときだけ、Pod に付くことを確認する(非 root のイメージが、ボリュームに書けるようにする)。
func TestKubernetesDeploymentOperatorFSGroup(testingT *testing.T) {
	for name, testCase := range map[string]struct {
		fsGroup *int64
		want    *int64
	}{"指定あり": {fsGroup: int64Pointer(472), want: int64Pointer(472)}, "指定なし": {fsGroup: nil, want: nil}} {
		testingT.Run(name, func(subTest *testing.T) {
			fakeClientset := fake.NewSimpleClientset()
			definition := newTestDefinition()
			definition.FSGroup = testCase.fsGroup
			if _, applyError := NewKubernetesDeploymentOperator(fakeClientset, OperatorSettings{}).ApplyDeployment(context.Background(), definition); applyError != nil {
				subTest.Fatal(applyError)
			}
			deployment, _ := fakeClientset.AppsV1().Deployments(validNamespaceName).Get(context.Background(), testDeploymentName, metav1.GetOptions{})
			securityContext := deployment.Spec.Template.Spec.SecurityContext
			if testCase.want == nil && securityContext != nil && securityContext.FSGroup != nil {
				subTest.Errorf("指定なしのときは、fsGroup が付かないはず: %+v", securityContext)
			}
			if testCase.want != nil && (securityContext == nil || securityContext.FSGroup == nil || *securityContext.FSGroup != *testCase.want) {
				subTest.Errorf("fsGroup が付くはず: %+v", securityContext)
			}
		})
	}
}

// int64Pointer は、値のポインターを返す(試験用)。
func int64Pointer(value int64) *int64 { return &value }

// TestBuildDeploymentRequestPercent は、requests を limits の何 % にするかの設定が、Deployment の requests だけに効き、
// limits(deploy の大きさ)は変わらないこと、未設定と範囲の外は、requests と limits が同じになることを確認する。
func TestBuildDeploymentRequestPercent(testingT *testing.T) {
	testCases := []struct {
		name                 string
		cpuPercent           int
		memoryPercent        int
		wantCPURequestMilli  int64
		wantMemoryRequestMiB int64
	}{
		{"未設定は limits と同じ", 0, 0, 250, 256},
		{"CPU を 10%", 10, 0, 25, 256},
		{"CPU を 10%、メモリを 50%", 10, 50, 25, 128},
		{"範囲の外は limits と同じ", 150, -1, 250, 256},
		{"1m を下回らない", 1, 100, 2, 256},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			operator := NewKubernetesDeploymentOperator(nil, OperatorSettings{CPURequestPercent: testCase.cpuPercent, MemoryRequestPercent: testCase.memoryPercent})
			container := operator.buildDeployment(newTestDefinition()).Spec.Template.Spec.Containers[0]
			// requests は、設定した割合。
			if container.Resources.Requests.Cpu().MilliValue() != testCase.wantCPURequestMilli || container.Resources.Requests.Memory().Value() != testCase.wantMemoryRequestMiB*1024*1024 {
				subTest.Errorf("requests が違う: %+v", container.Resources.Requests)
			}
			// limits は、いつも、deploy の大きさ(250m、256Mi)のまま。
			if container.Resources.Limits.Cpu().MilliValue() != 250 || container.Resources.Limits.Memory().Value() != 256*1024*1024 {
				subTest.Errorf("limits は変わらないはず: %+v", container.Resources.Limits)
			}
		})
	}
}
