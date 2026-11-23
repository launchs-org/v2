[← エンドポイント一覧](README.md)

## アカウント管理 (/auth/admin)

認証コンテナ(auth)の管理 API。パス、エラー形式などの共通ルールは [共通ルール](common.md) を参照。実際の URL は `/auth/admin/...`(パスを書き換えずに auth に渡す)。設計は [docs/auth.md](../auth.md) の §6・§14。

### 共通
- Header: `{"Authorization": "Bearer <accessToken>"}`
- **管理者だけ**が使える。access token の `role` は見ず、**毎回、DB の最新の状態**で「管理者で、停止中でない」ことを確かめる(降格・停止された人が、残りの有効期間(最大15分)で操作できないようにするため)。
  - トークンが無い・使えない(refresh token を含む): 401。
  - 管理者でない、または停止中: **403** `forbidden`。
- 操作は、auth のログと、監査ログ(`admin_audit_logs`。[docs/security.md](../security.md))に、誰が(`actor`)、何を(`action`)、何に対して(`target`)行ったかを残す。
- 最初の管理者は、API では作れない。**起動時に作る**(環境変数 `AUTH_BOOTSTRAP_ADMIN_NAME` と、パスワード `AUTH_BOOTSTRAP_ADMIN_PASSWORD` または `AUTH_BOOTSTRAP_ADMIN_PASSWORD_FILE`。管理者が1人もいないときだけ。Kubernetes ではこちら)か、コマンドで作る: `task admin:create -- <userName>`(パスワードは、環境変数 `AUTH_ADMIN_PASSWORD` か、標準入力の1行で渡す。通常のサインアップと同じ強さの検査を行う)。すでにあるユーザーを管理者にする: `task admin:promote -- <userName>`。

### ユーザーの一覧
- Get: /auth/admin/users?limit=20&cursor=...
- Response (200)
```json
{
  "users": [
    {"userId": "userid-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "userName": "alice", "status": "active", "role": "user", "createdAt": "2026-11-15T20:38:25+09:00"}
  ],
  "nextCursor": null
}
```
- Note: 新しい順。`limit` は 1〜100(既定 20)、`cursor` は前のレスポンスの `nextCursor`。不正は 400。パスワードのハッシュは返さない。

### ユーザーの取得
- Get: /auth/admin/users/:userId
- Response (200): 一覧の要素と同じ形。存在しなければ 404。

### ユーザーの停止
- Post: /auth/admin/users/:userId/ban
- Response (200): `{}`
- Note: 状態が `banned` になり、**そのユーザーの有効な refresh token が全て無効**になる。停止中は、ログインできず(理由を区別しない 401)、`/refresh` もできない。発行済みの access token は、期限(最大15分)まで使える。
- Note: 自分自身は 400。ユーザーが無ければ 404。すでに停止中なら 409。

### ユーザーの停止の解除
- Post: /auth/admin/users/:userId/unban
- Response (200): `{}`
- Note: 状態が `active` に戻る。無効にした refresh token は戻らない(ログインし直す)。ユーザーが無ければ 404。停止中でなければ 409。

### ログインのロックの解除
- Post: /auth/admin/users/:userId/unlock
- Response (200): `{}`
- Note: そのユーザー名の、ログイン失敗の回数とロック(5回の失敗で15分)を消す。ユーザーが無ければ 404。IP ごとのレート制限は、解除しない。

### 招待の発行
- Post: /auth/admin/invitations
- Body(省略可)
```json
{"expiresInHours": 24, "note": "for carol"}
```
- Response (200)
```json
{"invitationId": "invitation-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "code": "opIJOdrJssFhh9J1KslFxIttdk8qVSWx7tXcrkaQBu8", "expiresAt": "2026-11-16T20:38:26+09:00"}
```
- Note: `code` は、サインアップの `invitationCode` に使う。**このレスポンスでだけ分かる**(DB には、ハッシュだけを保存する。再取得はできない)。
- Note: `expiresInHours` は省略(0)で 168(7日)。上限は 720(30日)。負や上限超えは 400。`note` は 200 バイトまで(超えたら 400)。
- Note: 招待制(`AUTH_SIGNUP_MODE=invite`)のときだけ、サインアップに必要になる。コードは、使えるのは1回だけ。

### 招待の一覧
- Get: /auth/admin/invitations?limit=20&cursor=...
- Response (200)
```json
{
  "invitations": [
    {"invitationId": "invitation-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "note": "for carol", "status": "active", "createdBy": "userid-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
     "createdAt": "2026-11-15T20:38:26+09:00", "expiresAt": "2026-11-16T20:38:26+09:00", "usedBy": null, "usedAt": null}
  ],
  "nextCursor": null
}
```
- Note: 新しい順。コードは含まない。`status` は、`active`(有効)、`used`(使用済み)、`expired`(期限切れ)、`revoked`(取り消し済み)のどれか。

### 招待の取り消し
- Delete: /auth/admin/invitations/:invitationId
- Response (200): `{}`
- Note: 取り消した招待は使えない。招待が無ければ 404。有効でない(使用済み、期限切れ、取り消し済み)なら 409。

### ログイン履歴の一覧(全ユーザー)
- Get: /auth/admin/login-events?limit=20&cursor=...&result=failure&userName=ali
- Response (200)
```json
{
  "events": [
    {"eventId": 130, "userId": null, "userName": "ghost", "kind": "login", "result": "failure", "reason": "unknown_user",
     "ip": "203.0.113.30", "userAgent": "curl/8.14.1", "occurredAt": "2026-11-24T00:31:44+09:00"}
  ],
  "nextCursor": null
}
```
- Note: 新しい順。成功も失敗も含み、**存在しないユーザー名への試行も出る**(`userId` は null)。不正なアクセスの確認に使う。
- Note: `result`(`success` か `failure`。それ以外は 400)と、`userName`(部分一致。大文字小文字は区別しない)で絞り込める。`limit` は 1〜100(既定 20)、`cursor` は前のレスポンスの `nextCursor`。
- Note: `reason` は、失敗のときだけ(`unknown_user`、`wrong_password`、`locked`、`banned`、`invalid_input`)。
