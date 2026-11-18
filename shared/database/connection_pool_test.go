package database_test

import (
	"testing"

	"launchs/shared/database/testdatabase"
)

// TestOpenLimitsConnections は、開いた接続に、数の上限が付くことを確認する(本番で、PostgreSQL の接続数を使い切らないため)。
func TestOpenLimitsConnections(testingT *testing.T) {
	// 試験用のデータベースは、Open で開かれる。
	database := testdatabase.New(testingT)
	sqlDatabase, sqlError := database.DB()
	if sqlError != nil {
		testingT.Fatal(sqlError)
	}
	if stats := sqlDatabase.Stats(); stats.MaxOpenConnections != 10 {
		testingT.Errorf("開く接続の上限は 10 のはず: %d", stats.MaxOpenConnections)
	}
}
