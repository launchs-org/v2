package middlewares

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"

	"launchs/shared/httpmiddleware"
	"launchs/shared/jwtverify"
)

// fakeVerifier は、決まったトークンだけを通す、テスト用の検証。
type fakeVerifier struct {
	// validToken は、通すトークン。
	validToken string
}

// Verify は、validToken と同じ文字列なら、中身を返し、違えばエラーを返す。
//
// 引数:
//   - requestContext: 使わない。
//   - tokenText: 検証するトークン。
//
// 戻り値:
//   - トークンの中身。違えばエラー。
func (verifier fakeVerifier) Verify(requestContext context.Context, tokenText string) (*jwtverify.Claims, error) {
	// 一致するかだけを見る。
	if tokenText != verifier.validToken {
		return nil, errors.New("無効")
	}
	return &jwtverify.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "userid-alice"}, Name: "alice", Role: "admin"}, nil
}

// TestAccessTokenMiddleware は、正しい Bearer トークンだけを通してユーザーの情報をコンテキストに入れ、
// それ以外(ヘッダ無し、形が違う、検証に失敗)は 401(WWW-Authenticate つき)で拒否することを確かめる。
//
// 引数:
//   - testingT: テストの状態。
func TestAccessTokenMiddleware(testingT *testing.T) {
	// ケースごとの、Authorization ヘッダと、期待するステータスを並べる。
	testCases := []struct {
		name               string
		authorization      string
		expectedStatusCode int
	}{
		{"正しいトークンは通る", "Bearer good-token", http.StatusOK},
		{"Bearer は大文字小文字を区別しない", "bearer good-token", http.StatusOK},
		{"ヘッダが無いと 401", "", http.StatusUnauthorized},
		{"Bearer 以外の方式は 401", "Basic good-token", http.StatusUnauthorized},
		{"トークンが空だと 401", "Bearer ", http.StatusUnauthorized},
		{"スキームだけだと 401", "Bearer", http.StatusUnauthorized},
		{"検証に失敗すると 401", "Bearer bad-token", http.StatusUnauthorized},
	}
	// ケースごとに、サブテストを走らせる。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// エラーを本番と同じ形式に変換するサーバーを作り、保護されたルートを登録する。
			echoServer := echo.NewWithConfig(echo.Config{HTTPErrorHandler: httpmiddleware.NewErrorHandler(nil)})
			var actualUserID, actualRole string
			echoServer.GET("/protected", func(echoContext *echo.Context) error {
				// 認証のミドルウェアが入れた、ユーザーの ID と役割を取り出して記録する。
				userID, userIDError := UserIDFrom(echoContext)
				if userIDError != nil {
					return userIDError
				}
				actualUserID, actualRole = userID, UserRoleFrom(echoContext)
				return echoContext.NoContent(http.StatusOK)
			}, NewAccessTokenMiddleware(fakeVerifier{validToken: "good-token"}))
			// リクエストを直接渡す。
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if testCase.authorization != "" {
				request.Header.Set("Authorization", testCase.authorization)
			}
			recorder := httptest.NewRecorder()
			echoServer.ServeHTTP(recorder, request)
			// ステータスを確かめる。
			if recorder.Code != testCase.expectedStatusCode {
				subTest.Fatalf("ステータスが違う: 期待 %d、実際 %d", testCase.expectedStatusCode, recorder.Code)
			}
			// 通ったときは、ユーザーの情報が入る。拒否のときは、入らず、WWW-Authenticate が付く。
			if testCase.expectedStatusCode == http.StatusOK {
				if actualUserID != "userid-alice" || actualRole != "admin" {
					subTest.Errorf("ユーザーの情報が違う: %q %q", actualUserID, actualRole)
				}
			} else if actualUserID != "" || recorder.Header().Get("WWW-Authenticate") != "Bearer" {
				subTest.Errorf("拒否のとき、情報は入らず、WWW-Authenticate が付くはず: %q %q", actualUserID, recorder.Header().Get("WWW-Authenticate"))
			}
		})
	}
}
