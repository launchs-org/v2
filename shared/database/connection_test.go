package database

import (
	"strings"
	"testing"
)

// TestOpenFailsWhenDatabaseIsUnreachable は、接続できない接続先を渡すと、
// エラーになり、そのエラーに接続先のパスワードが含まれないことを確認する。
func TestOpenFailsWhenDatabaseIsUnreachable(testingT *testing.T) {
	// 使われていないポート(1番)を指定して、すぐ接続に失敗させる。
	// 接続できない接続先で Open を呼ぶ。
	gormDatabase, openError := Open("postgres://user:secret-password@127.0.0.1:1/app?sslmode=disable&connect_timeout=1")

	// エラーが無いなら、失敗するべき所で成功しているので、テストを止める。
	if openError == nil {
		testingT.Fatalf("エラーになるはずなのに、エラーが無い")
	}
	// 失敗したときに、使えない接続を返していないかを確認する。
	if gormDatabase != nil {
		testingT.Errorf("エラーのときは、nil を返すはず")
	}
	// エラーのメッセージから、パスワードが漏れていないかを確認する。
	if strings.Contains(openError.Error(), "secret-password") {
		testingT.Errorf("エラーにパスワードが含まれている: %v", openError)
	}
}
