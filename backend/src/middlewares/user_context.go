package middlewares

import (
	"github.com/labstack/echo/v5"

	"launchs/shared/apperror"
)

// userIDContextKey は、認証したユーザーの ID を、Echo のコンテキストに入れるときのキー。
const userIDContextKey = "user_id"

// userNameContextKey は、認証したユーザーの名前を、Echo のコンテキストに入れるときのキー。
const userNameContextKey = "user_name"

// userRoleContextKey は、認証したユーザーの役割(user か admin)を、Echo のコンテキストに入れるときのキー。
const userRoleContextKey = "user_role"

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
	// コンテキストから値を取り出し、文字列として扱えるか確認する(入っていないと isString は false になる)。
	userID, isString := echoContext.Get(userIDContextKey).(string)
	// 入っていない、または空のときは、認証を通っていないとみなす。
	if !isString || userID == "" {
		// unauthorized のエラーを返す。
		return "", apperror.New(apperror.KindUnauthorized, "認証が必要です")
	}
	// 取り出せたので、ユーザー ID を返す。
	return userID, nil
}

// UserRoleFrom は、認証したユーザーの役割(user か admin)を、Echo のコンテキストから取り出す。
//
// 開発用の仮の認証では、役割は入らないので、空になる(管理者としては扱わない)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 役割。入っていなければ空。
func UserRoleFrom(echoContext *echo.Context) string {
	// 入っていれば文字列で、無ければ空になる。
	role, _ := echoContext.Get(userRoleContextKey).(string)
	return role
}
