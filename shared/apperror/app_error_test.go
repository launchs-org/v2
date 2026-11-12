package apperror

import (
	"errors"
	"fmt"
	"testing"
)

// TestErrorString は、Error の文字列に、種類とメッセージが入り、
// 原因があるときは原因も後ろに入ることを確認する。
func TestErrorString(testingT *testing.T) {
	testCases := []struct {
		name           string
		appError       *Error
		expectedString string
	}{
		{
			name:           "原因が無いときは「種類: メッセージ」",
			appError:       New(KindNotFound, "プロジェクトが見つからない"),
			expectedString: "not_found: プロジェクトが見つからない",
		},
		{
			name:           "原因があるときは「種類: メッセージ: 原因」",
			appError:       Wrap(KindInternal, "保存に失敗した", errors.New("connection refused")),
			expectedString: "internal_error: 保存に失敗した: connection refused",
		},
	}

	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			actualString := testCase.appError.Error()
			if actualString != testCase.expectedString {
				subTest.Errorf("文字列が違う: 期待 %q、実際 %q", testCase.expectedString, actualString)
			}
		})
	}
}

// TestWrapKeepsCause は、Wrap で作った Error から、errors.Is で原因のエラーまで辿れることを確認する。
func TestWrapKeepsCause(testingT *testing.T) {
	cause := errors.New("原因のエラー")
	appError := Wrap(KindInternal, "失敗した", cause)

	if !errors.Is(appError, cause) {
		testingT.Errorf("errors.Is で原因まで辿れない")
	}
	if appError.Unwrap() != cause {
		testingT.Errorf("Unwrap が原因を返さない")
	}
}

// TestNewHasNoCause は、New で作った Error は原因を持たないことを確認する。
func TestNewHasNoCause(testingT *testing.T) {
	appError := New(KindConflict, "名前が重複している")

	if appError.Unwrap() != nil {
		testingT.Errorf("New で作った Error が原因を持っている: %v", appError.Unwrap())
	}
}

// TestKindOf は、エラーの種類を取り出せることを確認する。
// %w で包まれた Error も見つけ、Error を含まない通常のエラーは内部エラー、nil は空の種類になる。
func TestKindOf(testingT *testing.T) {
	testCases := []struct {
		name         string
		err          error
		expectedKind Kind
	}{
		{"Error はその種類", New(KindConflict, "重複"), KindConflict},
		{"%w で包まれた Error も見つける", fmt.Errorf("追加の説明: %w", New(KindNotFound, "無い")), KindNotFound},
		{"通常のエラーは内部エラー", errors.New("普通のエラー"), KindInternal},
		{"nil は空の種類", nil, Kind("")},
	}

	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			actualKind := KindOf(testCase.err)
			if actualKind != testCase.expectedKind {
				subTest.Errorf("種類が違う: 期待 %q、実際 %q", testCase.expectedKind, actualKind)
			}
		})
	}
}
