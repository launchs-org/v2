package middlewares

import (
	"github.com/labstack/echo/v5"

	"launchs/shared/apperror"
)

// NewDevAuthenticationMiddleware は、開発用の仮の認証をするミドルウェアを作る。
//
// 認証のコンテナ(auth)を使わない開発用。環境変数 DEV_USER_ID で指定した1人のユーザーとして、
// 全てのリクエストを扱う。DEV_USER_ID が空のときは、全てのリクエストを 401 で拒否する。
// 本番では使わない(AUTH_REQUIRE_JWKS=true のときは、設定の読み込みが、DEV_USER_ID を拒否する)。
//
// 引数:
//   - devUserID: 全てのリクエストのユーザーとして扱う ID。空なら全て拒否する。
//
// 戻り値:
//   - Echo に登録するミドルウェア。
func NewDevAuthenticationMiddleware(devUserID string) echo.MiddlewareFunc {
	// Echo の形式に合わせて、次のハンドラーを受け取り、認証を挟んだハンドラーを返す関数を返す。
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		// リクエストごとに呼ばれる、認証を挟んだハンドラーを返す。
		return func(echoContext *echo.Context) error {
			// 開発用のユーザーが設定されていないときは、誰でもアクセスできないよう、拒否する。
			if devUserID == "" {
				// unauthorized のエラーを返し、エラーハンドラーに 401 へ変換させる。
				return apperror.New(apperror.KindUnauthorized, "認証が必要です")
			}
			// 後ろのハンドラーが UserIDFrom で取り出せるよう、ユーザー ID をコンテキストに入れる。
			echoContext.Set(userIDContextKey, devUserID)
			// 認証できたので、次のハンドラーに処理を渡す。
			return next(echoContext)
		}
	}
}
