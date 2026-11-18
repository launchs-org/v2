package middlewares

import (
	"github.com/labstack/echo/v5"

	"launchs/shared/apperror"
)

// AdminRole は、管理者の役割の名前(access token の role の値)。
const AdminRole = "admin"

// RequireAdmin は、管理者だけを通すミドルウェア。access token の role が admin でなければ、403 にする。
//
// backend はユーザーの DB を持たないので、role は、access token の値を使う(管理者でなくなった人が、最大15分、通ってしまうことがある)。
// 開発用の仮の認証(DEV_USER_ID)では、role が入らないので、全て 403 になる。
// 認証のミドルウェアの後ろに付けること(認証されていないときは、先に 401 になる)。
//
// 引数:
//   - next: 次のハンドラー。
//
// 戻り値:
//   - 管理者のときだけ、次のハンドラーを呼ぶハンドラー。
func RequireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	// ハンドラーを返す。
	return func(echoContext *echo.Context) error {
		// 管理者でなければ、権限が無い(403)。
		if UserRoleFrom(echoContext) != AdminRole {
			return apperror.New(apperror.KindForbidden, "この操作は、管理者だけが行えます")
		}
		// 管理者なので、次のハンドラーに処理を渡す。
		return next(echoContext)
	}
}
