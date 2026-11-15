package model

import (
	"testing"
	"time"

	"launchs/shared/apperror"
)

// TestUserBanUnbanPromote は、停止・解除・管理者への昇格の規則(自分自身は停止できない、二重は conflict、停止中は昇格できない)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestUserBanUnbanPromote(testingT *testing.T) {
	// 自分自身は、停止できない(状態は変わらない)。
	admin := User{ID: "userid-admin", Status: UserStatusActive, Role: RoleAdmin}
	if banError := admin.Ban("userid-admin"); apperror.KindOf(banError) != apperror.KindInvalidRequest || admin.Status != UserStatusActive {
		testingT.Fatalf("自分自身は invalid_request のはず: %v", banError)
	}
	// 他のユーザーは停止でき、二重は conflict。
	user := User{ID: "userid-a", Status: UserStatusActive, Role: RoleUser}
	if banError := user.Ban("userid-admin"); banError != nil || user.Status != UserStatusBanned {
		testingT.Fatalf("停止できるはず: %v", banError)
	}
	if twiceError := user.Ban("userid-admin"); apperror.KindOf(twiceError) != apperror.KindConflict {
		testingT.Fatalf("二重は conflict のはず: %v", twiceError)
	}
	// 停止中は、管理者にできない。解除できて、二重の解除は conflict。
	if promoteError := user.PromoteToAdmin(); apperror.KindOf(promoteError) != apperror.KindConflict || user.Role != RoleUser {
		testingT.Fatalf("停止中は昇格できないはず: %v", promoteError)
	}
	if unbanError := user.Unban(); unbanError != nil || user.Status != UserStatusActive {
		testingT.Fatalf("解除できるはず: %v", unbanError)
	}
	if twiceError := user.Unban(); apperror.KindOf(twiceError) != apperror.KindConflict {
		testingT.Fatalf("停止中でなければ conflict のはず: %v", twiceError)
	}
	// 昇格できて、二重は conflict。管理の操作は、使える状態の管理者だけができる。
	if user.CanAdminister() {
		testingT.Fatal("一般ユーザーは、管理できないはず")
	}
	if promoteError := user.PromoteToAdmin(); promoteError != nil || user.Role != RoleAdmin || !user.CanAdminister() {
		testingT.Fatalf("昇格できるはず: %v", promoteError)
	}
	if twiceError := user.PromoteToAdmin(); apperror.KindOf(twiceError) != apperror.KindConflict {
		testingT.Fatalf("二重は conflict のはず: %v", twiceError)
	}
	user.Status = UserStatusBanned
	if user.CanAdminister() {
		testingT.Fatal("停止中の管理者は、管理できないはず")
	}
}

// TestInvitationStatusAndRevoke は、招待の状態(取り消し > 使用済み > 期限切れ > 有効)の判定と、有効なものだけ取り消せることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestInvitationStatusAndRevoke(testingT *testing.T) {
	// 基準の時刻と、時刻のポインタを用意する。
	now := time.Now()
	earlier := now.Add(-time.Hour)
	// 状態の判定(複数の条件が重なるときは、取り消し、使用済み、期限切れの順に優先する)。
	testCases := []struct {
		name       string
		invitation Invitation
		expected   string
	}{
		{"有効", Invitation{ExpiresAt: now.Add(time.Hour)}, InvitationStatusActive},
		{"期限切れ", Invitation{ExpiresAt: earlier}, InvitationStatusExpired},
		{"期限ちょうどは期限切れ", Invitation{ExpiresAt: now}, InvitationStatusExpired},
		{"使用済み", Invitation{ExpiresAt: now.Add(time.Hour), UsedAt: &earlier}, InvitationStatusUsed},
		{"使用済みで期限切れ", Invitation{ExpiresAt: earlier, UsedAt: &earlier}, InvitationStatusUsed},
		{"取り消し", Invitation{ExpiresAt: now.Add(time.Hour), RevokedAt: &earlier}, InvitationStatusRevoked},
		{"取り消しは、使用済みより優先", Invitation{ExpiresAt: now.Add(time.Hour), UsedAt: &earlier, RevokedAt: &earlier}, InvitationStatusRevoked},
	}
	for _, testCase := range testCases {
		if actual := testCase.invitation.Status(now); actual != testCase.expected {
			testingT.Errorf("%s: 期待 %s、実際 %s", testCase.name, testCase.expected, actual)
		}
	}
	// 有効なものは取り消せ、日時が入る。有効でないものは conflict で、変わらない。
	active := Invitation{ExpiresAt: now.Add(time.Hour)}
	if revokeError := active.Revoke(now); revokeError != nil || active.RevokedAt == nil {
		testingT.Fatalf("取り消せるはず: %v", revokeError)
	}
	if twiceError := active.Revoke(now); apperror.KindOf(twiceError) != apperror.KindConflict {
		testingT.Fatalf("二重は conflict のはず: %v", twiceError)
	}
	used := Invitation{ExpiresAt: now.Add(time.Hour), UsedAt: &earlier}
	if usedError := used.Revoke(now); apperror.KindOf(usedError) != apperror.KindConflict || used.RevokedAt != nil {
		testingT.Fatalf("使用済みは conflict で、変わらないはず: %v", usedError)
	}
}
