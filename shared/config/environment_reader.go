// Package config は、環境変数から設定を読む部品を提供する。
//
// 設定は環境変数だけで受け取る。必須の値が無いなど、設定に問題があるときは、
// 起動時に全ての問題をまとめて報告して、起動を止める(1つずつ直す手間を減らすため)。
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// LookupFunc は、環境変数の名前から値を調べる関数。
// os.LookupEnv と同じ形で、値と、その環境変数が設定されているかを返す。
// テストでは、固定の値を返す関数に差し替える。
type LookupFunc func(name string) (string, bool)

// EnvironmentReader は、環境変数を読み、設定に問題があれば覚えておく。
//
// 各メソッドは、問題があっても止まらず、代わりの値を返して、問題を覚える。
// 全ての環境変数を読み終えたあとで、Err を呼んで、まとめて確認する。
type EnvironmentReader struct {
	// lookup は、環境変数の値を調べる関数。
	lookup LookupFunc

	// problems は、これまでに見つかった設定の問題の説明。
	problems []string
}

// NewEnvironmentReader は、指定した関数で環境変数を調べる EnvironmentReader を作る。
//
// 引数:
//   - lookup: 環境変数の値を調べる関数。
//
// 戻り値:
//   - 作成した EnvironmentReader。
func NewEnvironmentReader(lookup LookupFunc) *EnvironmentReader {
	return &EnvironmentReader{lookup: lookup}
}

// NewProcessEnvironmentReader は、実行中のプロセスの環境変数を読む EnvironmentReader を作る。
//
// 戻り値:
//   - 作成した EnvironmentReader。
func NewProcessEnvironmentReader() *EnvironmentReader {
	return NewEnvironmentReader(os.LookupEnv)
}

// RequiredString は、必須の文字列の環境変数を読む。
//
// 環境変数が無い、または空のときは、問題として覚える。
//
// 引数:
//   - name: 環境変数の名前。
//
// 戻り値:
//   - 環境変数の値。問題があったときは空文字。
func (reader *EnvironmentReader) RequiredString(name string) string {
	value, isSet := reader.lookup(name)
	if !isSet || value == "" {
		reader.problems = append(reader.problems, fmt.Sprintf("必須の環境変数 %s が設定されていない", name))
		return ""
	}
	return value
}

// OptionalString は、省略できる文字列の環境変数を読む。
//
// 引数:
//   - name: 環境変数の名前。
//   - defaultValue: 環境変数が無い、または空のときに使う値。
//
// 戻り値:
//   - 環境変数の値。無い、または空のときは defaultValue。
func (reader *EnvironmentReader) OptionalString(name string, defaultValue string) string {
	value, isSet := reader.lookup(name)
	if !isSet || value == "" {
		return defaultValue
	}
	return value
}

// OptionalInt は、省略できる整数の環境変数を読む。
//
// 環境変数が整数として読めないときは、問題として覚える。
//
// 引数:
//   - name: 環境変数の名前。
//   - defaultValue: 環境変数が無い、または空のときに使う値。
//
// 戻り値:
//   - 環境変数の値。無い、空、または整数として読めないときは defaultValue。
func (reader *EnvironmentReader) OptionalInt(name string, defaultValue int) int {
	value, isSet := reader.lookup(name)
	if !isSet || value == "" {
		return defaultValue
	}

	parsedValue, parseError := strconv.Atoi(strings.TrimSpace(value))
	if parseError != nil {
		reader.problems = append(reader.problems, fmt.Sprintf("環境変数 %s は整数でなければならない: %q", name, value))
		return defaultValue
	}
	return parsedValue
}

// OptionalDuration は、省略できる時間の環境変数を読む。
//
// 時間は "30s"、"5m"、"1h30m" のような、Go の time.ParseDuration の形で書く。
// 環境変数が時間として読めないときは、問題として覚える。
//
// 引数:
//   - name: 環境変数の名前。
//   - defaultValue: 環境変数が無い、または空のときに使う値。
//
// 戻り値:
//   - 環境変数の値。無い、空、または時間として読めないときは defaultValue。
func (reader *EnvironmentReader) OptionalDuration(name string, defaultValue time.Duration) time.Duration {
	value, isSet := reader.lookup(name)
	if !isSet || value == "" {
		return defaultValue
	}

	parsedValue, parseError := time.ParseDuration(strings.TrimSpace(value))
	if parseError != nil {
		reader.problems = append(reader.problems, fmt.Sprintf("環境変数 %s は時間(例: 30s、5m)でなければならない: %q", name, value))
		return defaultValue
	}
	return parsedValue
}

// Err は、これまでに見つかった設定の問題を、1つのエラーにまとめて返す。
//
// 戻り値:
//   - 問題が無かったときは nil。
//   - 問題があったときは、全ての問題の説明を含むエラー。
func (reader *EnvironmentReader) Err() error {
	if len(reader.problems) == 0 {
		return nil
	}
	return errors.New("環境変数の設定に問題がある: " + strings.Join(reader.problems, "; "))
}
