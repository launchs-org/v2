package repository

import (
	"context"
	"testing"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/database/testdatabase"
)

// TestSecurityRepository は、ブロックの追加・一覧・解除、不審な IP の集計、監査ログの追記と一覧を確かめる。
func TestSecurityRepository(testingT *testing.T) {
	// auth のマイグレーション済みの、使い捨てのデータベースを用意する。
	database := testdatabase.NewWithMigrations(testingT, "auth")
	requestContext := context.Background()
	securityRepository := NewSecurityRepository(database)

	// ログイン履歴を入れる: 198.51.100.7 は、3 つの名前で 6 回失敗。203.0.113.1 は、2 回だけ失敗。
	for index := 0; index < 6; index++ {
		names := []string{"a", "b", "c"}
		if execError := database.Exec("INSERT INTO login_events (user_name_lower, kind, result, reason, ip) VALUES (?, 'login', 'failure', 'unknown_user', '198.51.100.7')", names[index%3]).Error; execError != nil {
			testingT.Fatal(execError)
		}
	}
	for index := 0; index < 2; index++ {
		if execError := database.Exec("INSERT INTO login_events (user_name_lower, kind, result, reason, ip) VALUES ('x', 'login', 'failure', 'wrong_password', '203.0.113.1')").Error; execError != nil {
			testingT.Fatal(execError)
		}
	}
	// 閾値 5 では、1 つだけ出る(失敗 6、名前 3)。まだブロックされていない。
	suspicious, suspiciousError := securityRepository.SuspiciousIPs(requestContext, 24, 5)
	if suspiciousError != nil || len(suspicious) != 1 || suspicious[0].IP != "198.51.100.7" || suspicious[0].Failures != 6 || suspicious[0].UserNames != 3 || suspicious[0].Blocked {
		testingT.Fatalf("不審な IP が 1 つのはず: %+v %v", suspicious, suspiciousError)
	}

	// 範囲でブロックすると、一覧に出て、不審な IP にも「ブロック中」が付く。期限切れは出ない。
	expired := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	for _, block := range []BlockedIP{
		{ID: "block-1", Network: "198.51.100.0/24", Reason: "scan", CreatedBy: "admin-1", CreatedByName: "root"},
		{ID: "block-2", Network: "192.0.2.5/32", CreatedBy: "admin-1", ExpiresAt: &future},
		{ID: "block-3", Network: "192.0.2.9/32", CreatedBy: "admin-1", ExpiresAt: &expired},
	} {
		if createError := securityRepository.CreateBlock(requestContext, block); createError != nil {
			testingT.Fatal(createError)
		}
	}
	blocks, listError := securityRepository.ListBlocked(requestContext)
	if listError != nil || len(blocks) != 2 {
		testingT.Fatalf("有効なブロックは 2 件のはず: %+v %v", blocks, listError)
	}
	if again, _ := securityRepository.SuspiciousIPs(requestContext, 24, 5); len(again) != 1 || !again[0].Blocked {
		testingT.Errorf("ブロック中が付くはず: %+v", again)
	}
	// 解除すると、一覧から消える。無い ID は not_found。
	if network, deleteError := securityRepository.DeleteBlock(requestContext, "block-1"); deleteError != nil || network != "198.51.100.0/24" {
		testingT.Errorf("解除できるはず: %q %v", network, deleteError)
	}
	if _, missingError := securityRepository.DeleteBlock(requestContext, "block-1"); apperror.KindOf(missingError) != apperror.KindNotFound {
		testingT.Errorf("無い ID は not_found のはず: %v", missingError)
	}

	// 監査ログを追記して、新しい順に、絞り込みとページングで読める。
	for _, action := range []string{"ip_block", "ip_unblock", "ip_block"} {
		if auditError := securityRepository.CreateAudit(requestContext, AuditLog{ActorID: "admin-1", ActorName: "root", Action: action, Target: "198.51.100.0/24", IP: "10.0.0.1"}); auditError != nil {
			testingT.Fatal(auditError)
		}
	}
	all, next, auditListError := securityRepository.ListAudit(requestContext, 2, nil, "")
	if auditListError != nil || len(all) != 2 || next == nil {
		testingT.Fatalf("2 件と次のページがあるはず: %+v %v", all, auditListError)
	}
	rest, _, _ := securityRepository.ListAudit(requestContext, 2, next, "")
	if len(rest) != 1 {
		testingT.Errorf("残りは 1 件のはず: %+v", rest)
	}
	if blocksOnly, _, _ := securityRepository.ListAudit(requestContext, 10, nil, "ip_block"); len(blocksOnly) != 2 {
		testingT.Errorf("action で絞り込めるはず: %+v", blocksOnly)
	}
}
