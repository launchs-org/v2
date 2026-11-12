package config

import (
	"log/slog"
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
	return sharedconfig.NewEnvironmentReader(func(name string) (string, bool) {
		value, isSet := environment[name]
		return value, isSet
	})
}

// TestLoadBackendConfig は、環境変数からバックエンドの設定を読めることを確認する。
// 省略すると既定値になり、問題があるときはエラーになる。
func TestLoadBackendConfig(testingT *testing.T) {
	testCases := []struct {
		name                string
		environment         map[string]string
		expectedConfig      BackendConfig
		expectedErrorPieces []string
	}{
		{
			name:           "DATABASE_URL だけ設定すれば、他は既定値になる",
			environment:    map[string]string{"DATABASE_URL": "postgres://db"},
			expectedConfig: BackendConfig{HTTPPort: 8080, LogLevel: slog.LevelInfo, DatabaseURL: "postgres://db"},
		},
		{
			name:           "全て設定すると、その値になる",
			environment:    map[string]string{"DATABASE_URL": "postgres://db", "HTTP_PORT": "9090", "LOG_LEVEL": "debug"},
			expectedConfig: BackendConfig{HTTPPort: 9090, LogLevel: slog.LevelDebug, DatabaseURL: "postgres://db"},
		},
		{
			name:                "DATABASE_URL が無いとエラー",
			environment:         map[string]string{},
			expectedErrorPieces: []string{"DATABASE_URL"},
		},
		{
			name:                "ポート番号が整数でないとエラー",
			environment:         map[string]string{"DATABASE_URL": "postgres://db", "HTTP_PORT": "abc"},
			expectedErrorPieces: []string{"HTTP_PORT"},
		},
		{
			name:                "ポート番号が0だとエラー",
			environment:         map[string]string{"DATABASE_URL": "postgres://db", "HTTP_PORT": "0"},
			expectedErrorPieces: []string{"HTTP_PORT"},
		},
		{
			name:                "ポート番号が65536だとエラー",
			environment:         map[string]string{"DATABASE_URL": "postgres://db", "HTTP_PORT": "65536"},
			expectedErrorPieces: []string{"HTTP_PORT"},
		},
		{
			name:                "ログレベルが未知だとエラー",
			environment:         map[string]string{"DATABASE_URL": "postgres://db", "LOG_LEVEL": "verbose"},
			expectedErrorPieces: []string{"LOG_LEVEL"},
		},
		{
			name:                "問題が複数あると、全てまとめて報告する",
			environment:         map[string]string{"DATABASE_URL": "postgres://db", "HTTP_PORT": "99999", "LOG_LEVEL": "verbose"},
			expectedErrorPieces: []string{"HTTP_PORT", "LOG_LEVEL"},
		},
	}

	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			actualConfig, actualError := LoadBackendConfig(newTestReader(testCase.environment))

			if len(testCase.expectedErrorPieces) == 0 {
				if actualError != nil {
					subTest.Fatalf("エラーにならないはずなのに、エラーがある: %v", actualError)
				}
				if actualConfig != testCase.expectedConfig {
					subTest.Errorf("設定が違う: 期待 %+v、実際 %+v", testCase.expectedConfig, actualConfig)
				}
				return
			}

			if actualError == nil {
				subTest.Fatalf("エラーになるはずなのに、エラーが無い。設定: %+v", actualConfig)
			}
			for _, expectedPiece := range testCase.expectedErrorPieces {
				if !strings.Contains(actualError.Error(), expectedPiece) {
					subTest.Errorf("エラーに %s が含まれていない: %v", expectedPiece, actualError)
				}
			}
			if actualConfig != (BackendConfig{}) {
				subTest.Errorf("エラーのときは、ゼロ値の設定を返すはず: %+v", actualConfig)
			}
		})
	}
}
