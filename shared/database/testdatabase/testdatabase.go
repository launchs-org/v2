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
	testingT.Helper()

	adminURL := os.Getenv(adminDatabaseURLName)
	if adminURL == "" {
		testingT.Skipf("環境変数 %s が無いので、データベースを使うテストを飛ばす", adminDatabaseURLName)
	}

	adminDatabase, adminError := database.Open(adminURL)
	if adminError != nil {
		testingT.Fatalf("管理用の接続を開けなかった: %v", adminError)
	}

	databaseName := "test_" + uuid.NewString()[:8]
	if createError := adminDatabase.Exec("CREATE DATABASE " + databaseName).Error; createError != nil {
		testingT.Fatalf("テスト用のデータベースを作れなかった: %v", createError)
	}

	testURL, urlError := replaceDatabaseName(adminURL, databaseName)
	if urlError != nil {
		testingT.Fatalf("接続先を作れなかった: %v", urlError)
	}
	testDatabase, openError := database.Open(testURL)
	if openError != nil {
		testingT.Fatalf("テスト用のデータベースに接続できなかった: %v", openError)
	}

	testingT.Cleanup(func() {
		_ = database.Close(testDatabase)
		// 接続が残っていても消せるように FORCE を付ける。
		adminDatabase.Exec("DROP DATABASE IF EXISTS " + databaseName + " WITH (FORCE)")
		_ = database.Close(adminDatabase)
	})

	applyMigrations(testingT, testDatabase)
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
	parsedURL, parseError := url.Parse(rawURL)
	if parseError != nil {
		return "", fmt.Errorf("接続先を解釈できなかった: %w", parseError)
	}
	parsedURL.Path = "/" + databaseName
	return parsedURL.String(), nil
}

// applyMigrations は、migrations/app の .up.sql を、番号の順に流す。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - testDatabase: 流す先のデータベース。
func applyMigrations(testingT *testing.T, testDatabase *gorm.DB) {
	testingT.Helper()

	_, thisFile, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	migrationFiles, globError := filepath.Glob(filepath.Join(repositoryRoot, "migrations", "app", "*.up.sql"))
	if globError != nil || len(migrationFiles) == 0 {
		testingT.Fatalf("マイグレーションのファイルが見つからない: %v", globError)
	}
	sort.Strings(migrationFiles)

	for _, migrationFile := range migrationFiles {
		migrationSQL, readError := os.ReadFile(migrationFile)
		if readError != nil {
			testingT.Fatalf("マイグレーションを読めなかった: %v", readError)
		}
		if execError := testDatabase.Exec(string(migrationSQL)).Error; execError != nil {
			testingT.Fatalf("マイグレーション %s を流せなかった: %v", filepath.Base(migrationFile), execError)
		}
	}
}
