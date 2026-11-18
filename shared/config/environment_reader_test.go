package config

import (
	"strings"
	"testing"
	"time"
)

// newTestReader は、固定の環境変数を持つ EnvironmentReader を作る(テスト用)。
//
// 引数:
//   - environment: 環境変数の名前と値の組。
//
// 戻り値:
//   - 指定した環境変数だけを読める EnvironmentReader。
func newTestReader(environment map[string]string) *EnvironmentReader {
	// 渡された map から値を引く関数で、EnvironmentReader を作る。
	return NewEnvironmentReader(func(name string) (string, bool) {
		// map に名前があるかどうかを、環境変数が設定されているかとして扱う。
		value, isSet := environment[name]
		// 値と、設定されているかを返す。
		return value, isSet
	})
}

// TestRequiredString は、必須の文字列が、設定されていれば読め、無い・空のときは問題になることを確認する。
func TestRequiredString(testingT *testing.T) {
	// ケースごとの、環境変数と、期待する値・問題の有無を並べる。
	testCases := []struct {
		name          string
		environment   map[string]string
		expectedValue string
		expectProblem bool
	}{
		{"設定されていれば読める", map[string]string{"DATABASE_URL": "postgres://db"}, "postgres://db", false},
		{"設定されていないと問題になる", map[string]string{}, "", true},
		{"空だと問題になる", map[string]string{"DATABASE_URL": ""}, "", true},
	}

	// ケースを1つずつ、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// ケースの環境変数だけを持つ reader を作る。
			reader := newTestReader(testCase.environment)

			// テスト対象の RequiredString を呼ぶ。
			actualValue := reader.RequiredString("DATABASE_URL")

			// 期待した値と違えば、失敗にする。
			if actualValue != testCase.expectedValue {
				subTest.Errorf("値が違う: 期待 %q、実際 %q", testCase.expectedValue, actualValue)
			}
			// 問題の有無が期待と違えば、失敗にする。
			if (reader.Err() != nil) != testCase.expectProblem {
				subTest.Errorf("問題の有無が違う: 期待 %v、実際 %v", testCase.expectProblem, reader.Err())
			}
		})
	}
}

// TestOptionalString は、省略できる文字列が、無い・空のときは既定値になり、問題にならないことを確認する。
func TestOptionalString(testingT *testing.T) {
	// ケースごとの、環境変数と、期待する値を並べる。
	testCases := []struct {
		name          string
		environment   map[string]string
		expectedValue string
	}{
		{"設定されていればその値", map[string]string{"STORAGE_CLASS": "fast"}, "fast"},
		{"設定されていなければ既定値", map[string]string{}, "standard"},
		{"空なら既定値", map[string]string{"STORAGE_CLASS": ""}, "standard"},
	}

	// ケースを1つずつ、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// ケースの環境変数だけを持つ reader を作る。
			reader := newTestReader(testCase.environment)

			// テスト対象の OptionalString を呼ぶ。
			actualValue := reader.OptionalString("STORAGE_CLASS", "standard")

			// 期待した値と違えば、失敗にする。
			if actualValue != testCase.expectedValue {
				subTest.Errorf("値が違う: 期待 %q、実際 %q", testCase.expectedValue, actualValue)
			}
			// 省略できる値は問題にならないので、エラーがあれば失敗にする。
			if reader.Err() != nil {
				subTest.Errorf("問題にならないはずなのに、エラーがある: %v", reader.Err())
			}
		})
	}
}

// TestOptionalInt は、省略できる整数が、読める・無い・整数でないの各場合で、正しい値と問題の有無になることを確認する。
func TestOptionalInt(testingT *testing.T) {
	// ケースごとの、環境変数と、期待する値・問題の有無を並べる。
	testCases := []struct {
		name          string
		environment   map[string]string
		expectedValue int
		expectProblem bool
	}{
		{"整数として読める", map[string]string{"PORT": "9090"}, 9090, false},
		{"前後の空白は無視する", map[string]string{"PORT": " 9090 "}, 9090, false},
		{"設定されていなければ既定値", map[string]string{}, 8080, false},
		{"空なら既定値", map[string]string{"PORT": ""}, 8080, false},
		{"整数でないと、既定値で問題になる", map[string]string{"PORT": "abc"}, 8080, true},
	}

	// ケースを1つずつ、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// ケースの環境変数だけを持つ reader を作る。
			reader := newTestReader(testCase.environment)

			// テスト対象の OptionalInt を呼ぶ。
			actualValue := reader.OptionalInt("PORT", 8080)

			// 期待した値と違えば、失敗にする。
			if actualValue != testCase.expectedValue {
				subTest.Errorf("値が違う: 期待 %d、実際 %d", testCase.expectedValue, actualValue)
			}
			// 問題の有無が期待と違えば、失敗にする。
			if (reader.Err() != nil) != testCase.expectProblem {
				subTest.Errorf("問題の有無が違う: 期待 %v、実際 %v", testCase.expectProblem, reader.Err())
			}
		})
	}
}

// TestOptionalDuration は、省略できる時間が、読める・無い・時間でないの各場合で、正しい値と問題の有無になることを確認する。
func TestOptionalDuration(testingT *testing.T) {
	// ケースごとの、環境変数と、期待する値・問題の有無を並べる。
	testCases := []struct {
		name          string
		environment   map[string]string
		expectedValue time.Duration
		expectProblem bool
	}{
		{"秒で読める", map[string]string{"TIMEOUT": "30s"}, 30 * time.Second, false},
		{"分と時間を組み合わせて読める", map[string]string{"TIMEOUT": "1h30m"}, 90 * time.Minute, false},
		{"設定されていなければ既定値", map[string]string{}, 10 * time.Second, false},
		{"空なら既定値", map[string]string{"TIMEOUT": ""}, 10 * time.Second, false},
		{"時間でないと、既定値で問題になる", map[string]string{"TIMEOUT": "30"}, 10 * time.Second, true},
	}

	// ケースを1つずつ、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// ケースの環境変数だけを持つ reader を作る。
			reader := newTestReader(testCase.environment)

			// テスト対象の OptionalDuration を呼ぶ。
			actualValue := reader.OptionalDuration("TIMEOUT", 10*time.Second)

			// 期待した値と違えば、失敗にする。
			if actualValue != testCase.expectedValue {
				subTest.Errorf("値が違う: 期待 %v、実際 %v", testCase.expectedValue, actualValue)
			}
			// 問題の有無が期待と違えば、失敗にする。
			if (reader.Err() != nil) != testCase.expectProblem {
				subTest.Errorf("問題の有無が違う: 期待 %v、実際 %v", testCase.expectProblem, reader.Err())
			}
		})
	}
}

// TestErrReportsAllProblems は、複数の問題があるとき、止まらずに全て読み、
// Err が全ての問題をまとめて報告することを確認する。
func TestErrReportsAllProblems(testingT *testing.T) {
	// PORT だけが不正な値の環境変数を用意する(他の2つは未設定)。
	reader := newTestReader(map[string]string{"PORT": "abc"})

	// 未設定の必須の値を読んで、問題を1つ覚えさせる。
	reader.RequiredString("DATABASE_URL")
	// 止まらずに、2つ目の必須の値も読む。
	reader.RequiredString("TEMPORAL_ADDRESS")
	// 整数でない値を読んで、3つ目の問題を覚えさせる。
	reader.OptionalInt("PORT", 8080)

	// エラーの文章を入れる変数。エラーが無いときは空のままにして、下の確認で失敗させる。
	errorText := ""
	// まとめたエラーがあれば、その文章を取り出す。
	if reportedError := reader.Err(); reportedError != nil {
		errorText = reportedError.Error()
	}
	// 3つの問題の環境変数名が、全て文章に含まれているかを確認する。
	for _, expectedPart := range []string{"DATABASE_URL", "TEMPORAL_ADDRESS", "PORT"} {
		// 含まれていない問題があれば、失敗にする。
		if !strings.Contains(errorText, expectedPart) {
			testingT.Errorf("エラーに %s の問題が含まれていない: %q", expectedPart, errorText)
		}
	}
}

// TestErrIsNilWithoutProblems は、問題が1つも無いとき、Err が nil を返すことを確認する。
func TestErrIsNilWithoutProblems(testingT *testing.T) {
	// 必須の値だけを設定した環境変数を用意する。
	reader := newTestReader(map[string]string{"DATABASE_URL": "postgres://db"})

	// 設定されている必須の値を読む。
	reader.RequiredString("DATABASE_URL")
	// 省略されている値を読んで、既定値になるようにする。
	reader.OptionalInt("PORT", 8080)

	// 問題が無いのにエラーが返ったら、失敗にする。
	if reportedError := reader.Err(); reportedError != nil {
		testingT.Errorf("問題が無いのに、エラーがある: %v", reportedError)
	}
}
