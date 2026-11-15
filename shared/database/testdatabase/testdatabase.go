// Package testdatabase は、テストのために、使い捨てのデータベースを用意する。
//
// テストごとに、PostgreSQL のサーバーの中に新しいデータベースを作り、
// migrations/app のマイグレーションを流して、テストが終わると消す。
// これで、テスト同士が影響し合わず、開発用のデータも汚さない。
package testdatabase

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"launchs/shared/database"
)

// adminDatabaseURLName は、データベースを作れる接続先を渡す環境変数の名前。
// 設定されていないとき、このパッケージを使うテストは飛ばされる。
const adminDatabaseURLName = "TEST_ADMIN_DATABASE_URL"

// New は、マイグレーション済みの、使い捨てのデータベースを作って返す。
//
// 環境変数 TEST_ADMIN_DATABASE_URL が無いときは、テストを飛ばす。
// 作ったデータベースは、テストが終わるとき(Cleanup)に接続を閉じて消す。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - 使い捨てのデータベースの接続。
func New(testingT *testing.T) *gorm.DB {
	// app データベースのマイグレーションを流した、データベースを返す。
	return NewWithMigrations(testingT, "app")
}

// NewWithMigrations は、指定したフォルダ(migrations/<フォルダ>)のマイグレーションを流した、使い捨てのデータベースを作る。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - migrationDirectory: migrations 直下のフォルダの名前("app" や "auth")。
//
// 戻り値:
//   - マイグレーション済みの、使い捨てのデータベースへの接続。テストが終わると、削除される。
func NewWithMigrations(testingT *testing.T, migrationDirectory string) *gorm.DB {
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()

	// データベースを作れる接続先を、環境変数から読む。
	adminURL := os.Getenv(adminDatabaseURLName)
	// 接続先が無い環境(データベース無し)では、失敗にせず、テストを飛ばす。
	if adminURL == "" {
		testingT.Skipf("環境変数 %s が無いので、データベースを使うテストを飛ばす", adminDatabaseURLName)
	}

	// データベースを作るための、管理用の接続を開く。
	adminDatabase, adminError := database.Open(adminURL)
	// 開けなければ、テストを続けられないので止める。
	if adminError != nil {
		testingT.Fatalf("管理用の接続を開けませんでした: %v", adminError)
	}

	// 他のテストと重ならないよう、UUID の先頭を使ったデータベース名にする。
	databaseName := "test_" + uuid.NewString()[:8]
	// テスト用のデータベースを作る。名前は自分で作った英数字なので、文字列の連結でよい。
	if createError := adminDatabase.Exec("CREATE DATABASE " + databaseName).Error; createError != nil {
		testingT.Fatalf("テスト用のデータベースを作れませんでした: %v", createError)
	}

	// 管理用の接続先の、データベース名だけを入れ替えて、テスト用の接続先にする。
	testURL, urlError := replaceDatabaseName(adminURL, databaseName)
	// 接続先を作れなければ、テストを続けられないので止める。
	if urlError != nil {
		testingT.Fatalf("接続先を作れませんでした: %v", urlError)
	}
	// テスト用のデータベースに接続する。
	testDatabase, openError := database.Open(testURL)
	// 接続できなければ、テストを続けられないので止める。
	if openError != nil {
		testingT.Fatalf("テスト用のデータベースに接続できませんでした: %v", openError)
	}

	// テストが終わるときに、作ったデータベースを片付ける処理を登録する。
	testingT.Cleanup(func() {
		// 先にテスト用の接続を閉じる。閉じるときのエラーは、片付けでは扱えないので捨てる。
		_ = database.Close(testDatabase)
		// 接続が残っていても消せるように FORCE を付ける。
		// テスト用のデータベースを消す。
		adminDatabase.Exec("DROP DATABASE IF EXISTS " + databaseName + " WITH (FORCE)")
		// 最後に、管理用の接続を閉じる。
		_ = database.Close(adminDatabase)
	})

	// 作ったばかりの空のデータベースに、テーブルを作るマイグレーションを流す。
	applyMigrations(testingT, testDatabase, migrationDirectory)
	// 使える状態になったデータベースを返す。
	return testDatabase
}

// replaceDatabaseName は、接続先の URL の、データベース名だけを入れ替える。
//
// 引数:
//   - rawURL: 元の接続先。
//   - databaseName: 新しいデータベース名。
//
// 戻り値:
//   - 入れ替えた接続先。
//   - 元の接続先を解釈できなかったときのエラー。
func replaceDatabaseName(rawURL string, databaseName string) (string, error) {
	// 文字列の URL を、部品に分けて扱えるようにする。
	parsedURL, parseError := url.Parse(rawURL)
	// 解釈できなければ、エラーにして返す。
	if parseError != nil {
		return "", fmt.Errorf("接続先を解釈できませんでした: %w", parseError)
	}
	// URL のパスの部分が、データベース名なので、新しい名前に入れ替える。
	parsedURL.Path = "/" + databaseName
	// 部品を、文字列の URL に戻して返す。
	return parsedURL.String(), nil
}

// applyMigrations は、migrations/app の .up.sql を、番号の順に流す。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - testDatabase: 流す先のデータベース。
func applyMigrations(testingT *testing.T, testDatabase *gorm.DB, migrationDirectory string) {
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()

	// このソースファイルの場所を知る。テストを実行するフォルダに左右されないようにするため。
	_, thisFile, _, _ := runtime.Caller(0)
	// このファイルは shared/database/testdatabase にあるので、3つ上がリポジトリのルート。
	repositoryRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	// マイグレーションの .up.sql を全て集める。
	migrationFiles, globError := filepath.Glob(filepath.Join(repositoryRoot, "migrations", migrationDirectory, "*.up.sql"))
	// 1つも無いのは、設定の間違いなので、テストを止める。
	if globError != nil || len(migrationFiles) == 0 {
		testingT.Fatalf("マイグレーションのファイルが見つからない: %v", globError)
	}
	// 番号の順(ファイル名の順)に流せるよう、並べ替える。
	sort.Strings(migrationFiles)

	// 1つずつ、順に流す。
	for _, migrationFile := range migrationFiles {
		// マイグレーションの SQL を読む。
		migrationSQL, readError := os.ReadFile(migrationFile)
		// 読めなければ、テストを止める。
		if readError != nil {
			testingT.Fatalf("マイグレーションを読めませんでした: %v", readError)
		}
		// SQL をデータベースで実行する。失敗したら、どのファイルかが分かるように止める。
		if execError := testDatabase.Exec(string(migrationSQL)).Error; execError != nil {
			testingT.Fatalf("マイグレーション %s を流せませんでした: %v", filepath.Base(migrationFile), execError)
		}
	}
}
