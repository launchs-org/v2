package model

import (
	"strings"
	"testing"

	"gorm.io/datatypes"

	"launchs/shared/apperror"
)

// TestNormalizeDeployTags は、タグの検査(文字、長さ、数、重複、並び)を確認する。
func TestNormalizeDeployTags(testingT *testing.T) {
	for _, testCase := range []struct {
		name      string
		tags      []string
		want      string
		wantError bool
	}{
		{"空の組は許す", []string{}, "", false},
		{"重複を除き、昇順に並べる", []string{"b", "a", "b"}, "a,b", false},
		{"前後の空白は取り除く", []string{" web "}, "web", false},
		{"template: の形を許す", []string{"template:redash"}, "template:redash", false},
		{"大文字は不可", []string{"Web"}, "", true},
		{"空のタグは不可", []string{" "}, "", true},
		{"記号で始まると不可", []string{"-a"}, "", true},
		{"40 文字は許す", []string{strings.Repeat("a", 40)}, strings.Repeat("a", 40), false},
		{"41 文字は不可", []string{strings.Repeat("a", 41)}, "", true},
		{"10 個は許す", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, "a,b,c,d,e,f,g,h,i,j", false},
		{"11 個は不可", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}, "", true},
	} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// 検査を実行する。
			normalized, normalizeError := NormalizeDeployTags(testCase.tags)
			// 不可のケースは、invalid_request のはず。
			if testCase.wantError {
				if normalizeError == nil || apperror.KindOf(normalizeError) != apperror.KindInvalidRequest {
					subTest.Errorf("invalid_request のはず: %v", normalizeError)
				}
				return
			}
			// 許すケースは、整えた結果が期待どおりのはず。
			if normalizeError != nil || strings.Join(normalized, ",") != testCase.want {
				subTest.Errorf("期待 %q、実際 %v、エラー %v", testCase.want, normalized, normalizeError)
			}
		})
	}
}

// TestTemplateDeployTags は、テンプレートの deploy に付くタグが、検査に通り、適用ごとに違うことを確認する。
func TestTemplateDeployTags(testingT *testing.T) {
	// 2 つの適用のタグを作る。
	first := TemplateDeployTags("redash", "apply-11111111-2222-4333-8444-aaaaaaaaaaaa")
	second := TemplateDeployTags("redash", "apply-11111111-2222-4333-8444-bbbbbbbbbbbb")
	// どちらも、検査に通り、変わらない。
	if normalized, normalizeError := NormalizeDeployTags(first); normalizeError != nil || strings.Join(normalized, ",") != strings.Join(first, ",") {
		testingT.Fatalf("検査に通るはず: %v %v", first, normalizeError)
	}
	// template: は同じで、apply: は違う。
	if first[1] != "template:redash" || first[0] == second[0] {
		testingT.Errorf("template は同じ、apply は違うはず: %v %v", first, second)
	}
}

// TestDeployTagList は、列の JSON から、タグの配列を読むことを確認する(空と壊れた値は、空の配列)。
func TestDeployTagList(testingT *testing.T) {
	// 配列を読める。
	if tags := (Deploy{Tags: datatypes.JSON(`["a","b"]`)}).TagList(); len(tags) != 2 || tags[1] != "b" {
		testingT.Errorf("2 つのはず: %v", tags)
	}
	// 空と壊れた値は、nil ではなく空の配列。
	for _, column := range []datatypes.JSON{nil, datatypes.JSON(`{`)} {
		if tags := (Deploy{Tags: column}).TagList(); tags == nil || len(tags) != 0 {
			testingT.Errorf("空の配列のはず: %v", tags)
		}
	}
}
