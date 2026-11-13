package k8s

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// minimalKubeconfig は、クラスタへ通信せずに読み込めるだけの、最小の kubeconfig。
const minimalKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:6443
users:
- name: test
  user:
    token: dummy
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
`

// TestNewClientset は、接続の方法ごとに、クライアントを作れるか、決まったエラーになるかを確認する。
func TestNewClientset(testingT *testing.T) {
	// 一時フォルダに、読み込める kubeconfig と、壊れた kubeconfig を作る。
	temporaryDirectory := testingT.TempDir()
	validPath := filepath.Join(temporaryDirectory, "valid.yaml")
	brokenPath := filepath.Join(temporaryDirectory, "broken.yaml")
	if writeError := os.WriteFile(validPath, []byte(minimalKubeconfig), 0o600); writeError != nil {
		testingT.Fatalf("ファイルを書けませんでした: %v", writeError)
	}
	if writeError := os.WriteFile(brokenPath, []byte("not: [valid"), 0o600); writeError != nil {
		testingT.Fatalf("ファイルを書けませんでした: %v", writeError)
	}

	// 各接続の方法の期待。
	testCases := []struct {
		name             string
		settings         Settings
		expectClient     bool
		expectNotConnect bool
		expectError      bool
	}{
		{name: "kubeconfig が読めればクライアントを作れる", settings: Settings{Access: AccessKubeconfig, KubeconfigPath: validPath}, expectClient: true},
		{name: "none は ErrNotConnected", settings: Settings{Access: AccessNone}, expectNotConnect: true, expectError: true},
		{name: "kubeconfig のファイルが無いとエラー", settings: Settings{Access: AccessKubeconfig, KubeconfigPath: filepath.Join(temporaryDirectory, "missing.yaml")}, expectError: true},
		{name: "kubeconfig が壊れているとエラー", settings: Settings{Access: AccessKubeconfig, KubeconfigPath: brokenPath}, expectError: true},
		{name: "in-cluster は、クラスタの外ではエラー", settings: Settings{Access: AccessInCluster}, expectError: true},
		{name: "未知の方法はエラー", settings: Settings{Access: "unknown"}, expectError: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			clientset, clientError := NewClientset(testCase.settings)

			// エラーの有無、クライアントの有無、ErrNotConnected かどうかを確認する。
			if (clientError != nil) != testCase.expectError {
				subTest.Fatalf("エラーの有無が違う: %v", clientError)
			}
			if (clientset != nil) != testCase.expectClient {
				subTest.Errorf("クライアントの有無が違う: %v", clientset)
			}
			if errors.Is(clientError, ErrNotConnected) != testCase.expectNotConnect {
				subTest.Errorf("ErrNotConnected かどうかが違う: %v", clientError)
			}
		})
	}
}
