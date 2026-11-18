// Package database は、PostgreSQL への接続を作る。
//
// バックエンドとワーカーが、同じ接続の作り方を使うために shared に置く。
package database

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// connectionCheckTimeout は、接続できるかの確認(Ping)を待つ最長の時間。
const connectionCheckTimeout = 5 * time.Second

// Open は、PostgreSQL に接続して、Gorm のデータベースを返す。
//
// 接続したあと、実際に通信できるか(Ping)まで確認する。SQL は Gorm のログに出さない
// (値にパスワードなどが含まれうるため)。
//
// 引数:
//   - databaseURL: 接続先(postgres://user:password@host:port/dbname?sslmode=...)。
//
// 戻り値:
//   - 接続した Gorm のデータベース。
//   - 接続や確認に失敗したときのエラー。エラーのメッセージに接続先の文字列は含めない。
func Open(databaseURL string) (*gorm.DB, error) {
	// Gorm で PostgreSQL を開く。この時点では、まだ通信できるかは分からない。
	gormDatabase, openError := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		Logger: gormlogger.Discard,
		// 一意制約などの違反を、gorm.ErrDuplicatedKey などの共通のエラーに変換する。
		TranslateError: true,
	})
	// 開けなかったときは、接続先の文字列を含めず、原因だけを包んで返す。
	if openError != nil {
		return nil, fmt.Errorf("データベースに接続できませんでした: %w", openError)
	}

	// Ping と Close のために、Gorm の下にある標準の接続を取り出す。
	sqlDatabase, sqlError := gormDatabase.DB()
	// 取り出せなかったときは、エラーにして返す。
	if sqlError != nil {
		return nil, fmt.Errorf("データベースの接続を取り出せませんでした: %w", sqlError)
	}

	// 確認が長引いてプログラムが止まらないよう、時間切れのある context を作る。
	checkContext, cancelCheck := context.WithTimeout(context.Background(), connectionCheckTimeout)
	// 関数を抜けるときに context を解放する。
	defer cancelCheck()
	// 実際に通信できるかを確認する。失敗したときは、開いた接続を閉じて、接続を残さない。
	if pingError := sqlDatabase.PingContext(checkContext); pingError != nil {
		// 閉じるときのエラーは、ここでは返す意味が無いので捨てる。
		_ = sqlDatabase.Close()
		// 通信できなかったことを、原因を包んで返す。
		return nil, fmt.Errorf("データベースと通信できませんでした: %w", pingError)
	}
	// 通信まで確認できたので、データベースを返す。
	return gormDatabase, nil
}

// Close は、データベースの接続を閉じる。
//
// 引数:
//   - gormDatabase: Open で作ったデータベース。
//
// 戻り値:
//   - 閉じるときのエラー。
func Close(gormDatabase *gorm.DB) error {
	// 閉じるために、Gorm の下にある標準の接続を取り出す。
	sqlDatabase, sqlError := gormDatabase.DB()
	// 取り出せなかったときは、エラーにして返す。
	if sqlError != nil {
		return fmt.Errorf("データベースの接続を取り出せませんでした: %w", sqlError)
	}
	// 接続を閉じて、そのエラーをそのまま返す。
	return sqlDatabase.Close()
}
