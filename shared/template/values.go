package template

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"unicode/utf8"
)

// defaultPasswordLength は、自動で作る password の、既定の文字数。
const defaultPasswordLength = 24

// passwordAlphabet は、自動で作る password に使う文字(英数字。記号は、設定ファイルやシェルで、扱いにくいので、入れない)。
const passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// sortStrings は、文字列の一覧を、辞書順に並べる。
//
// 引数:
//   - values: 並べる一覧(その場で並べ替える)。
func sortStrings(values []string) {
	// 標準の並べ替えに任せる。
	sort.Strings(values)
}

// GeneratePassword は、暗号論的な乱数で、英数字の password を作る。
//
// 1 文字ずつ、文字の集合(62 文字)から、rand.Int で選ぶ(乱数を、文字数で割った余りで選ぶと、偏りが出るので、しない)。
// 作った値は、ログに出してはいけない。
//
// 引数:
//   - length: 文字数(1 以上)。
//
// 戻り値:
//   - 作った password。
//   - 長さが不正なとき、または、乱数を得られなかったときのエラー。
func GeneratePassword(length int) (string, error) {
	// 長さが 0 以下は、作れない。
	if length < 1 {
		return "", fmt.Errorf("password の長さは、1 以上です")
	}
	// 1 文字ずつ、集合の中から、偏り無く選ぶ。
	limit := big.NewInt(int64(len(passwordAlphabet)))
	characters := make([]byte, length)
	for index := range characters {
		choice, randomError := rand.Int(rand.Reader, limit)
		if randomError != nil {
			return "", fmt.Errorf("乱数を得られませんでした: %w", randomError)
		}
		characters[index] = passwordAlphabet[choice.Int64()]
	}
	return string(characters), nil
}

// validateValue は、変数の値1つが、その変数の型と制約に合うかを確かめる。
//
// 引数:
//   - variable: 変数の定義。
//   - value: 入力された値(文字列)。
//
// 戻り値:
//   - 合わなければ、その理由。
func validateValue(variable Variable, value string) error {
	// 型ごとに確かめる。
	switch variable.Type {
	case VariableInteger:
		number, parseError := strconv.Atoi(value)
		if parseError != nil {
			return fmt.Errorf("整数で入力してください")
		}
		if variable.Min != nil && number < *variable.Min {
			return fmt.Errorf("%d 以上で入力してください", *variable.Min)
		}
		if variable.Max != nil && number > *variable.Max {
			return fmt.Errorf("%d 以下で入力してください", *variable.Max)
		}
	case VariableBoolean:
		if value != "true" && value != "false" {
			return fmt.Errorf("true か false で入力してください")
		}
	case VariableChoice:
		for _, option := range variable.Options {
			if option.Value == value {
				return nil
			}
		}
		return fmt.Errorf("選択肢にない値です")
	case VariableString, VariablePassword:
		if !utf8.ValidString(value) {
			return fmt.Errorf("UTF-8 のテキストで入力してください")
		}
		length := utf8.RuneCountInString(value)
		if variable.MinLength != nil && length < *variable.MinLength {
			return fmt.Errorf("%d 文字以上で入力してください", *variable.MinLength)
		}
		if variable.MaxLength != nil && length > *variable.MaxLength {
			return fmt.Errorf("%d 文字以下で入力してください", *variable.MaxLength)
		}
		// pattern は、値の全体に一致させる(一部だけの一致では、通さない)。
		if variable.Pattern != "" {
			matched, matchError := regexp.MatchString("^(?:"+variable.Pattern+")$", value)
			if matchError != nil || !matched {
				return fmt.Errorf("形式が合いません")
			}
		}
	}
	// 改行や NUL は、環境変数や、設定ファイルの値を壊すので、1 行の string と password には、入れさせない(複数行を許す変数は、まだ無い)。
	if variable.Type == VariableString || variable.Type == VariablePassword {
		for _, character := range value {
			if character == '\n' || character == '\r' || character == 0 {
				return fmt.Errorf("改行は使えません")
			}
		}
	}
	return nil
}

// ResolveValues は、利用者が入力した値に、既定値と、自動生成の password を補い、全ての変数の値を、検査して返す。
//
// 引数:
//   - template: テンプレート。
//   - input: 利用者が入力した値(変数名 → 文字列)。空の文字列は、入力が無い扱い。
//
// 戻り値:
//   - 全ての変数の値(補ったもの)。テンプレートに無い変数名が入力に含まれていたら、エラー。
//   - 必須の入力が無い、または、値が型と制約に合わないときの、変数ごとのエラー(日本語)。最初の1つだけを返す。
func ResolveValues(template Template, input map[string]string) (map[string]string, error) {
	// テンプレートに無い変数は、typo の可能性が高いので、拒否する。
	known := map[string]bool{}
	for _, variable := range template.Variables {
		known[variable.Name] = true
	}
	for name := range input {
		if !known[name] {
			return nil, fmt.Errorf("変数 %q は、このテンプレートにありません", name)
		}
	}
	// 変数ごとに、入力、既定値、自動生成の順に、値を決めて、検査する。
	resolved := map[string]string{}
	for _, variable := range template.Variables {
		value := input[variable.Name]
		switch {
		case value != "":
		case variable.Type == VariablePassword && variable.Generate:
			length := defaultPasswordLength
			if variable.Length != nil {
				length = *variable.Length
			}
			generated, generateError := GeneratePassword(length)
			if generateError != nil {
				return nil, generateError
			}
			value = generated
		case variable.Default != nil:
			value = *variable.Default
		default:
			return nil, fmt.Errorf("%s(%s)の入力が必要です", variable.Label, variable.Name)
		}
		if valueError := validateValue(variable, value); valueError != nil {
			return nil, fmt.Errorf("%s(%s): %v", variable.Label, variable.Name, valueError)
		}
		resolved[variable.Name] = value
	}
	return resolved, nil
}

// ValidateValues は、利用者が入力した値が、検査に通るかを確かめる(補った値は、返さない)。
//
// 引数:
//   - template: テンプレート。
//   - input: 利用者が入力した値。
//
// 戻り値:
//   - 通らなければ、最初の1つのエラー。
func ValidateValues(template Template, input map[string]string) error {
	// 補ったうえで検査する(自動生成の password も、同じ制約に通ることを確かめる)。
	_, resolveError := ResolveValues(template, input)
	return resolveError
}
