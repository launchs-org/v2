[← エンドポイント一覧](README.md)

## 管理コンテナ (/admin-api)

管理者だけが使う API。**別コンテナ `admin/`** が提供する(app データベースにつなぐ。アカウントの管理は、auth の [/auth/admin](auth_admin.md))。パス、エラー形式などの共通ルールは [共通ルール](common.md) を参照。URL は `/admin-api/...`(パスを書き換えずに渡す)。画面は、フロントエンドの `/admin`。

### 共通
- Header: `{"Authorization": "Bearer <accessToken>"}`
- access token の `role` が `admin` のものだけが通る。トークンが無い・使えない: 401。管理者でない: **403** `forbidden`。
- `role` は access token の値なので、管理者でなくなった人が、残りの有効期間(最大15分)、通ることがある(`/auth/admin` は、毎回 DB で確かめる)。
- 最初の管理者(bootstrap のアカウント)は、auth の起動時に作る(`AUTH_BOOTSTRAP_ADMIN_NAME`、`AUTH_BOOTSTRAP_ADMIN_PASSWORD`。[auth_admin.md](auth_admin.md))。

### 全体の概要
- Get: /admin-api/overview
- Response (200)
```json
{
  "projects": [{"status": "active", "count": 11}],
  "deploys": [{"status": "active", "count": 6}, {"status": "failed", "count": 1}],
  "ingresses": [{"status": "active", "count": 3}],
  "volumes": [{"status": "active", "count": 3}],
  "owners": 9
}
```
- Note: プロジェクト、deploy、ingress、volume の、状態ごとの数。`owners` は、プロジェクトを持つユーザーの数。

### deploy の一覧(全ユーザー)
- Get: /admin-api/deploys?limit=50&cursor=...&status=failed&userId=...&q=nginx
- Response (200)
```json
{"deploys": [{"deployId": "deploy-...", "projectId": "project-...", "projectName": "alpha", "userId": "userid-...", "imageRef": "nginx:1.27", "spec": "small", "replicas": 1, "status": "active", "createdAt": "2026-11-23T03:59:00+09:00"}], "nextCursor": null}
```
- Note: 新しい順。読むだけ。`q` は、イメージ・プロジェクト名・deploy の ID の部分一致(大文字小文字は区別しない)。`limit` は 1〜100(既定 20)。

### アクセスログ(全 ingress)
- Get: /admin-api/access-logs?limit=100&cursor=...&status=4xx&ip=203.0.113.5&q=/login
- Response (200)
```json
{"logs": [{"time": "2026-11-24T00:31:44.123+09:00", "host": "brave-blue-fox.example.org", "ingressId": "ingress-...", "method": "GET", "url": "/a?b=1", "status": 404, "durationMs": 1.2, "clientIp": "203.0.113.5", "bytes": 10}], "nextCursor": null}
```
- Note: 新しい順。`status` は `2xx`〜`5xx` か、`404` のような数字(不正は 400)。`q` は、ホスト名か URL の部分一致。ingress が消えていると、`host` は空。保存の期間と行数は、`ACCESS_LOG_RETENTION`、`ACCESS_LOG_MAX_ROWS`。

### 全体のクォータ(既定)
- Get: /admin-api/quota-defaults
- Put: /admin-api/quota-defaults
- Body / Response (200)
```json
{"projects": 10, "deploys": 20, "volumes": 10, "volumeTotalMB": 20480, "deploysBySpec": {"small": 20, "medium": 10, "large": 5}}
```
- Note: PUT は、全ての数の項目が必要(0〜1000000。範囲外、知らない項目名、存在しないスペックは 400)。`deploysBySpec` に含めないスペックは、変えない。1 つのトランザクションで更新する。
- Note: 既に上限を超えているデータは、消さない(新しく作れなくなるだけ)。ユーザーごとの上書きは、[quota.md](quota.md) の `/api/v1/admin/users/:userId/quota`(backend)。

### システムの設定(読むだけ)
- Get: /admin-api/settings
- Response (200): `{"settings": [{"name": "INGRESS_BASE_DOMAIN", "value": "shconn.org"}]}`
- Note: 秘密でない、決まった名前の環境変数だけ(`LAUNCHS_HOST`、`INGRESS_BASE_DOMAIN`、`VOLUME_*`、`AUTH_SIGNUP_MODE` など)。空は、未設定(既定)。**変更は、デプロイの設定(`deploy/settings/config.yaml`、Argo CD の上書き)で行う**。

## セキュリティ (/admin-api/security)
設計と限界は [dev-docs/security.md](../security.md)。

### 不審な IP
- Get: /admin-api/security/suspicious-ips?hours=24&minFailures=5
- Response (200): `{"ips": [{"ip": "198.51.100.7", "failures": 6, "successes": 0, "userNames": 3, "lastSeen": "2026-11-24T01:33:44+09:00", "blocked": false}]}`
- Note: ログイン履歴から、期間内に失敗が `minFailures` 回以上の IP を、失敗の多い順に(最大 100)。`hours` は 1〜720、`minFailures` は 1〜10000(範囲外は 400)。`blocked` は、いま、ブロックされているか。

### ブロックの一覧・追加・解除
- Get: /admin-api/security/blocked-ips → `{"blocks": [{"blockId": "block-...", "network": "198.51.100.7/32", "reason": "...", "createdBy": "...", "createdByName": "root", "createdAt": "...", "expiresAt": null}]}`(期限内のものだけ)
- Post: /admin-api/security/blocked-ips
```json
{"network": "203.0.113.0/24", "reason": "総当たり", "expiresInHours": 24}
```
- Response (200): 追加したブロック(一覧の要素と同じ形)。
- Note: `network` は IP か CIDR(ホスト部は落とす)。`expiresInHours` は 0〜8760(0 か省略は、解除するまで)。理由は 200 バイトまで。形が不正、範囲外、**操作している自分の接続元(ヘッダー `X-Real-Ip`、無ければ接続の相手)を含む**ときは 400。知らない項目名も 400。
- Delete: /admin-api/security/blocked-ips/:blockId → `{}`。無ければ 404。
- Note: ブロックは、auth のログインとサインアップを 403 にする(履歴の理由は `blocked`)。

### 監査ログ
- Get: /admin-api/security/audit-logs?limit=50&cursor=...&action=ip_block
- Response (200): `{"logs": [{"time": "...", "actorId": "...", "actorName": "root", "action": "ip_block", "target": "198.51.100.7/32", "detail": "総当たり", "ip": "203.0.113.2"}], "nextCursor": null}`
- Note: 新しい順。`action` は `ban`、`unban`、`unlock`、`password_reset`、`invitation_create`、`invitation_revoke`、`admin_create`、`admin_promote`、`ip_block`、`ip_unblock`、`quota_defaults_update`。追記だけ(変更・削除の API は無い)。
