package middlewares

import (
	"launchs/shared/httpmiddleware"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"launchs/shared/apperror"
)

// TestDevAuthenticationMiddleware は、DEV_USER_ID があるときは、そのユーザーとして通し、
// 空のときは unauthorized で拒否することを確認する。
func TestDevAuthenticationMiddleware(testingT *testing.T) {
	// DEV_USER_ID の有無ごとに、期待するユーザー ID とステータスを並べる。
	testCases := []struct {
		name               string
		devUserID          string
		expectedUserID     string
		expectedStatusCode int
	}{
		{name: "DEV_USER_ID があれば、そのユーザーとして通す", devUserID: "user-dev", expectedUserID: "user-dev", expectedStatusCode: http.StatusOK},
		{name: "DEV_USER_ID が空なら 401", devUserID: "", expectedStatusCode: http.StatusUnauthorized},
	}
	// ケースごとに、サブテストを走らせる。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// 認証のエラーを、本番と同じ形式に変換するため、エラーハンドラーを設定したサーバーを作る(ログは出さないので、ロガーは nil)。
			echoServer := echo.NewWithConfig(echo.Config{HTTPErrorHandler: httpmiddleware.NewErrorHandler(nil)})
			// ハンドラーが取り出したユーザー ID を、テストの側で確認するために受け取る変数。
			var actualUserID string
			// 認証のミドルウェアを付けた保護されたルートを登録する。
			echoServer.GET("/protected", func(echoContext *echo.Context) error {
				// 認証のミドルウェアが入れたユーザー ID を取り出す。
				userID, userIDError := UserIDFrom(echoContext)
				// 取り出せなかったときは、そのエラーを返す。
				if userIDError != nil {
					return userIDError
				}
				// 取り出せたユーザー ID を、確認用の変数に記録する。
				actualUserID = userID
				// 本文の無い 200 を返す。
				return echoContext.NoContent(http.StatusOK)
			}, NewDevAuthenticationMiddleware(testCase.devUserID))

			// レスポンスを記録する入れ物を作る。
			recorder := httptest.NewRecorder()
			// ネットワークを使わずに、サーバーへリクエストを直接渡す。
			echoServer.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/protected", nil))

			// ステータスコードが期待と違うときは失敗させる。
			if recorder.Code != testCase.expectedStatusCode {
				subTest.Errorf("ステータスコードが違う: 期待 %d、実際 %d", testCase.expectedStatusCode, recorder.Code)
			}
			// ハンドラーが受け取ったユーザー ID が期待と違うときは失敗させる(拒否のときは、空のままのはず)。
			if actualUserID != testCase.expectedUserID {
				subTest.Errorf("ユーザー ID が違う: 期待 %q、実際 %q", testCase.expectedUserID, actualUserID)
			}
		})
	}
}

// TestUserIDFromWithoutAuthentication は、認証を通っていないコンテキストから取り出すと、
// unauthorized になることを確認する。
func TestUserIDFromWithoutAuthentication(testingT *testing.T) {
	// 認証のミドルウェアを通っていない、空のコンテキストを作る。
	echoContext := echo.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())

	// ユーザー ID を取り出そうとする。ID は使わないので捨て、エラーだけ受け取る。
	_, userIDError := UserIDFrom(echoContext)

	// エラーの種類が unauthorized でなければ失敗させる。
	if apperror.KindOf(userIDError) != apperror.KindUnauthorized {
		testingT.Errorf("unauthorized になるはずが、違う: %v", userIDError)
	}
}
