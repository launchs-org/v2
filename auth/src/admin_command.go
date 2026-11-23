package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"launchs/auth/src/config"
	"launchs/auth/src/password"
	"launchs/auth/src/repository"
	"launchs/auth/src/service"
	sharedconfig "launchs/shared/config"
	"launchs/shared/database"
	"launchs/shared/logger"
)

// adminPasswordEnvironmentName は、管理者のパスワードを渡す環境変数の名前(無ければ、標準入力から1行読む)。
const adminPasswordEnvironmentName = "AUTH_ADMIN_PASSWORD"

// runAdminCommand は、管理者を作る・昇格させるコマンド(`auth admin create <userName>`、`auth admin promote <userName>`)を実行する。
//
// 最初の管理者は、API では作れないので、このコマンドで作る。サーバーとは別のプロセスで、同じデータベースを使う。
// パスワードは、コマンドの引数に書くと履歴に残るので、環境変数 AUTH_ADMIN_PASSWORD か、標準入力(1行)で渡す。
//
// 引数:
//   - arguments: "admin" より後ろの引数("create" か "promote" と、ユーザー名)。
//
// 戻り値:
//   - 使い方が違う、設定や入力が不正、すでに存在するなどのときのエラー。
func runAdminCommand(arguments []string) error {
	// 使い方を確かめる。
	if len(arguments) != 2 || (arguments[0] != "create" && arguments[0] != "promote") {
		return errors.New("使い方: auth admin create <userName>(パスワードは、環境変数 " + adminPasswordEnvironmentName + " か標準入力) / auth admin promote <userName>")
	}
	subcommand, userName := arguments[0], arguments[1]
	// 設定を読み、データベースに接続する。
	authConfig, configError := config.LoadAuthConfig(sharedconfig.NewProcessEnvironmentReader())
	if configError != nil {
		return fmt.Errorf("設定を読み込めませんでした: %w", configError)
	}
	appDatabase, databaseError := database.Open(authConfig.DatabaseURL)
	if databaseError != nil {
		return databaseError
	}
	defer func() { _ = database.Close(appDatabase) }()
	// 管理のユースケースを組み立てる(トークンは使わないので nil)。
	adminService := service.NewAdminService(repository.NewUserRepository(appDatabase), repository.NewInvitationRepository(appDatabase), repository.NewLoginFailureRepository(appDatabase),
		logger.New(os.Stderr, "auth-admin", authConfig.LogLevel), password.NewHasher(password.DefaultParameters()), nil)
	// コマンドからの操作も、監査ログに残す。
	adminService.SetAuditStore(repository.NewAuditRepository(appDatabase))
	// 昇格のときは、パスワードは要らない。
	if subcommand == "promote" {
		if promoteError := adminService.PromoteToAdmin(context.Background(), userName); promoteError != nil {
			return promoteError
		}
		fmt.Println("管理者にした:", userName)
		return nil
	}
	// 作成のときは、パスワードを読む。
	plainPassword, passwordError := readAdminPassword(os.Stdin)
	if passwordError != nil {
		return passwordError
	}
	user, createError := adminService.CreateAdmin(context.Background(), userName, plainPassword)
	if createError != nil {
		return createError
	}
	fmt.Println("管理者を作成した:", user.UserName, user.ID)
	return nil
}

// readAdminPassword は、管理者のパスワードを、環境変数か、標準入力の1行から読む。
//
// 引数:
//   - input: 標準入力。
//
// 戻り値:
//   - パスワード(末尾の改行は除く)。空なら、エラー。
func readAdminPassword(input io.Reader) (string, error) {
	// 環境変数があれば、それを使う。
	plainPassword := os.Getenv(adminPasswordEnvironmentName)
	// 無ければ、標準入力の1行を読む。
	if plainPassword == "" {
		line, readError := bufio.NewReader(input).ReadString('\n')
		if readError != nil && !errors.Is(readError, io.EOF) {
			return "", fmt.Errorf("パスワードを読めませんでした: %w", readError)
		}
		plainPassword = strings.TrimRight(line, "\r\n")
	}
	// 空のパスワードは、使えない。
	if plainPassword == "" {
		return "", errors.New("パスワードが空です(環境変数 " + adminPasswordEnvironmentName + " か標準入力で渡してください)")
	}
	return plainPassword, nil
}
