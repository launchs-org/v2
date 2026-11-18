package pagination

import (
	"testing"
	"time"

	"launchs/shared/apperror"
)

// TestCursorRoundTrip は、位置をカーソルにして、また位置に戻せることを確認する。
func TestCursorRoundTrip(testingT *testing.T) {
	// 往復させる位置を用意する。ミリ秒の端数を持たせて、精度が落ちないかも見る。
	originalPosition := Position{CreatedAt: time.Date(2026, 11, 13, 1, 2, 3, 456000000, time.UTC), ID: "project-abc"}

	// カーソルにして、すぐ位置に戻す。
	decodedPosition, decodeError := DecodeCursor(EncodeCursor(originalPosition))

	// 戻せなければ、続けられないのでテストを止める。
	if decodeError != nil {
		// 失敗の理由を出して、テストを止める。
		testingT.Fatalf("戻せませんでした: %v", decodeError)
	}
	// 日時または ID が元と違えば、失敗にする。
	if !decodedPosition.CreatedAt.Equal(originalPosition.CreatedAt) || decodedPosition.ID != originalPosition.ID {
		testingT.Errorf("位置が違う: 期待 %+v、実際 %+v", originalPosition, decodedPosition)
	}
}

// TestDecodeCursorRejectsInvalid は、不正なカーソルが invalid_request になることを確認する。
func TestDecodeCursorRejectsInvalid(testingT *testing.T) {
	// ケースごとの、不正なカーソルを並べる。
	testCases := []struct {
		name   string
		cursor string
	}{
		// 不正な理由の違う4種類のカーソル(base64 でない、区切りが無い、日時が数字でない、ID が空)。
		{name: "base64 ではない文字列", cursor: "!!!"},
		{name: "区切りが無い", cursor: "YWJj"},
		{name: "日時が数字ではない", cursor: "YWJjfGlk"},
		{name: "ID が空", cursor: "MTIzfA"},
	}
	// ケースを1つずつ、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// テスト対象の DecodeCursor を呼ぶ。位置は使わない。
			_, decodeError := DecodeCursor(testCase.cursor)
			// invalid_request でなければ、失敗にする。
			if apperror.KindOf(decodeError) != apperror.KindInvalidRequest {
				subTest.Errorf("invalid_request になるはずが、違う: %v", decodeError)
			}
		})
	}
}

// TestParseLimit は、limit の省略・正常・範囲外・整数でない場合を確認する。
func TestParseLimit(testingT *testing.T) {
	// ケースごとの、limit の文字列と、期待する件数・エラーの有無を並べる。
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
	// ケースを1つずつ、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// テスト対象の ParseLimit を呼ぶ。
			actualLimit, parseError := ParseLimit(testCase.limitText)
			// エラーを期待するケースでは、種類だけを確認して、このサブテストを終える。
			if testCase.expectError {
				// invalid_request でなければ、失敗にする。
				if apperror.KindOf(parseError) != apperror.KindInvalidRequest {
					subTest.Errorf("invalid_request になるはずが、違う: %v", parseError)
				}
				// エラーを期待するケースは、件数の確認をしない。
				return
			}
			// エラーが出た、または件数が期待と違えば、失敗にする。
			if parseError != nil || actualLimit != testCase.expectedLimit {
				subTest.Errorf("期待 %d、実際 %d(エラー %v)", testCase.expectedLimit, actualLimit, parseError)
			}
		})
	}
}
