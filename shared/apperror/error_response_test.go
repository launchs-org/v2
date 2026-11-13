package apperror

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestDescribe は、エラーが、種類に応じたステータスコードとレスポンスのボディに変換されることを確認する。
func TestDescribe(testingT *testing.T) {
	// ケースごとの、エラーと、期待するステータス・code・メッセージを並べる。
	testCases := []struct {
		name            string
		err             error
		expectedStatus  int
		expectedCode    string
		expectedMessage string
	}{
		{
			name:            "Error はその種類とメッセージを使う",
			err:             New(KindConflict, "名前が重複している"),
			expectedStatus:  http.StatusConflict,
			expectedCode:    "conflict",
			expectedMessage: "名前が重複している",
		},
		{
			name:            "%w で包まれた Error も、その種類とメッセージを使う",
			err:             fmt.Errorf("追加の説明: %w", New(KindNotFound, "プロジェクトが見つからない")),
			expectedStatus:  http.StatusNotFound,
			expectedCode:    "not_found",
			expectedMessage: "プロジェクトが見つからない",
		},
		{
			name:            "通常のエラーは内部エラーとして、一般的なメッセージを返す",
			err:             errors.New("pq: password authentication failed"),
			expectedStatus:  http.StatusInternalServerError,
			expectedCode:    "internal_error",
			expectedMessage: internalErrorMessage,
		},
		{
			name:            "メッセージが空の Error は、一般的なメッセージを返す",
			err:             New(KindInternal, ""),
			expectedStatus:  http.StatusInternalServerError,
			expectedCode:    "internal_error",
			expectedMessage: internalErrorMessage,
		},
	}

	// ケースを1つずつ、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// テスト対象の Describe を呼ぶ。
			actualStatus, actualResponse := Describe(testCase.err)

			// ステータスコードが違えば、失敗にする。
			if actualStatus != testCase.expectedStatus {
				subTest.Errorf("ステータスコードが違う: 期待 %d、実際 %d", testCase.expectedStatus, actualStatus)
			}
			// code が違えば、失敗にする。
			if actualResponse.Error.Code != testCase.expectedCode {
				subTest.Errorf("code が違う: 期待 %q、実際 %q", testCase.expectedCode, actualResponse.Error.Code)
			}
			// メッセージが違えば、失敗にする。
			if actualResponse.Error.Message != testCase.expectedMessage {
				subTest.Errorf("message が違う: 期待 %q、実際 %q", testCase.expectedMessage, actualResponse.Error.Message)
			}
		})
	}
}

// TestDescribeDoesNotLeakCause は、Error の原因(内部の情報)が、レスポンスに含まれないことを確認する。
func TestDescribeDoesNotLeakCause(testingT *testing.T) {
	// 利用者に見せてはいけない、内部の情報(接続先の IP アドレス)を含む原因を用意する。
	secretCause := errors.New("dial tcp 10.0.0.5:5432: connection refused")
	// その原因を持つ Error を作る。
	err := Wrap(KindInternal, "保存に失敗した", secretCause)

	// レスポンスに変換する(ステータスコードは使わない)。
	_, response := Describe(err)
	// 実際に返す形に近づけるため、JSON にする。
	responseJSON, marshalError := json.Marshal(response)
	// JSON にできなければ、続けられないのでテストを止める。
	if marshalError != nil {
		// 失敗の理由を出して、テストを止める。
		testingT.Fatalf("JSON にできない: %v", marshalError)
	}

	// JSON に内部の情報が含まれていたら、失敗にする。
	if strings.Contains(string(responseJSON), "10.0.0.5") {
		testingT.Errorf("レスポンスに内部の情報が含まれている: %s", responseJSON)
	}
}

// TestErrorResponseJSON は、エラーレスポンスの JSON が、共通ルールの形式
// ({"error": {"code": "...", "message": "..."}})になることを確認する。
func TestErrorResponseJSON(testingT *testing.T) {
	// 種類とメッセージから、レスポンスのボディを組み立てる。
	response := buildErrorResponse(KindNotFound, "見つからない")

	// ボディを JSON にする。
	actualJSON, marshalError := json.Marshal(response)
	// JSON にできなければ、続けられないのでテストを止める。
	if marshalError != nil {
		// 失敗の理由を出して、テストを止める。
		testingT.Fatalf("JSON にできない: %v", marshalError)
	}

	// 共通ルールで決まっている JSON の形を、期待値にする。
	expectedJSON := `{"error":{"code":"not_found","message":"見つからない"}}`
	// 期待した JSON と違えば、失敗にする。
	if string(actualJSON) != expectedJSON {
		testingT.Errorf("JSON が違う: 期待 %s、実際 %s", expectedJSON, actualJSON)
	}
}
