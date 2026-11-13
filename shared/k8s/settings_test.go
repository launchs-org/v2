package k8s

import (
	"strings"
	"testing"

	sharedconfig "launchs/shared/config"
)

// newTestReader は、固定の環境変数を持つ EnvironmentReader を作る(テスト用)。
//
// 引数:
//   - environment: 環境変数の名前と値の組。
//
// 戻り値:
//   - 指定した環境変数だけを読める EnvironmentReader。
func newTestReader(environment map[string]string) *sharedconfig.EnvironmentReader {
	// 環境変数の代わりに、渡された map から読む関数を使う。
	return sharedconfig.NewEnvironmentReader(func(name string) (string, bool) {
		value, isSet := environment[name]
		return value, isSet
	})
}

// TestReadSettingsAndValidate は、環境変数から設定を読み、値を検査できることを確認する。
func TestReadSettingsAndValidate(testingT *testing.T) {
	// 正しい設定と、正しくない設定の場合。
	testCases := []struct {
		name                string
		environment         map[string]string
		expectedSettings    Settings
		expectedErrorPieces []string
	}{
		{name: "none を指定できる", environment: map[string]string{"KUBERNETES_ACCESS": "none"}, expectedSettings: Settings{Access: AccessNone}},
		{name: "in-cluster を指定できる", environment: map[string]string{"KUBERNETES_ACCESS": "in-cluster"}, expectedSettings: Settings{Access: AccessInCluster}},
		{name: "kubeconfig とパスを指定できる", environment: map[string]string{"KUBERNETES_ACCESS": "kubeconfig", "KUBECONFIG": "/kube/config"}, expectedSettings: Settings{Access: AccessKubeconfig, KubeconfigPath: "/kube/config"}},
		{name: "kubeconfig なのにパスが無いとエラー", environment: map[string]string{"KUBERNETES_ACCESS": "kubeconfig"}, expectedErrorPieces: []string{"KUBECONFIG"}},
		{name: "未知の値だとエラー", environment: map[string]string{"KUBERNETES_ACCESS": "yes"}, expectedErrorPieces: []string{"KUBERNETES_ACCESS", "yes"}},
		{name: "KUBERNETES_ACCESS が無いと、読み込みの時点でエラー(何も消さないまま動くのを防ぐ)", environment: map[string]string{}, expectedErrorPieces: []string{"KUBERNETES_ACCESS"}},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// 設定を読み、読み込みの問題と、値の検査の問題を合わせて集める。
			reader := newTestReader(testCase.environment)
			actualSettings := ReadSettings(reader)
			problems := reader.Err()
			if validateError := actualSettings.Validate(); validateError != nil {
				problems = validateError
			}

			// 問題を期待しないケースは、設定の一致を確認する。
			if len(testCase.expectedErrorPieces) == 0 {
				if problems != nil || actualSettings != testCase.expectedSettings {
					subTest.Errorf("期待 %+v、実際 %+v(問題 %v)", testCase.expectedSettings, actualSettings, problems)
				}
				return
			}
			// 問題を期待するケースは、全ての語がエラーに含まれることを確認する。
			if problems == nil {
				subTest.Fatalf("問題があるはず")
			}
			for _, expectedPiece := range testCase.expectedErrorPieces {
				if !strings.Contains(problems.Error(), expectedPiece) {
					subTest.Errorf("エラーに %s が含まれていない: %v", expectedPiece, problems)
				}
			}
		})
	}
}
