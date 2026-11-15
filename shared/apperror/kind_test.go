package apperror

import (
	"net/http"
	"testing"
)

// TestKindStatusCode は、全ての Kind が共通ルールどおりの HTTP ステータスコードに
// 対応し、未知の Kind は 500 になることを確認する。
func TestKindStatusCode(testingT *testing.T) {
	// ケースごとの、Kind と、期待するステータスコードを並べる。
	testCases := []struct {
		name           string
		kind           Kind
		expectedStatus int
	}{
		{"invalid_request は 400", KindInvalidRequest, http.StatusBadRequest},
		{"unauthorized は 401", KindUnauthorized, http.StatusUnauthorized},
		{"not_found は 404", KindNotFound, http.StatusNotFound},
		{"conflict は 409", KindConflict, http.StatusConflict},
		{"forbidden は 403", KindForbidden, http.StatusForbidden},
		{"payload_too_large は 413", KindPayloadTooLarge, http.StatusRequestEntityTooLarge},
		{"too_many_requests は 429", KindTooManyRequests, http.StatusTooManyRequests},
		{"internal_error は 500", KindInternal, http.StatusInternalServerError},
		{"未知の Kind は 500", Kind("unknown_kind"), http.StatusInternalServerError},
		{"空の Kind は 500", Kind(""), http.StatusInternalServerError},
	}

	// ケースを1つずつ、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// テスト対象の StatusCode を呼ぶ。
			actualStatus := testCase.kind.StatusCode()
			// 期待したステータスコードと違えば、失敗にする。
			if actualStatus != testCase.expectedStatus {
				subTest.Errorf("ステータスコードが違う: 期待 %d、実際 %d", testCase.expectedStatus, actualStatus)
			}
		})
	}
}
