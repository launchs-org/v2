package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"launchs/auth/src/model"
	"launchs/auth/src/service"
)

// mustJSON は、値を JSON のバイト列にする(テスト用。失敗しない値だけを渡す)。
//
// 引数:
//   - value: JSON にする値。
//
// 戻り値:
//   - JSON のバイト列。
func mustJSON(value any) []byte {
	// 変換して返す(失敗は、テストの書き間違いなので、握りつぶさず、空にする)。
	encoded, _ := json.Marshal(value)
	return encoded
}

// adminSession は、管理者としてログインした状態(access token)。
type adminSession struct {
	userID      string
	accessToken string
}

// accessTokenFor は、ユーザーのサインアップ(またはログイン)から、access token を取る。
//
// 引数:
//   - testing: テストの状態。
//   - environment: テストの環境。
//   - userName: ユーザー名。
//   - signUp: true ならサインアップ、false ならログインする。
//   - ip: クライアントの IP。
//
// 戻り値:
//   - ユーザーの ID と、access token。
func accessTokenFor(testing *testing.T, environment *testEnvironment, userName string, signUp bool, ip string) (string, string) {
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testing.Helper()
	// サインアップかログインで、refresh token を取る。
	var refreshResponse response
	if signUp {
		refreshResponse = signup(testing, environment, userName, goodPassword, ip)
	} else {
		refreshResponse = login(testing, environment, userName, goodPassword, ip)
	}
	if refreshResponse.status != http.StatusOK {
		testing.Fatalf("トークンを取れるはず: %d %v", refreshResponse.status, refreshResponse.body)
	}
	// access token に替えて、持ち主の ID を、/me で確かめる。
	accessResponse := call(testing, environment, http.MethodPost, "/auth/refresh", "", refreshResponse.body["token"].(string), ip)
	accessToken := accessResponse.body["token"].(string)
	me := call(testing, environment, http.MethodGet, "/auth/me", "", accessToken, ip)
	return me.body["userId"].(string), accessToken
}

// newAdminSession は、管理者を作って(コマンドと同じ CreateAdmin)、ログインする。
//
// 引数:
//   - testing: テストの状態。
//   - environment: テストの環境。
//   - userName: 管理者のユーザー名。
//
// 戻り値:
//   - 管理者のセッション。
func newAdminSession(testing *testing.T, environment *testEnvironment, userName string) adminSession {
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testing.Helper()
	// コマンドと同じ経路で、管理者を作る。
	if _, createError := environment.admin.CreateAdmin(context.Background(), userName, goodPassword); createError != nil {
		testing.Fatalf("管理者を作れるはず: %v", createError)
	}
	userID, accessToken := accessTokenFor(testing, environment, userName, false, "203.0.113.50")
	return adminSession{userID: userID, accessToken: accessToken}
}

// TestAdminAuthorization は、管理 API が、トークン無し・不正は 401、一般ユーザーは 403、管理者は 200 であること、
// access token の role ではなく、DB の最新の状態で判定すること(停止された管理者、降格された管理者は 403)を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestAdminAuthorization(testing *testing.T) {
	// 管理者と、一般ユーザーを用意する。
	environment := newTestEnvironment(testing, nil)
	admin := newAdminSession(testing, environment, "root-admin")
	_, userToken := accessTokenFor(testing, environment, "alice", true, "203.0.113.1")
	// 管理 API の全てのパスが、同じ判定になる。
	paths := []struct{ method, path string }{
		{http.MethodGet, "/auth/admin/users"}, {http.MethodGet, "/auth/admin/users/userid-x"},
		{http.MethodPost, "/auth/admin/users/userid-x/ban"}, {http.MethodPost, "/auth/admin/users/userid-x/unban"}, {http.MethodPost, "/auth/admin/users/userid-x/unlock"},
		{http.MethodPost, "/auth/admin/invitations"}, {http.MethodGet, "/auth/admin/invitations"}, {http.MethodDelete, "/auth/admin/invitations/invitation-x"},
	}
	for _, endpoint := range paths {
		if noToken := call(testing, environment, endpoint.method, endpoint.path, "", "", "203.0.113.2"); noToken.status != http.StatusUnauthorized {
			testing.Errorf("%s %s: トークン無しは 401 のはず: %d", endpoint.method, endpoint.path, noToken.status)
		}
		if badToken := call(testing, environment, endpoint.method, endpoint.path, "", userToken+"x", "203.0.113.2"); badToken.status != http.StatusUnauthorized {
			testing.Errorf("%s %s: 不正なトークンは 401 のはず: %d", endpoint.method, endpoint.path, badToken.status)
		}
		if forbidden := call(testing, environment, endpoint.method, endpoint.path, "", userToken, "203.0.113.2"); forbidden.status != http.StatusForbidden || forbidden.body["error"].(map[string]any)["code"] != "forbidden" {
			testing.Errorf("%s %s: 一般ユーザーは 403 のはず: %d %v", endpoint.method, endpoint.path, forbidden.status, forbidden.body)
		}
	}
	// 管理者は通る。
	if listed := call(testing, environment, http.MethodGet, "/auth/admin/users", "", admin.accessToken, "203.0.113.2"); listed.status != http.StatusOK {
		testing.Fatalf("管理者は 200 のはず: %d %v", listed.status, listed.body)
	}
	// 管理者が降格された(role を user に戻した)ら、発行済みの access token(role=admin)でも、403 になる。
	environment.database.Exec("UPDATE users SET role = 'user' WHERE id = ?", admin.userID)
	if demoted := call(testing, environment, http.MethodGet, "/auth/admin/users", "", admin.accessToken, "203.0.113.2"); demoted.status != http.StatusForbidden {
		testing.Errorf("降格された管理者は 403 のはず: %d", demoted.status)
	}
	// 管理者に戻して、停止すると、これも 403。
	environment.database.Exec("UPDATE users SET role = 'admin', status = 'banned' WHERE id = ?", admin.userID)
	if banned := call(testing, environment, http.MethodGet, "/auth/admin/users", "", admin.accessToken, "203.0.113.2"); banned.status != http.StatusForbidden {
		testing.Errorf("停止された管理者は 403 のはず: %d", banned.status)
	}
	// refresh token を、access token として使っても、401(種類の確認)。
	refreshToken := login(testing, environment, "alice", goodPassword, "203.0.113.3").body["token"].(string)
	if asAccess := call(testing, environment, http.MethodGet, "/auth/admin/users", "", refreshToken, "203.0.113.3"); asAccess.status != http.StatusUnauthorized {
		testing.Errorf("refresh token は 401 のはず: %d", asAccess.status)
	}
}

// TestAdminUsersAndBan は、ユーザーの一覧(ページング、ハッシュを含まない)と取得、停止(refresh token が全て無効になる、
// ログインできない、自分自身は停止できない、二重の停止は 409)、解除(ログインし直せる)、ロックの解除を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestAdminUsersAndBan(testing *testing.T) {
	// 管理者と、一般ユーザー2人を用意する。
	environment := newTestEnvironment(testing, func(settings *service.Settings) { settings.LoginMaxFailures = 2 })
	admin := newAdminSession(testing, environment, "root-admin")
	aliceID, _ := accessTokenFor(testing, environment, "alice", true, "203.0.113.1")
	bobID, _ := accessTokenFor(testing, environment, "bob", true, "203.0.113.1")
	// 一覧は新しい順で、ページングでき、パスワードのハッシュを含まない。
	page := call(testing, environment, http.MethodGet, "/auth/admin/users?limit=2", "", admin.accessToken, "203.0.113.2")
	users, _ := page.body["users"].([]any)
	if page.status != 200 || len(users) != 2 || users[0].(map[string]any)["userName"] != "bob" || page.body["nextCursor"] == nil {
		testing.Fatalf("新しい順の2件と、次のカーソルのはず: %d %v", page.status, page.body)
	}
	if _, leaked := users[0].(map[string]any)["passwordHash"]; leaked || strings.Contains(string(mustJSON(page.body)), "argon2") {
		testing.Fatal("パスワードのハッシュを返してはいけない")
	}
	next := call(testing, environment, http.MethodGet, "/auth/admin/users?limit=2&cursor="+page.body["nextCursor"].(string), "", admin.accessToken, "203.0.113.2")
	if nextUsers, _ := next.body["users"].([]any); len(nextUsers) != 1 || nextUsers[0].(map[string]any)["userName"] != "root-admin" || next.body["nextCursor"] != nil {
		testing.Fatalf("残りの1件のはず: %v", next.body)
	}
	if bad := call(testing, environment, http.MethodGet, "/auth/admin/users?limit=0", "", admin.accessToken, "203.0.113.2"); bad.status != 400 {
		testing.Errorf("limit=0 は 400 のはず: %d", bad.status)
	}
	// 取得できる。存在しない ID は 404。
	if got := call(testing, environment, http.MethodGet, "/auth/admin/users/"+aliceID, "", admin.accessToken, "203.0.113.2"); got.status != 200 || got.body["userName"] != "alice" || got.body["role"] != "user" || got.body["status"] != "active" {
		testing.Errorf("alice を取得できるはず: %d %v", got.status, got.body)
	}
	if missing := call(testing, environment, http.MethodGet, "/auth/admin/users/userid-nothing", "", admin.accessToken, "203.0.113.2"); missing.status != 404 {
		testing.Errorf("404 のはず: %d", missing.status)
	}
	// alice の refresh token を持っておく(停止で、無効になるはず)。
	aliceRefresh := login(testing, environment, "alice", goodPassword, "203.0.113.3").body["token"].(string)
	if before := call(testing, environment, http.MethodPost, "/auth/refresh", "", aliceRefresh, "203.0.113.3"); before.status != 200 {
		testing.Fatalf("停止の前は、使えるはず: %d", before.status)
	}
	// 停止する。自分自身は 400、存在しないものは 404、二重は 409。
	if self := call(testing, environment, http.MethodPost, "/auth/admin/users/"+admin.userID+"/ban", "", admin.accessToken, "203.0.113.2"); self.status != 400 {
		testing.Errorf("自分自身は 400 のはず: %d", self.status)
	}
	if missing := call(testing, environment, http.MethodPost, "/auth/admin/users/userid-nothing/ban", "", admin.accessToken, "203.0.113.2"); missing.status != 404 {
		testing.Errorf("404 のはず: %d", missing.status)
	}
	if banned := call(testing, environment, http.MethodPost, "/auth/admin/users/"+aliceID+"/ban", "", admin.accessToken, "203.0.113.2"); banned.status != 200 {
		testing.Fatalf("停止できるはず: %d %v", banned.status, banned.body)
	}
	if twice := call(testing, environment, http.MethodPost, "/auth/admin/users/"+aliceID+"/ban", "", admin.accessToken, "203.0.113.2"); twice.status != 409 {
		testing.Errorf("二重は 409 のはず: %d", twice.status)
	}
	// 停止中は、既存の refresh token も、ログインも、使えない(理由は区別されない 401)。bob は影響されない。
	if after := call(testing, environment, http.MethodPost, "/auth/refresh", "", aliceRefresh, "203.0.113.3"); after.status != 401 {
		testing.Errorf("refresh token は無効のはず: %d", after.status)
	}
	if loginBanned := login(testing, environment, "alice", goodPassword, "203.0.113.3"); loginBanned.status != 401 {
		testing.Errorf("停止中はログインできないはず: %d", loginBanned.status)
	}
	if bobLogin := login(testing, environment, "bob", goodPassword, "203.0.113.3"); bobLogin.status != 200 {
		testing.Errorf("bob は影響されないはず: %d", bobLogin.status)
	}
	var revokedCount, activeCount int64
	environment.database.Table("refresh_tokens").Where("user_id = ? AND revoked_at IS NOT NULL", aliceID).Count(&revokedCount)
	environment.database.Table("refresh_tokens").Where("user_id = ? AND revoked_at IS NULL", aliceID).Count(&activeCount)
	if revokedCount == 0 || activeCount != 0 {
		testing.Errorf("alice の refresh token は全て無効のはず: 無効 %d、有効 %d", revokedCount, activeCount)
	}
	// 解除する(停止中でなければ 409)。ログインし直せるが、古い refresh token は戻らない。
	if unbanned := call(testing, environment, http.MethodPost, "/auth/admin/users/"+aliceID+"/unban", "", admin.accessToken, "203.0.113.2"); unbanned.status != 200 {
		testing.Fatalf("解除できるはず: %d", unbanned.status)
	}
	if notBanned := call(testing, environment, http.MethodPost, "/auth/admin/users/"+aliceID+"/unban", "", admin.accessToken, "203.0.113.2"); notBanned.status != 409 {
		testing.Errorf("停止中でなければ 409 のはず: %d", notBanned.status)
	}
	if relogin := login(testing, environment, "alice", goodPassword, "203.0.113.3"); relogin.status != 200 {
		testing.Errorf("解除後は、ログインできるはず: %d", relogin.status)
	}
	if old := call(testing, environment, http.MethodPost, "/auth/refresh", "", aliceRefresh, "203.0.113.3"); old.status != 401 {
		testing.Errorf("古い refresh token は戻らないはず: %d", old.status)
	}
	// ロックの解除: 2回失敗するとロックされ、正しいパスワードでも入れない。解除すると、入れる。
	_ = login(testing, environment, "bob", "wrong password!!", "203.0.113.4")
	_ = login(testing, environment, "bob", "wrong password!!", "203.0.113.4")
	if locked := login(testing, environment, "bob", goodPassword, "203.0.113.4"); locked.status != 401 {
		testing.Fatalf("ロック中のはず: %d", locked.status)
	}
	if unlocked := call(testing, environment, http.MethodPost, "/auth/admin/users/"+bobID+"/unlock", "", admin.accessToken, "203.0.113.2"); unlocked.status != 200 {
		testing.Fatalf("ロックを解除できるはず: %d", unlocked.status)
	}
	if afterUnlock := login(testing, environment, "bob", goodPassword, "203.0.113.4"); afterUnlock.status != 200 {
		testing.Errorf("解除後は入れるはず: %d", afterUnlock.status)
	}
	if missing := call(testing, environment, http.MethodPost, "/auth/admin/users/userid-nothing/unlock", "", admin.accessToken, "203.0.113.2"); missing.status != 404 {
		testing.Errorf("404 のはず: %d", missing.status)
	}
}

// TestAdminInvitations は、招待の発行(コードは1度だけ返る)、一覧(コードを含まない、状態)、招待制のサインアップでの使用(1回だけ)、
// 取り消し(使えなくなる、使用済み・取り消し済みは 409)、期間とメモの検証を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestAdminInvitations(testing *testing.T) {
	// 招待制の環境に、管理者を用意する。
	environment := newTestEnvironment(testing, func(settings *service.Settings) { settings.SignupMode = service.SignupModeInvite })
	admin := newAdminSession(testing, environment, "root-admin")
	issue := func(body string) response {
		return call(testing, environment, http.MethodPost, "/auth/admin/invitations", body, admin.accessToken, "203.0.113.2")
	}
	signupWith := func(userName string, code string) response {
		return call(testing, environment, http.MethodPost, "/auth/signup", `{"userName":"`+userName+`","password":"`+goodPassword+`","invitationCode":"`+code+`"}`, "", "203.0.113.9")
	}
	// 発行すると、ID、コード、期限が返る(ボディが空でも、既定の期間で発行できる)。
	first := issue(`{"note":"for carol","expiresInHours":24}`)
	code, _ := first.body["code"].(string)
	if first.status != 200 || len(code) < 40 || first.body["invitationId"] == nil || first.body["expiresAt"] == nil {
		testing.Fatalf("発行できるはず: %d %v", first.status, first.body)
	}
	if empty := issue(``); empty.status != 200 {
		testing.Fatalf("ボディが空でも発行できるはず: %d %v", empty.status, empty.body)
	}
	// コードは、保存されていない(ハッシュだけ)。
	var storedCodes int64
	environment.database.Table("invitations").Where("code_hash = ?", code).Count(&storedCodes)
	if storedCodes != 0 {
		testing.Fatal("コードそのものを保存してはいけない")
	}
	// 一覧には、コードが含まれず、状態が分かる。
	listed := call(testing, environment, http.MethodGet, "/auth/admin/invitations", "", admin.accessToken, "203.0.113.2")
	invitations, _ := listed.body["invitations"].([]any)
	if listed.status != 200 || len(invitations) != 2 || strings.Contains(string(mustJSON(listed.body)), code) {
		testing.Fatalf("2件で、コードを含まないはず: %d %v", listed.status, listed.body)
	}
	var carolInvitation map[string]any
	for _, entry := range invitations {
		if entry.(map[string]any)["invitationId"] == first.body["invitationId"] {
			carolInvitation = entry.(map[string]any)
		}
	}
	if carolInvitation["status"] != "active" || carolInvitation["note"] != "for carol" || carolInvitation["createdBy"] != admin.userID || carolInvitation["usedBy"] != nil {
		testing.Fatalf("有効で、メモと発行者が分かるはず: %v", carolInvitation)
	}
	// 招待コードで、サインアップできる。2回目は使えない。使うと、一覧では used になる。
	if signedUp := signupWith("carol", code); signedUp.status != 200 {
		testing.Fatalf("招待でサインアップできるはず: %d %v", signedUp.status, signedUp.body)
	}
	if reused := signupWith("dave", code); reused.status != 400 {
		testing.Errorf("使用済みの招待は 400 のはず: %d", reused.status)
	}
	listed = call(testing, environment, http.MethodGet, "/auth/admin/invitations", "", admin.accessToken, "203.0.113.2")
	for _, entry := range listed.body["invitations"].([]any) {
		if entry.(map[string]any)["invitationId"] == first.body["invitationId"] && (entry.(map[string]any)["status"] != "used" || entry.(map[string]any)["usedBy"] == nil) {
			testing.Errorf("使用済みで、使った人が分かるはず: %v", entry)
		}
	}
	// 使用済みの招待は、取り消せない(409)。
	if revokeUsed := call(testing, environment, http.MethodDelete, "/auth/admin/invitations/"+first.body["invitationId"].(string), "", admin.accessToken, "203.0.113.2"); revokeUsed.status != 409 {
		testing.Errorf("使用済みは 409 のはず: %d", revokeUsed.status)
	}
	// 取り消した招待では、サインアップできない。二重の取り消しは 409。存在しない ID は 404。
	second := issue(`{}`)
	secondCode := second.body["code"].(string)
	if revoked := call(testing, environment, http.MethodDelete, "/auth/admin/invitations/"+second.body["invitationId"].(string), "", admin.accessToken, "203.0.113.2"); revoked.status != 200 {
		testing.Fatalf("取り消せるはず: %d %v", revoked.status, revoked.body)
	}
	if useRevoked := signupWith("erin", secondCode); useRevoked.status != 400 {
		testing.Errorf("取り消した招待は使えないはず: %d", useRevoked.status)
	}
	if twice := call(testing, environment, http.MethodDelete, "/auth/admin/invitations/"+second.body["invitationId"].(string), "", admin.accessToken, "203.0.113.2"); twice.status != 409 {
		testing.Errorf("二重の取り消しは 409 のはず: %d", twice.status)
	}
	if missing := call(testing, environment, http.MethodDelete, "/auth/admin/invitations/invitation-nothing", "", admin.accessToken, "203.0.113.2"); missing.status != 404 {
		testing.Errorf("404 のはず: %d", missing.status)
	}
	// 期限切れの招待は使えず、状態は expired。取り消せない。
	third := issue(`{}`)
	environment.database.Exec("UPDATE invitations SET expires_at = now() - interval '1 minute' WHERE id = ?", third.body["invitationId"])
	if useExpired := signupWith("frank", third.body["code"].(string)); useExpired.status != 400 {
		testing.Errorf("期限切れは使えないはず: %d", useExpired.status)
	}
	if revokeExpired := call(testing, environment, http.MethodDelete, "/auth/admin/invitations/"+third.body["invitationId"].(string), "", admin.accessToken, "203.0.113.2"); revokeExpired.status != 409 {
		testing.Errorf("期限切れは 409 のはず: %d", revokeExpired.status)
	}
	// 期間とメモの検証。
	for name, body := range map[string]string{
		"期間が負": `{"expiresInHours":-1}`, "期間が上限(720時間)を超える": `{"expiresInHours":721}`,
		"メモが長すぎる": `{"note":"` + strings.Repeat("a", model.MaxInvitationNoteLength+1) + `"}`, "JSON でない": `not json`,
	} {
		if invalid := issue(body); invalid.status != 400 {
			testing.Errorf("%s は 400 のはず: %d", name, invalid.status)
		}
	}
	if maxOK := issue(`{"expiresInHours":720}`); maxOK.status != 200 {
		testing.Errorf("720時間は発行できるはず: %d", maxOK.status)
	}
}

// TestAdminCommands は、コマンドの経路(CreateAdmin、PromoteToAdmin)が、パスワードの強さを検査し、重複を拒み、
// すでにあるユーザーを管理者にでき(二重は 409、停止中は 409)、作った管理者でログインできることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestAdminCommands(testing *testing.T) {
	// 環境を用意する。
	environment := newTestEnvironment(testing, nil)
	ctx := context.Background()
	// 弱いパスワードと、不正な名前は、作れない。
	if _, weakError := environment.admin.CreateAdmin(ctx, "root-admin", "password1234"); weakError == nil {
		testing.Error("弱いパスワードは拒否されるはず")
	}
	if _, nameError := environment.admin.CreateAdmin(ctx, "bad name", goodPassword); nameError == nil {
		testing.Error("不正な名前は拒否されるはず")
	}
	// 作れて、ログインでき、role が admin の access token になる。重複は、大文字小文字を区別せず、拒否される。
	if _, createError := environment.admin.CreateAdmin(ctx, "Root-Admin", goodPassword); createError != nil {
		testing.Fatalf("作れるはず: %v", createError)
	}
	if _, duplicateError := environment.admin.CreateAdmin(ctx, "root-admin", goodPassword); duplicateError == nil {
		testing.Error("重複は拒否されるはず")
	}
	_, accessToken := accessTokenFor(testing, environment, "Root-Admin", false, "203.0.113.5")
	claims, parseError := environment.tokens.Parse(accessToken, "access")
	if parseError != nil || claims.Role != "admin" {
		testing.Fatalf("role=admin の access token のはず: %+v %v", claims, parseError)
	}
	// 既にあるユーザーを、管理者にできる(名前は、大文字小文字を区別しない)。二重は 409、無い名前は 404。
	_, _ = accessTokenFor(testing, environment, "alice", true, "203.0.113.6")
	if promoteError := environment.admin.PromoteToAdmin(ctx, "ALICE"); promoteError != nil {
		testing.Fatalf("昇格できるはず: %v", promoteError)
	}
	if twiceError := environment.admin.PromoteToAdmin(ctx, "alice"); twiceError == nil {
		testing.Error("二重の昇格は拒否されるはず")
	}
	if missingError := environment.admin.PromoteToAdmin(ctx, "nobody"); missingError == nil {
		testing.Error("存在しないユーザーは拒否されるはず")
	}
	// 停止中のユーザーは、管理者にできない。
	_, _ = accessTokenFor(testing, environment, "bob", true, "203.0.113.7")
	environment.database.Exec("UPDATE users SET status = 'banned' WHERE user_name_lower = 'bob'")
	if bannedError := environment.admin.PromoteToAdmin(ctx, "bob"); bannedError == nil {
		testing.Error("停止中のユーザーは拒否されるはず")
	}
}

// TestBootstrapAdmin は、起動時の最初の管理者の作成が、管理者がいないときだけ作ること(何度呼んでも1人)、
// 作った管理者で管理 API を使えること、停止中の管理者でも「いる」と数えること、同じ名前の一般ユーザーがいると
// 作らずにエラーにすること、弱いパスワードを拒否すること、複数の auth が同時に起動しても1人だけ作られることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestBootstrapAdmin(testing *testing.T) {
	// 管理者がいない状態から始める。最初の呼び出しは作り、2回目は何もしない。
	environment := newTestEnvironment(testing, nil)
	ctx := context.Background()
	if created, bootstrapError := environment.admin.BootstrapAdmin(ctx, "first-admin", goodPassword); bootstrapError != nil || !created {
		testing.Fatalf("作れるはず: %v %v", created, bootstrapError)
	}
	if created, bootstrapError := environment.admin.BootstrapAdmin(ctx, "first-admin", goodPassword); bootstrapError != nil || created {
		testing.Fatalf("2回目は、何もしないはず: %v %v", created, bootstrapError)
	}
	// 名前やパスワードが違っても、管理者がいれば何もしない(別の管理者は作られない)。
	if created, bootstrapError := environment.admin.BootstrapAdmin(ctx, "another", goodPassword); bootstrapError != nil || created {
		testing.Fatalf("管理者がいれば、何もしないはず: %v %v", created, bootstrapError)
	}
	var adminCount int64
	environment.database.Table("users").Where("role = 'admin'").Count(&adminCount)
	if adminCount != 1 {
		testing.Fatalf("管理者は1人のはず: %d", adminCount)
	}
	// 作った管理者で、ログインして、管理 API を使える。
	_, accessToken := accessTokenFor(testing, environment, "first-admin", false, "203.0.113.60")
	if listed := call(testing, environment, http.MethodGet, "/auth/admin/users", "", accessToken, "203.0.113.60"); listed.status != http.StatusOK {
		testing.Fatalf("管理 API を使えるはず: %d", listed.status)
	}
	// 管理者が停止されても、「いる」と数える(再起動のたびに、作ろうとして失敗しない)。
	environment.database.Exec("UPDATE users SET status = 'banned' WHERE role = 'admin'")
	if created, bootstrapError := environment.admin.BootstrapAdmin(ctx, "first-admin", goodPassword); bootstrapError != nil || created {
		testing.Fatalf("停止中でも、何もしないはず: %v %v", created, bootstrapError)
	}

	// 別の環境: 同じ名前の一般ユーザーがいて、管理者がいないときは、作らずにエラー。
	other := newTestEnvironment(testing, nil)
	_, _ = accessTokenFor(testing, other, "alice", true, "203.0.113.61")
	if created, bootstrapError := other.admin.BootstrapAdmin(ctx, "Alice", goodPassword); bootstrapError == nil || created || !strings.Contains(bootstrapError.Error(), "promote") {
		testing.Fatalf("同じ名前の一般ユーザーがいるときは、エラーのはず: %v %v", created, bootstrapError)
	}
	// 弱いパスワードは、エラー(管理者は作られない)。
	if created, bootstrapError := other.admin.BootstrapAdmin(ctx, "bob-admin", "password1234"); bootstrapError == nil || created {
		testing.Fatalf("弱いパスワードは、エラーのはず: %v %v", created, bootstrapError)
	}
	var otherAdmins int64
	other.database.Table("users").Where("role = 'admin'").Count(&otherAdmins)
	if otherAdmins != 0 {
		testing.Fatalf("管理者は作られないはず: %d", otherAdmins)
	}

	// 別の環境: 複数の auth が、同時に起動した場合。エラーにならず、管理者は1人だけ。
	concurrent := newTestEnvironment(testing, nil)
	type outcome struct {
		created bool
		err     error
	}
	outcomes := make(chan outcome, 6)
	for attempt := 0; attempt < 6; attempt++ {
		go func() {
			created, bootstrapError := concurrent.admin.BootstrapAdmin(ctx, "racing-admin", goodPassword)
			outcomes <- outcome{created: created, err: bootstrapError}
		}()
	}
	createdCount := 0
	for attempt := 0; attempt < 6; attempt++ {
		result := <-outcomes
		if result.err != nil {
			testing.Errorf("同時でも、エラーにならないはず: %v", result.err)
		}
		if result.created {
			createdCount++
		}
	}
	var racingAdmins int64
	concurrent.database.Table("users").Where("role = 'admin'").Count(&racingAdmins)
	if createdCount != 1 || racingAdmins != 1 {
		testing.Fatalf("作られるのは、ちょうど1人のはず: created=%d admins=%d", createdCount, racingAdmins)
	}
}

// TestAdminLoginEvents は、管理者が、全ユーザーのログイン履歴(失敗と、存在しない名前への試行を含む)を、絞り込んで見られることを確かめる。
func TestAdminLoginEvents(testing *testing.T) {
	// 管理者と、一般のユーザーを用意する。
	environment := newTestEnvironment(testing, nil)
	admin := newAdminSession(testing, environment, "root-admin")
	_, userToken := accessTokenFor(testing, environment, "mallory", true, "203.0.113.20")
	// 存在しない名前と、パスワードの間違いで、失敗の履歴を作る。
	call(testing, environment, http.MethodPost, "/auth/login", `{"userName":"ghost","password":"`+goodPassword+`"}`, "", "203.0.113.30")
	call(testing, environment, http.MethodPost, "/auth/login", `{"userName":"mallory","password":"wrong-password-123"}`, "", "203.0.113.31")
	list := func(query string, token string) response {
		return call(testing, environment, http.MethodGet, "/auth/admin/login-events"+query, "", token, "203.0.113.2")
	}
	// 失敗だけに絞ると、2 件の失敗が、理由と IP つきで出る(存在しない名前は、持ち主なし)。
	failures := list("?result=failure", admin.accessToken)
	events, _ := failures.body["events"].([]any)
	if failures.status != 200 || len(events) != 2 {
		testing.Fatalf("失敗が 2 件のはず: %d %v", failures.status, failures.body)
	}
	foundGhost := false
	for _, entry := range events {
		event := entry.(map[string]any)
		if event["userName"] == "ghost" && event["userId"] == nil && event["reason"] == "unknown_user" && event["ip"] == "203.0.113.30" {
			foundGhost = true
		}
	}
	if !foundGhost {
		testing.Errorf("存在しない名前への試行が出るはず: %v", events)
	}
	// ユーザー名の一部で絞り込める。
	if byName := list("?userName=MALLO", admin.accessToken); len(byName.body["events"].([]any)) < 2 {
		testing.Errorf("名前で絞り込めるはず: %v", byName.body)
	}
	// ページングできる(1 件ずつ)。
	first := list("?limit=1", admin.accessToken)
	if first.body["nextCursor"] == nil {
		testing.Fatalf("次のページがあるはず: %v", first.body)
	}
	second := list("?limit=1&cursor="+first.body["nextCursor"].(string), admin.accessToken)
	if second.status != 200 || len(second.body["events"].([]any)) != 1 {
		testing.Errorf("2 ページ目が取れるはず: %d %v", second.status, second.body)
	}
	// 結果の指定が不正なら 400。管理者でなければ 403。トークンが無ければ 401。
	if bad := list("?result=maybe", admin.accessToken); bad.status != 400 {
		testing.Errorf("不正な result は 400 のはず: %d", bad.status)
	}
	if forbidden := list("", userToken); forbidden.status != 403 {
		testing.Errorf("管理者でなければ 403 のはず: %d", forbidden.status)
	}
	if anonymous := list("", ""); anonymous.status != 401 {
		testing.Errorf("トークンが無ければ 401 のはず: %d", anonymous.status)
	}
}

// TestBlockedIPAndAudit は、ブロックした IP(単独と範囲)からのログインとサインアップが拒否され、期限切れ・他の IP は通ることと、
// 管理操作が監査ログに残ることを確かめる。
func TestBlockedIPAndAudit(testing *testing.T) {
	// 管理者と一般のユーザーを用意する。
	environment := newTestEnvironment(testing, nil)
	admin := newAdminSession(testing, environment, "root-admin")
	userID, _ := accessTokenFor(testing, environment, "victim", true, "203.0.113.50")
	// 単独の IP と、範囲(期限つき)と、期限切れの範囲を、登録する(管理コンテナが書く表と、同じ形)。
	environment.database.Exec("INSERT INTO blocked_ips (id, network, created_by) VALUES ('block-1', '198.51.100.7/32', 'x')")
	environment.database.Exec("INSERT INTO blocked_ips (id, network, created_by, expires_at) VALUES ('block-2', '192.0.2.0/24', 'x', now() + interval '1 hour')")
	environment.database.Exec("INSERT INTO blocked_ips (id, network, created_by, expires_at) VALUES ('block-3', '203.0.113.99/32', 'x', now() - interval '1 hour')")
	loginFrom := func(ip string) response {
		return call(testing, environment, http.MethodPost, "/auth/login", `{"userName":"victim","password":"`+goodPassword+`"}`, "", ip)
	}
	// ブロックされた IP は、正しいパスワードでも、403。範囲の中の IP も、403。
	for _, blockedIP := range []string{"198.51.100.7", "192.0.2.200"} {
		if blocked := loginFrom(blockedIP); blocked.status != 403 {
			testing.Errorf("%s は 403 のはず: %d %v", blockedIP, blocked.status, blocked.body)
		}
	}
	if signup := call(testing, environment, http.MethodPost, "/auth/signup", `{"userName":"newbie","password":"`+goodPassword+`"}`, "", "198.51.100.7"); signup.status != 403 {
		testing.Errorf("サインアップも 403 のはず: %d", signup.status)
	}
	// 期限切れのブロックと、範囲の外の IP は、通る。
	for _, allowedIP := range []string{"203.0.113.99", "198.51.100.8"} {
		if allowed := loginFrom(allowedIP); allowed.status != 200 {
			testing.Errorf("%s は通るはず: %d %v", allowedIP, allowed.status, allowed.body)
		}
	}
	// 拒否は、履歴に、理由 blocked で残る(持ち主なし)。
	var blockedEvents int64
	environment.database.Table("login_events").Where("reason = 'blocked' AND user_id IS NULL").Count(&blockedEvents)
	if blockedEvents != 3 {
		testing.Errorf("blocked の履歴が 3 件のはず: %d", blockedEvents)
	}
	// 管理操作(停止)が、監査ログに、誰が・何を・何にかが残る。
	if banned := call(testing, environment, http.MethodPost, "/auth/admin/users/"+userID+"/ban", "", admin.accessToken, "203.0.113.2"); banned.status != 200 {
		testing.Fatalf("停止できるはず: %d", banned.status)
	}
	var audit struct {
		ActorID string
		Action  string
		Target  string
	}
	environment.database.Raw("SELECT actor_id, action, target FROM admin_audit_logs WHERE action = 'ban'").Scan(&audit)
	if audit.ActorID != admin.userID || audit.Target != userID {
		testing.Errorf("監査ログに残るはず: %+v", audit)
	}
}
