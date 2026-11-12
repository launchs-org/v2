package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"launchs/shared/apperror"
)

// TestDevAuthenticationMiddleware は、DEV_USER_ID があるときは、そのユーザーとして通し、
// 空のときは unauthorized で拒否することを確認する。
func TestDevAuthenticationMiddleware(testingT *testing.T) {
	testCases := []struct {
		name               string
		devUserID          string
		expectedUserID     string
		expectedStatusCode int
	}{
		{name: "DEV_USER_ID があれば、そのユーザーとして通す", devUserID: "user-dev", expectedUserID: "user-dev", expectedStatusCode: http.StatusOK},
		{name: "DEV_USER_ID が空なら 401", devUserID: "", expectedStatusCode: http.StatusUnauthorized},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			echoServer := echo.NewWithConfig(echo.Config{HTTPErrorHandler: NewErrorHandler(nil)})
			var actualUserID string
			echoServer.GET("/protected", func(echoContext *echo.Context) error {
				userID, userIDError := UserIDFrom(echoContext)
				if userIDError != nil {
					return userIDError
				}
				actualUserID = userID
				return echoContext.NoContent(http.StatusOK)
			}, NewDevAuthenticationMiddleware(testCase.devUserID))

			recorder := httptest.NewRecorder()
			echoServer.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/protected", nil))

			if recorder.Code != testCase.expectedStatusCode {
				subTest.Errorf("ステータスコードが違う: 期待 %d、実際 %d", testCase.expectedStatusCode, recorder.Code)
			}
			if actualUserID != testCase.expectedUserID {
				subTest.Errorf("ユーザー ID が違う: 期待 %q、実際 %q", testCase.expectedUserID, actualUserID)
			}
		})
	}
}

// TestUserIDFromWithoutAuthentication は、認証を通っていないコンテキストから取り出すと、
// unauthorized になることを確認する。
func TestUserIDFromWithoutAuthentication(testingT *testing.T) {
	echoContext := echo.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())

	_, userIDError := UserIDFrom(echoContext)

	if apperror.KindOf(userIDError) != apperror.KindUnauthorized {
		testingT.Errorf("unauthorized になるはずが、違う: %v", userIDError)
	}
}
