package config

import (
	"log/slog"
	"reflect"
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
	// 指定した map だけを環境変数として返す読み取り関数で、reader を作って返す。
	return sharedconfig.NewEnvironmentReader(func(name string) (string, bool) {
		// map から値を探す。無いときは、isSet が false になり、未設定として扱われる。
		value, isSet := environment[name]
		// 値と、設定されているかどうかを返す。
		return value, isSet
	})
}

// withIngressDefaults は、期待する設定に、ingress の設定の既定値を入れる(ingress の設定を指定しないケース用)。
//
// 引数:
//   - expected: ingress の設定が空の、期待する設定。
//
// 戻り値:
//   - ingress の設定に、既定値を入れた設定。
func withIngressDefaults(expected BackendConfig) BackendConfig {
	// ベースドメインと、払い出さないサブドメインに、既定値を入れる。
	expected.IngressBaseDomain = "launchs.org"
	expected.IngressReservedSubdomains = splitCommaSeparated(defaultIngressReservedSubdomains)
	return expected
}

// TestLoadBackendConfigIngress は、ingress のホスト名の設定を、環境変数で変えられることを確認する。
//
// 引数:
//   - testingT: テストの状態。
func TestLoadBackendConfigIngress(testingT *testing.T) {
	// 値を指定すると、既定値を置き換える(空の要素と前後の空白は捨てる)。
	loaded, loadError := LoadBackendConfig(newTestReader(map[string]string{
		"DATABASE_URL": "postgres://db", "INGRESS_BASE_DOMAIN": "apps.example.test", "INGRESS_RESERVED_SUBDOMAINS": " admin, ,root ",
	}))
	if loadError != nil || loaded.IngressBaseDomain != "apps.example.test" || !reflect.DeepEqual(loaded.IngressReservedSubdomains, []string{"admin", "root"}) {
		testingT.Fatalf("環境変数の値で置き換わるはず: %+v %v", loaded, loadError)
	}
	// DNS 名の形でないベースドメインは、エラーにする。
	for _, invalid := range []string{"Launchs.org", "localhost", "a..b", "-a.org", "a b.org"} {
		if _, invalidError := LoadBackendConfig(newTestReader(map[string]string{"DATABASE_URL": "postgres://db", "INGRESS_BASE_DOMAIN": invalid})); invalidError == nil || !strings.Contains(invalidError.Error(), "INGRESS_BASE_DOMAIN") {
			testingT.Errorf("%q はエラーになるはず: %v", invalid, invalidError)
		}
	}
}

// TestLoadBackendConfig は、環境変数からバックエンドの設定を読めることを確認する。
// 省略すると既定値になり、問題があるときはエラーになる。
func TestLoadBackendConfig(testingT *testing.T) {
	// 入力の環境変数と、期待する設定(または期待するエラーに含まれる文字列)の組を並べる。ケースの意図は name で示す。
	testCases := []struct {
		name                string
		environment         map[string]string
		expectedConfig      BackendConfig
		expectedErrorPieces []string
	}{
		{
			name:           "DATABASE_URL だけ設定すれば、他は既定値になる",
			environment:    map[string]string{"DATABASE_URL": "postgres://db"},
			expectedConfig: withIngressDefaults(BackendConfig{HTTPPort: 8080, LogLevel: slog.LevelInfo, DatabaseURL: "postgres://db", TemporalAddress: "temporal:7233", TemporalNamespace: "default"}),
		},
		{
			name:           "全て設定すると、その値になる",
			environment:    map[string]string{"DATABASE_URL": "postgres://db", "HTTP_PORT": "9090", "LOG_LEVEL": "debug", "TEMPORAL_ADDRESS": "host:1", "TEMPORAL_NAMESPACE": "ns"},
			expectedConfig: withIngressDefaults(BackendConfig{HTTPPort: 9090, LogLevel: slog.LevelDebug, DatabaseURL: "postgres://db", TemporalAddress: "host:1", TemporalNamespace: "ns"}),
		},
		{
			name:           "DEV_USER_ID を設定すると、その値になる",
			environment:    map[string]string{"DATABASE_URL": "postgres://db", "DEV_USER_ID": "user-dev"},
			expectedConfig: withIngressDefaults(BackendConfig{HTTPPort: 8080, LogLevel: slog.LevelInfo, DatabaseURL: "postgres://db", DevUserID: "user-dev", TemporalAddress: "temporal:7233", TemporalNamespace: "default"}),
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

	// 全てのケースを、別々のサブテストとして実行する。
	for _, testCase := range testCases {
		// ケースごとに、名前を付けて、サブテストを走らせる。
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// ケースの環境変数で設定を読み、結果とエラーを受け取る。
			actualConfig, actualError := LoadBackendConfig(newTestReader(testCase.environment))

			// エラーを期待しないケースは、エラーが無いことと、設定が一致することを確認する。
			if len(testCase.expectedErrorPieces) == 0 {
				// エラーが出ていたら、以降の比較は意味が無いので、すぐに失敗させる。
				if actualError != nil {
					subTest.Fatalf("エラーにならないはずなのに、エラーがある: %v", actualError)
				}
				// 読み込んだ設定が期待と違うときは、両方を表示して失敗させる。
				if !reflect.DeepEqual(actualConfig, testCase.expectedConfig) {
					subTest.Errorf("設定が違う: 期待 %+v、実際 %+v", testCase.expectedConfig, actualConfig)
				}
				// 正常系の確認は終わったので、エラーの確認には進まない。
				return
			}

			// エラーを期待するケースで、エラーが無いときは、すぐに失敗させる。
			if actualError == nil {
				subTest.Fatalf("エラーになるはずなのに、エラーが無い。設定: %+v", actualConfig)
			}
			// 期待する文字列が、全てエラーに含まれていることを確認する(複数の問題をまとめて報告できているかの確認)。
			for _, expectedPiece := range testCase.expectedErrorPieces {
				// 含まれていない文字列があれば、失敗させる。
				if !strings.Contains(actualError.Error(), expectedPiece) {
					subTest.Errorf("エラーに %s が含まれていない: %v", expectedPiece, actualError)
				}
			}
			// エラーのときは、中途半端な設定を返さず、ゼロ値であることを確認する。
			if !reflect.DeepEqual(actualConfig, BackendConfig{}) {
				subTest.Errorf("エラーのときは、ゼロ値の設定を返すはず: %+v", actualConfig)
			}
		})
	}
}
