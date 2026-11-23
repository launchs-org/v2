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
