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
	gormDatabase, openError := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		Logger: gormlogger.Discard,
		// 一意制約などの違反を、gorm.ErrDuplicatedKey などの共通のエラーに変換する。
		TranslateError: true,
	})
	if openError != nil {
		return nil, fmt.Errorf("データベースに接続できなかった: %w", openError)
	}

	sqlDatabase, sqlError := gormDatabase.DB()
	if sqlError != nil {
		return nil, fmt.Errorf("データベースの接続を取り出せなかった: %w", sqlError)
	}

	checkContext, cancelCheck := context.WithTimeout(context.Background(), connectionCheckTimeout)
	defer cancelCheck()
	if pingError := sqlDatabase.PingContext(checkContext); pingError != nil {
		_ = sqlDatabase.Close()
		return nil, fmt.Errorf("データベースと通信できなかった: %w", pingError)
	}
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
	sqlDatabase, sqlError := gormDatabase.DB()
	if sqlError != nil {
		return fmt.Errorf("データベースの接続を取り出せなかった: %w", sqlError)
	}
	return sqlDatabase.Close()
}
