package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"launchs/shared/apperror"
	"launchs/shared/jwtverify"
)

// fakeVerifier は、決まった中身を返す(または失敗する)、検証のテスト用の代役。
type fakeVerifier struct {
	claims *jwtverify.Claims
}

// Verify は、中身があればそれを返し、無ければ失敗する。
func (verifier fakeVerifier) Verify(_ context.Context, _ string) (*jwtverify.Claims, error) {
	// 中身が無ければ、検証に失敗した扱いにする。
	if verifier.claims == nil {
		return nil, errors.New("不正なトークン")
	}
	return verifier.claims, nil
}

// TestAdminAuthMiddleware は、管理者だけが通り、それ以外は 401 か 403 になることを確かめる。
func TestAdminAuthMiddleware(testingT *testing.T) {
	// 呼ばれたかを覚えるハンドラーと、ケースの一覧を用意する。
	cases := []struct {
		name       string
		header     string
		claims     *jwtverify.Claims
		wantKind   apperror.Kind
		wantPassed bool
	}{
		{name: "管理者は通る", header: "Bearer token", claims: &jwtverify.Claims{Role: "admin"}, wantPassed: true},
		{name: "一般のユーザーは 403", header: "Bearer token", claims: &jwtverify.Claims{Role: "user"}, wantKind: apperror.KindForbidden},
		{name: "ヘッダーが無ければ 401", header: "", claims: &jwtverify.Claims{Role: "admin"}, wantKind: apperror.KindUnauthorized},
		{name: "Bearer でなければ 401", header: "Basic abc", claims: &jwtverify.Claims{Role: "admin"}, wantKind: apperror.KindUnauthorized},
		{name: "検証に失敗したら 401", header: "Bearer token", claims: nil, wantKind: apperror.KindUnauthorized},
	}
	for _, testCase := range cases {
		testingT.Run(testCase.name, func(testingT *testing.T) {
			// リクエストを作る。
			request := httptest.NewRequest(http.MethodGet, "/admin-api/overview", nil)
			if testCase.header != "" {
				request.Header.Set("Authorization", testCase.header)
			}
			echoContext := echo.NewContext(request, httptest.NewRecorder())
			// ミドルウェアを通して、ハンドラーが呼ばれたかと、エラーの種類を調べる。
			passed := false
			handler := NewAdminAuthMiddleware(fakeVerifier{claims: testCase.claims})(func(*echo.Context) error {
				passed = true
				return nil
			})
			handlerError := handler(echoContext)
			if passed != testCase.wantPassed {
				testingT.Fatalf("通過: 期待 %v、実際 %v", testCase.wantPassed, passed)
			}
			if !testCase.wantPassed {
				if apperror.KindOf(handlerError) != testCase.wantKind {
					testingT.Fatalf("エラーの種類が違う: %v", handlerError)
				}
			}
		})
	}
}
