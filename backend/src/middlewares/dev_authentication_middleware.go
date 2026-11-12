package middlewares

import (
	"github.com/labstack/echo/v5"

	"launchs/shared/apperror"
)

// userIDContextKey は、認証したユーザーの ID を、Echo のコンテキストに入れるときのキー。
const userIDContextKey = "user_id"

// NewDevAuthenticationMiddleware は、開発用の仮の認証をするミドルウェアを作る。
//
// 認証のコンテナがまだ無いので、環境変数 DEV_USER_ID で指定した1人のユーザーとして、
// 全てのリクエストを扱う。DEV_USER_ID が空のときは、全てのリクエストを 401 で拒否する。
// 認証のコンテナを作るときに、アクセストークンを検証するミドルウェアに差し替える。
//
// 引数:
//   - devUserID: 全てのリクエストのユーザーとして扱う ID。空なら全て拒否する。
//
// 戻り値:
//   - Echo に登録するミドルウェア。
func NewDevAuthenticationMiddleware(devUserID string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(echoContext *echo.Context) error {
			if devUserID == "" {
				return apperror.New(apperror.KindUnauthorized, "認証が必要です")
			}
			echoContext.Set(userIDContextKey, devUserID)
			return next(echoContext)
		}
	}
}

// UserIDFrom は、認証したユーザーの ID を、Echo のコンテキストから取り出す。
//
// 認証のミドルウェアを通ったあとでなければ、ID は入っていない。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - ユーザーの ID。
//   - 取り出せなかったときの、unauthorized のエラー。
func UserIDFrom(echoContext *echo.Context) (string, error) {
	userID, isString := echoContext.Get(userIDContextKey).(string)
	if !isString || userID == "" {
		return "", apperror.New(apperror.KindUnauthorized, "認証が必要です")
	}
	return userID, nil
}
