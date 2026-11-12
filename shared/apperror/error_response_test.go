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

	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			actualStatus, actualResponse := Describe(testCase.err)

			if actualStatus != testCase.expectedStatus {
				subTest.Errorf("ステータスコードが違う: 期待 %d、実際 %d", testCase.expectedStatus, actualStatus)
			}
			if actualResponse.Error.Code != testCase.expectedCode {
				subTest.Errorf("code が違う: 期待 %q、実際 %q", testCase.expectedCode, actualResponse.Error.Code)
			}
			if actualResponse.Error.Message != testCase.expectedMessage {
				subTest.Errorf("message が違う: 期待 %q、実際 %q", testCase.expectedMessage, actualResponse.Error.Message)
			}
		})
	}
}

// TestDescribeDoesNotLeakCause は、Error の原因(内部の情報)が、レスポンスに含まれないことを確認する。
func TestDescribeDoesNotLeakCause(testingT *testing.T) {
	secretCause := errors.New("dial tcp 10.0.0.5:5432: connection refused")
	err := Wrap(KindInternal, "保存に失敗した", secretCause)

	_, response := Describe(err)
	responseJSON, marshalError := json.Marshal(response)
	if marshalError != nil {
		testingT.Fatalf("JSON にできない: %v", marshalError)
	}

	if strings.Contains(string(responseJSON), "10.0.0.5") {
		testingT.Errorf("レスポンスに内部の情報が含まれている: %s", responseJSON)
	}
}

// TestErrorResponseJSON は、エラーレスポンスの JSON が、共通ルールの形式
// ({"error": {"code": "...", "message": "..."}})になることを確認する。
func TestErrorResponseJSON(testingT *testing.T) {
	response := buildErrorResponse(KindNotFound, "見つからない")

	actualJSON, marshalError := json.Marshal(response)
	if marshalError != nil {
		testingT.Fatalf("JSON にできない: %v", marshalError)
	}

	expectedJSON := `{"error":{"code":"not_found","message":"見つからない"}}`
	if string(actualJSON) != expectedJSON {
		testingT.Errorf("JSON が違う: 期待 %s、実際 %s", expectedJSON, actualJSON)
	}
}
