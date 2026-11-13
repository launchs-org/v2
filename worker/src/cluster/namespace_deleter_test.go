package cluster

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// validNamespaceName は、プロジェクトの ID の形の、テスト用の namespace の名前。
const validNamespaceName = "project-5d3e1829-6d50-4752-bcb3-ee80a0db0f76"

// newNamespace は、テスト用の namespace を作る。
//
// 引数:
//   - namespaceName: namespace の名前。
//
// 戻り値:
//   - namespace のオブジェクト。
func newNamespace(namespaceName string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}}
}

// TestKubernetesNamespaceDeleterDeleteNamespace は、namespace を削除でき、
// すでに無い・すでに削除中でも成功し、他の失敗はエラーになることを確認する。
func TestKubernetesNamespaceDeleterDeleteNamespace(testingT *testing.T) {
	// 存在する・存在しない・削除中・k8s の障害の場合。
	testCases := []struct {
		name          string
		existing      []runtime.Object
		reactorError  error
		expectError   bool
		expectRemoved bool
	}{
		{name: "存在する namespace を削除できる", existing: []runtime.Object{newNamespace(validNamespaceName)}, expectRemoved: true},
		{name: "すでに無くても成功する(冪等)"},
		{name: "すでに削除中(Conflict)でも成功する", existing: []runtime.Object{newNamespace(validNamespaceName)},
			reactorError: apierrors.NewConflict(schema.GroupResource{Resource: "namespaces"}, validNamespaceName, errors.New("terminating"))},
		{name: "k8s の障害はエラーになる", existing: []runtime.Object{newNamespace(validNamespaceName)},
			reactorError: apierrors.NewInternalError(errors.New("boom")), expectError: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// fake のクライアントを作り、必要なら、削除の操作が決めたエラーを返すようにする。
			fakeClientset := fake.NewSimpleClientset(testCase.existing...)
			if testCase.reactorError != nil {
				fakeClientset.PrependReactor("delete", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, testCase.reactorError
				})
			}

			// 削除を頼む。
			deleteError := NewKubernetesNamespaceDeleter(fakeClientset).DeleteNamespace(context.Background(), validNamespaceName)

			// エラーの有無と、実際に消えたかを確認する。
			if (deleteError != nil) != testCase.expectError {
				subTest.Errorf("エラーの有無が違う: %v", deleteError)
			}
			_, getError := fakeClientset.CoreV1().Namespaces().Get(context.Background(), validNamespaceName, metav1.GetOptions{})
			if testCase.expectRemoved && !apierrors.IsNotFound(getError) {
				subTest.Errorf("namespace が消えているはず: %v", getError)
			}
		})
	}
}

// TestKubernetesNamespaceDeleterRejectsNonProjectNames は、プロジェクトの ID の形でない名前は、
// k8s に触れる前に拒否され、namespace が消えないことを確認する(誤削除の防止)。
func TestKubernetesNamespaceDeleterRejectsNonProjectNames(testingT *testing.T) {
	// 消してはいけない名前の例。
	rejectedNames := []string{"kube-system", "default", "", "project-", "project-abc", "project-5d3e1829-6d50-4752-bcb3-ee80a0db0f76-x", "Project-5d3e1829-6d50-4752-bcb3-ee80a0db0f76", "project-5D3E1829-6D50-4752-BCB3-EE80A0DB0F76"}
	for _, rejectedName := range rejectedNames {
		testingT.Run("拒否する: "+rejectedName, func(subTest *testing.T) {
			// その名前の namespace が実在する状態にして、削除と確認を頼む。
			fakeClientset := fake.NewSimpleClientset(newNamespace(rejectedName))
			deleter := NewKubernetesNamespaceDeleter(fakeClientset)
			deleteError := deleter.DeleteNamespace(context.Background(), rejectedName)
			_, existsError := deleter.NamespaceExists(context.Background(), rejectedName)

			// どちらもエラーになり、namespace は消えていないこと。
			if deleteError == nil || existsError == nil {
				subTest.Errorf("どちらも拒否されるはず: 削除 %v、確認 %v", deleteError, existsError)
			}
			if _, getError := fakeClientset.CoreV1().Namespaces().Get(context.Background(), rejectedName, metav1.GetOptions{}); getError != nil {
				subTest.Errorf("namespace は消えていないはず: %v", getError)
			}
		})
	}
}

// TestKubernetesNamespaceDeleterNamespaceExists は、残っていれば true、無ければ false、
// 問い合わせの失敗はエラーになることを確認する。
func TestKubernetesNamespaceDeleterNamespaceExists(testingT *testing.T) {
	// 残っている・無い・k8s の障害の場合。
	testCases := []struct {
		name           string
		existing       []runtime.Object
		reactorError   error
		expectedExists bool
		expectError    bool
	}{
		{name: "残っていれば true", existing: []runtime.Object{newNamespace(validNamespaceName)}, expectedExists: true},
		{name: "無ければ false"},
		{name: "k8s の障害はエラー", reactorError: apierrors.NewInternalError(errors.New("boom")), expectError: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// fake のクライアントを作り、必要なら、取得の操作が決めたエラーを返すようにする。
			fakeClientset := fake.NewSimpleClientset(testCase.existing...)
			if testCase.reactorError != nil {
				fakeClientset.PrependReactor("get", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, testCase.reactorError
				})
			}

			// 残っているかを確かめる。
			actualExists, existsError := NewKubernetesNamespaceDeleter(fakeClientset).NamespaceExists(context.Background(), validNamespaceName)

			// 結果とエラーの有無を確認する。
			if actualExists != testCase.expectedExists || (existsError != nil) != testCase.expectError {
				subTest.Errorf("期待 %v(エラー %v)、実際 %v(エラー %v)", testCase.expectedExists, testCase.expectError, actualExists, existsError)
			}
		})
	}
}
