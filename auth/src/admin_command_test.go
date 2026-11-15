package main

import (
	"strings"
	"testing"
)

// TestReadAdminPassword は、パスワードを、環境変数があればそれから、無ければ標準入力の1行から読むこと(改行は除く)、
// どちらも空ならエラーになることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestReadAdminPassword(testingT *testing.T) {
	// 環境変数が無いときは、標準入力の1行(改行を除く)。
	testingT.Setenv(adminPasswordEnvironmentName, "")
	if plain, readError := readAdminPassword(strings.NewReader("from stdin\r\nsecond line\n")); readError != nil || plain != "from stdin" {
		testingT.Fatalf("標準入力の1行のはず: %q %v", plain, readError)
	}
	// 末尾に改行が無くても読める。
	if plain, readError := readAdminPassword(strings.NewReader("no newline")); readError != nil || plain != "no newline" {
		testingT.Fatalf("改行が無くても読めるはず: %q %v", plain, readError)
	}
	// 空なら、エラー。
	if _, readError := readAdminPassword(strings.NewReader("")); readError == nil {
		testingT.Fatal("空はエラーのはず")
	}
	if _, readError := readAdminPassword(strings.NewReader("\n")); readError == nil {
		testingT.Fatal("空の行はエラーのはず")
	}
	// 環境変数があれば、標準入力より優先する。
	testingT.Setenv(adminPasswordEnvironmentName, "from env")
	if plain, readError := readAdminPassword(strings.NewReader("from stdin\n")); readError != nil || plain != "from env" {
		testingT.Fatalf("環境変数が優先のはず: %q %v", plain, readError)
	}
}

// TestRunAdminCommandUsage は、引数の使い方が違うとき、データベースに触れる前にエラーになることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestRunAdminCommandUsage(testingT *testing.T) {
	// 引数が足りない、多い、知らないサブコマンドは、全てエラー。
	for _, arguments := range [][]string{{}, {"create"}, {"create", "a", "b"}, {"delete", "alice"}} {
		if commandError := runAdminCommand(arguments); commandError == nil || !strings.Contains(commandError.Error(), "使い方") {
			testingT.Errorf("%v は使い方のエラーのはず: %v", arguments, commandError)
		}
	}
}
