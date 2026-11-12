package pagination

import (
	"testing"
	"time"

	"launchs/shared/apperror"
)

// TestCursorRoundTrip は、位置をカーソルにして、また位置に戻せることを確認する。
func TestCursorRoundTrip(testingT *testing.T) {
	originalPosition := Position{CreatedAt: time.Date(2026, 11, 13, 1, 2, 3, 456000000, time.UTC), ID: "project-abc"}

	decodedPosition, decodeError := DecodeCursor(EncodeCursor(originalPosition))

	if decodeError != nil {
		testingT.Fatalf("戻せませんでした: %v", decodeError)
	}
	if !decodedPosition.CreatedAt.Equal(originalPosition.CreatedAt) || decodedPosition.ID != originalPosition.ID {
		testingT.Errorf("位置が違う: 期待 %+v、実際 %+v", originalPosition, decodedPosition)
	}
}

// TestDecodeCursorRejectsInvalid は、不正なカーソルが invalid_request になることを確認する。
func TestDecodeCursorRejectsInvalid(testingT *testing.T) {
	testCases := []struct {
		name   string
		cursor string
	}{
		{name: "base64 ではない文字列", cursor: "!!!"},
		{name: "区切りが無い", cursor: "YWJj"},
		{name: "日時が数字ではない", cursor: "YWJjfGlk"},
		{name: "ID が空", cursor: "MTIzfA"},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			_, decodeError := DecodeCursor(testCase.cursor)
			if apperror.KindOf(decodeError) != apperror.KindInvalidRequest {
				subTest.Errorf("invalid_request になるはずが、違う: %v", decodeError)
			}
		})
	}
}

// TestParseLimit は、limit の省略・正常・範囲外・整数でない場合を確認する。
func TestParseLimit(testingT *testing.T) {
	testCases := []struct {
		name          string
		limitText     string
		expectedLimit int
		expectError   bool
	}{
		{name: "省略すると既定の20", limitText: "", expectedLimit: 20},
		{name: "1は最小で通る", limitText: "1", expectedLimit: 1},
		{name: "100は最大で通る", limitText: "100", expectedLimit: 100},
		{name: "0はエラー", limitText: "0", expectError: true},
		{name: "101はエラー", limitText: "101", expectError: true},
		{name: "整数でないとエラー", limitText: "abc", expectError: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			actualLimit, parseError := ParseLimit(testCase.limitText)
			if testCase.expectError {
				if apperror.KindOf(parseError) != apperror.KindInvalidRequest {
					subTest.Errorf("invalid_request になるはずが、違う: %v", parseError)
				}
				return
			}
			if parseError != nil || actualLimit != testCase.expectedLimit {
				subTest.Errorf("期待 %d、実際 %d(エラー %v)", testCase.expectedLimit, actualLimit, parseError)
			}
		})
	}
}
