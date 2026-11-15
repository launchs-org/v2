package repository

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"launchs/auth/src/model"
	"launchs/shared/apperror"
	"launchs/shared/database/testdatabase"
)

// newAuthDatabase は、auth のマイグレーションを流した、使い捨てのデータベースを作る。
//
// 引数:
//   - testing: テストの状態。
//
// 戻り値:
//   - データベースの接続。
func newAuthDatabase(testing *testing.T) *gorm.DB {
	// auth のマイグレーションを流す。
	return testdatabase.NewWithMigrations(testing, "auth")
}

// newUser は、テスト用のユーザーを作る。
//
// 引数:
//   - name: ユーザー名。
//
// 戻り値:
//   - 保存前のユーザー。
func newUser(name string) *model.User {
	// 名前から、ID と小文字の名前を作る。
	return &model.User{ID: model.UserIDPrefix + name, UserName: name, UserNameLower: model.NormalizeUserName(name), PasswordHash: "hash", Status: model.UserStatusActive, Role: model.RoleUser}
}

// TestUserRepository は、ユーザーの保存(大文字小文字を区別しない重複は conflict)、検索、招待の消費(同じトランザクション)を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestUserRepository(testing *testing.T) {
	ctx := context.Background()
	database := newAuthDatabase(testing)
	userRepository := NewUserRepository(database)
	// 保存して、小文字の名前と ID で、探せる。
	if createError := userRepository.Create(ctx, newUser("Alice"), ""); createError != nil {
		testing.Fatalf("保存できるはず: %v", createError)
	}
	found, exists, findError := userRepository.FindByNameLower(ctx, "alice")
	if findError != nil || !exists || found.UserName != "Alice" || found.Role != model.RoleUser || !found.IsActive() {
		testing.Errorf("探せるはず: %v %v %+v", findError, exists, found)
	}
	if byID, byIDExists, _ := userRepository.FindByID(ctx, "userid-Alice"); !byIDExists || byID.UserName != "Alice" {
		testing.Error("ID で探せるはず")
	}
	// 大文字小文字だけが違う名前は、conflict。
	duplicate := newUser("ALICE")
	duplicate.ID = "userid-other"
	var appError *apperror.Error
	if duplicateError := userRepository.Create(ctx, duplicate, ""); !errors.As(duplicateError, &appError) || appError.Kind != apperror.KindConflict {
		testing.Errorf("conflict のはず: %v", duplicateError)
	}
	// 空の条件では、どのユーザーも見つからない。
	for _, empty := range []func() (model.User, bool, error){
		func() (model.User, bool, error) { return userRepository.FindByNameLower(ctx, "") },
		func() (model.User, bool, error) { return userRepository.FindByID(ctx, "") },
	} {
		if _, emptyExists, _ := empty(); emptyExists {
			testing.Error("空の条件では見つからないはず")
		}
	}
	// ハッシュの更新。
	if updateError := userRepository.UpdatePasswordHash(ctx, "userid-Alice", "new-hash"); updateError != nil {
		testing.Fatal(updateError)
	}
	if updated, _, _ := userRepository.FindByID(ctx, "userid-Alice"); updated.PasswordHash != "new-hash" {
		testing.Error("ハッシュが更新されるはず")
	}
	if userRepository.UpdatePasswordHash(ctx, "", "x") == nil || userRepository.UpdatePasswordHash(ctx, "userid-Alice", "") == nil {
		testing.Error("空の値では更新しないはず")
	}
	// 招待: 有効な招待で作れ、使用済みになる。同じ招待は、もう使えない。期限切れ、無い招待も使えない。
	database.Exec("INSERT INTO invitations (code_hash, expires_at) VALUES ('valid', now() + interval '1 day'), ('expired', now() - interval '1 day')")
	if inviteError := userRepository.Create(ctx, newUser("bob"), "valid"); inviteError != nil {
		testing.Fatalf("招待で作れるはず: %v", inviteError)
	}
	for name, code := range map[string]string{"使用済み": "valid", "期限切れ": "expired", "無い": "nothing"} {
		inviteError := userRepository.Create(ctx, newUser("carol-"+name), code)
		if !errors.As(inviteError, &appError) || appError.Kind != apperror.KindInvalidRequest {
			testing.Errorf("%s の招待は invalid_request のはず: %v", name, inviteError)
		}
	}
	// 名前が重なって失敗したときは、招待を使用済みにしない(同じトランザクション)。
	database.Exec("INSERT INTO invitations (code_hash, expires_at) VALUES ('unused', now() + interval '1 day')")
	if conflictError := userRepository.Create(ctx, newUser("alice"), "unused"); conflictError == nil {
		testing.Fatal("conflict のはず")
	}
	var usedCount int64
	database.Table("invitations").Where("code_hash = 'unused' AND used_at IS NOT NULL").Count(&usedCount)
	if usedCount != 0 {
		testing.Error("失敗したときは、招待を使用済みにしないはず")
	}
}

// TestRefreshTokenRepository は、refresh token の保存・有効の判定・無効化・期限・掃除を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestRefreshTokenRepository(testing *testing.T) {
	ctx := context.Background()
	database := newAuthDatabase(testing)
	_ = NewUserRepository(database).Create(ctx, newUser("alice"), "")
	repository := NewRefreshTokenRepository(database)
	// 有効なトークンは、持ち主が分かる。
	if createError := repository.Create(ctx, "jti-1", "userid-alice", time.Now().Add(time.Hour)); createError != nil {
		testing.Fatal(createError)
	}
	if owner, active, _ := repository.FindActiveOwner(ctx, "jti-1"); !active || owner != "userid-alice" {
		testing.Error("有効なはず")
	}
	// 知らない、空の jti は、有効ではない。
	if _, active, _ := repository.FindActiveOwner(ctx, "jti-nothing"); active {
		testing.Error("知らない jti は有効でないはず")
	}
	if _, active, _ := repository.FindActiveOwner(ctx, ""); active {
		testing.Error("空の jti は有効でないはず")
	}
	// 無効にすると、有効でなくなり、もう一度は無効にできない。
	if revoked, _ := repository.Revoke(ctx, "jti-1"); !revoked {
		testing.Error("無効にできるはず")
	}
	if _, active, _ := repository.FindActiveOwner(ctx, "jti-1"); active {
		testing.Error("無効にしたので、有効でないはず")
	}
	if again, _ := repository.Revoke(ctx, "jti-1"); again {
		testing.Error("2回目は無効にできないはず")
	}
	if empty, _ := repository.Revoke(ctx, ""); empty {
		testing.Error("空の jti は対象が無いはず")
	}
	// 期限切れは、有効でなく、無効にもできず、掃除で消える。
	_ = repository.Create(ctx, "jti-expired", "userid-alice", time.Now().Add(-time.Minute))
	if _, active, _ := repository.FindActiveOwner(ctx, "jti-expired"); active {
		testing.Error("期限切れは有効でないはず")
	}
	if revoked, _ := repository.Revoke(ctx, "jti-expired"); revoked {
		testing.Error("期限切れは無効にできないはず")
	}
	if deleted, _ := repository.DeleteExpired(ctx); deleted != 1 {
		testing.Errorf("期限切れの1件が消えるはず: %d", deleted)
	}
	// ユーザーの全てを無効にする(他のユーザーのものには、影響しない)。
	_ = NewUserRepository(database).Create(ctx, newUser("bob"), "")
	for _, record := range []struct{ jti, user string }{{"a1", "userid-alice"}, {"a2", "userid-alice"}, {"b1", "userid-bob"}} {
		_ = repository.Create(ctx, record.jti, record.user, time.Now().Add(time.Hour))
	}
	if count, _ := repository.RevokeAllForUser(ctx, "userid-alice"); count != 2 {
		testing.Errorf("alice の2件が無効になるはず: %d", count)
	}
	if _, active, _ := repository.FindActiveOwner(ctx, "b1"); !active {
		testing.Error("bob のものは、有効なままのはず")
	}
	if count, _ := repository.RevokeAllForUser(ctx, ""); count != 0 {
		testing.Error("空の ID では何もしないはず")
	}
}

// TestLoginFailureRepository は、失敗の回数、上限でのロック(回数は0に戻る)、ロック中は数えない、成功での消去、掃除を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestLoginFailureRepository(testing *testing.T) {
	ctx := context.Background()
	database := newAuthDatabase(testing)
	repository := NewLoginFailureRepository(database)
	// 上限(3回)までは、ロックされない。3回目で、ロックされる。
	for attempt := 1; attempt <= 2; attempt++ {
		_ = repository.RecordFailure(ctx, "alice", 3, time.Minute)
		if locked, _ := repository.IsLocked(ctx, "alice"); locked {
			testing.Fatalf("%d回目では、ロックされないはず", attempt)
		}
	}
	_ = repository.RecordFailure(ctx, "alice", 3, time.Minute)
	if locked, _ := repository.IsLocked(ctx, "alice"); !locked {
		testing.Fatal("3回目でロックされるはず")
	}
	// 他の名前は、ロックされない。
	if locked, _ := repository.IsLocked(ctx, "bob"); locked {
		testing.Error("他の名前はロックされないはず")
	}
	// ロックされたとき、回数は 0 に戻る。
	var failedCount int
	database.Raw("SELECT failed_count FROM login_failures WHERE user_name_lower = 'alice'").Scan(&failedCount)
	if failedCount != 0 {
		testing.Errorf("回数は0に戻るはず: %d", failedCount)
	}
	// ロックの時間が過ぎると、ロックは終わる。
	database.Exec("UPDATE login_failures SET locked_until = now() - interval '1 second'")
	if locked, _ := repository.IsLocked(ctx, "alice"); locked {
		testing.Error("時間が過ぎたら、ロックは終わるはず")
	}
	// 成功すると、記録が消える。
	_ = repository.RecordFailure(ctx, "alice", 3, time.Minute)
	_ = repository.Reset(ctx, "alice")
	var rows int64
	database.Table("login_failures").Count(&rows)
	if rows != 0 {
		testing.Error("記録が消えるはず")
	}
	// 上限が1なら、最初の失敗でロックされる。
	_ = repository.RecordFailure(ctx, "carol", 1, time.Minute)
	if locked, _ := repository.IsLocked(ctx, "carol"); !locked {
		testing.Error("上限1なら、最初でロックされるはず")
	}
	// 掃除: ロックが終わっていて古い行だけを消す(ロック中のものは残る)。
	_ = repository.RecordFailure(ctx, "old", 3, time.Minute)
	database.Exec("UPDATE login_failures SET updated_at = now() - interval '2 days' WHERE user_name_lower IN ('old', 'carol')")
	if deleted, _ := repository.DeleteStale(ctx, 24*time.Hour); deleted != 1 {
		testing.Errorf("ロック中でない古い1件だけが消えるはず: %d", deleted)
	}
	if err := repository.Reset(ctx, ""); err != nil {
		testing.Error("空の名前は何もしないはず")
	}
}

// TestRateLimitRepository は、時間枠の中の上限、超えたときの待ち時間(Retry-After)、IP と種類ごとの独立、掃除を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestRateLimitRepository(testing *testing.T) {
	ctx := context.Background()
	database := newAuthDatabase(testing)
	repository := NewRateLimitRepository(database)
	// 上限(3回)までは許され、4回目から許されない。
	for attempt := 1; attempt <= 3; attempt++ {
		if allowed, _, hitError := repository.Hit(ctx, "login", "203.0.113.1", 3, time.Minute); !allowed || hitError != nil {
			testing.Fatalf("%d回目は許されるはず: %v", attempt, hitError)
		}
	}
	allowed, retryAfter, _ := repository.Hit(ctx, "login", "203.0.113.1", 3, time.Minute)
	if allowed || retryAfter <= 0 || retryAfter > time.Minute {
		testing.Fatalf("許されず、待ち時間が、0より大きく1分以内のはず: %v %v", allowed, retryAfter)
	}
	// 許されなかった試行は、記録されない(大量に送られても、行が増えない)。
	for attempt := 0; attempt < 20; attempt++ {
		_, _, _ = repository.Hit(ctx, "login", "203.0.113.1", 3, time.Minute)
	}
	var recorded int64
	database.Raw("SELECT count(*) FROM rate_limit_events WHERE kind = 'login' AND ip = '203.0.113.1'").Scan(&recorded)
	if recorded != 3 {
		testing.Errorf("記録は、許された3回だけのはず: %d", recorded)
	}
	// 同時に来ても、上限を超えて許さない(別の IP で、同時に20回送って、許されるのは3回だけ)。
	var concurrentAllowed atomic.Int64
	var group sync.WaitGroup
	for attempt := 0; attempt < 20; attempt++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if allowed, _, _ := repository.Hit(ctx, "login", "203.0.113.9", 3, time.Minute); allowed {
				concurrentAllowed.Add(1)
			}
		}()
	}
	group.Wait()
	if concurrentAllowed.Load() != 3 {
		testing.Errorf("同時でも、許されるのは3回だけのはず: %d", concurrentAllowed.Load())
	}
	// 別の IP、別の種類は、独立している。
	if other, _, _ := repository.Hit(ctx, "login", "203.0.113.2", 3, time.Minute); !other {
		testing.Error("別の IP は許されるはず")
	}
	if otherKind, _, _ := repository.Hit(ctx, "signup", "203.0.113.1", 3, time.Minute); !otherKind {
		testing.Error("別の種類は許されるはず")
	}
	// 古い試行が時間枠から外れると、また許される(最初の3回を、2分前にずらす)。
	database.Exec("UPDATE rate_limit_events SET occurred_at = now() - interval '2 minutes' WHERE kind = 'login' AND ip = '203.0.113.1'")
	if recovered, _, _ := repository.Hit(ctx, "login", "203.0.113.1", 3, time.Minute); !recovered {
		testing.Error("時間枠から外れたら、許されるはず")
	}
	// 待ち時間は、(超えた分の)古い方の試行が、時間枠から外れるまでの時間。
	database.Exec("DELETE FROM rate_limit_events")
	database.Exec("INSERT INTO rate_limit_events (kind, ip, occurred_at) VALUES ('login', 'x', now() - interval '50 seconds'), ('login', 'x', now() - interval '10 seconds')")
	blocked, waiting, _ := repository.Hit(ctx, "login", "x", 2, time.Minute)
	if blocked || waiting < 8*time.Second || waiting > 11*time.Second {
		testing.Errorf("最も古い試行(50秒前)が外れるまで、約10秒のはず: %v %v", blocked, waiting)
	}
	// 掃除: 古い記録を消す。
	database.Exec("UPDATE rate_limit_events SET occurred_at = now() - interval '3 hours' WHERE ip = 'x' AND id = (SELECT min(id) FROM rate_limit_events)")
	if deleted, _ := repository.DeleteOlderThan(ctx, 2*time.Hour); deleted != 1 {
		testing.Errorf("古い1件が消えるはず: %d", deleted)
	}
}

// TestLoginEventRepository は、履歴の保存、ユーザーごとの新しい順の取得(件数の上限)、古い履歴の掃除を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestLoginEventRepository(testing *testing.T) {
	ctx := context.Background()
	database := newAuthDatabase(testing)
	userRepository := NewUserRepository(database)
	_ = userRepository.Create(ctx, newUser("alice"), "")
	_ = userRepository.Create(ctx, newUser("bob"), "")
	repository := NewLoginEventRepository(database)
	aliceID, bobID, reason := "userid-alice", "userid-bob", "wrong_password"
	// alice に3件、bob に1件、存在しない名前(user_id なし)に1件。
	for _, event := range []model.LoginEvent{
		{UserID: &aliceID, UserNameLower: "alice", Kind: "signup", Result: "success", IP: "1.1.1.1"},
		{UserID: &aliceID, UserNameLower: "alice", Kind: "login", Result: "failure", Reason: &reason, IP: "1.1.1.2"},
		{UserID: &aliceID, UserNameLower: "alice", Kind: "login", Result: "success", IP: "1.1.1.3", UserAgent: "agent"},
		{UserID: &bobID, UserNameLower: "bob", Kind: "login", Result: "success", IP: "2.2.2.2"},
		{UserNameLower: "ghost", Kind: "login", Result: "failure", Reason: &reason, IP: "3.3.3.3"},
	} {
		if createError := repository.Create(ctx, event); createError != nil {
			testing.Fatalf("保存できるはず: %v", createError)
		}
	}
	// alice の履歴は、新しい順に3件。上限で絞れる。他のユーザーのものは混ざらない。
	events, _ := repository.ListByUser(ctx, aliceID, 10)
	if len(events) != 3 || events[0].IP != "1.1.1.3" || events[2].IP != "1.1.1.1" || events[0].UserAgent != "agent" || events[1].Reason == nil || *events[1].Reason != reason {
		testing.Fatalf("新しい順の3件のはず: %+v", events)
	}
	if limited, _ := repository.ListByUser(ctx, aliceID, 2); len(limited) != 2 || limited[0].IP != "1.1.1.3" {
		testing.Error("上限で絞れるはず")
	}
	if empty, _ := repository.ListByUser(ctx, "", 10); len(empty) != 0 {
		testing.Error("空の ID では何も返さないはず")
	}
	// 古い履歴だけを消す。
	database.Exec("UPDATE login_events SET occurred_at = now() - interval '200 days' WHERE ip IN ('1.1.1.1', '3.3.3.3')")
	if deleted, _ := repository.DeleteOlderThan(ctx, 180*24*time.Hour); deleted != 2 {
		testing.Errorf("古い2件が消えるはず: %d", deleted)
	}
	// ユーザーを消すと、そのユーザーの履歴も消える。
	database.Exec("DELETE FROM users WHERE id = 'userid-bob'")
	var bobRows int64
	database.Table("login_events").Where("user_name_lower = 'bob'").Count(&bobRows)
	if bobRows != 0 {
		testing.Error("ユーザーを消すと、履歴も消えるはず")
	}
}
