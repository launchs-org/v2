# 認証

`/api/v1/...` は、全て `Authorization: Bearer <トークン>` が要る(例外: `/healthz` と、自前の認証を持つ `/hooks/deploy`)。トークンは 2 種類。

| | API トークン(`lkt_…`) | ログインのトークン(access token) |
|---|---|---|
| 向き | **機械(CI、スクリプト)** | 画面(ブラウザ) |
| 有効期間 | 発行のとき決める(30 日〜無期限) | 15 分(refresh token で更新する) |
| 権限 | `read` / `write` / `deploy:image`。対象も絞れる | 本人と同じ全て |
| 取り消し | 一覧から失効(すぐ効く) | ログアウト、BAN |
| 作る場所 | 画面、`POST /api/v1/api-tokens` | `POST /auth/login` |

**機械には、API トークンを使う。** ログインのトークンは、パスワードが要り、15 分で切れるので、自動処理に向かない。

## API トークン

- 形式は `lkt_` + JWT(HS512。それ以外のアルゴリズムは拒否)。`scope`、`deploys`、`projects` が、トークンの中の claim にある。DB で管理するのは、**有効・無効だけ**。
- **管理(発行・一覧・失効)は、ログインしたユーザーだけ**。API トークンでは呼べない(403)。トークンが別のトークンを作れないようにするため。
- 管理者の権限は、与えられない(役割は常に `user`)。

### 権限(scope)

| scope | 許す操作 |
|---|---|
| `read` | GET だけ |
| `write` | 全て(`read` を含む)。ただし、トークンの管理は不可 |
| `deploy:image` | `PUT /deploy/:deployId/image` と、`POST /hooks/deploy` だけ |

### 対象の絞り込み

- `deploys`(deploy の ID の配列)、`projects`(プロジェクトの ID の配列)。**どちらかを指定すると**、パスの `:deployId` か `:projectId` が範囲内の操作だけを許す。
- パスに ID を持たない操作(`POST /deploy`、`GET /projects`、`GET /quota` など)は、絞ったトークンでは**拒否**される(判定できないため)。
- `deploy:image` だけのトークンは、`deploys` を**ちょうど 1 つ**にする。

### エラー

| 状況 | 結果 |
|---|---|
| ヘッダーが無い、署名・期限が不正、失効済み | 401 `unauthorized` |
| scope や対象の範囲外 | 403 `forbidden` |
| `API_TOKEN_SECRET` が、サーバーに未設定 | 発行は 503、API トークンは全て 401(機能が無効) |

## ログインのトークン(参考)

```bash
# ログイン → refresh token(90 日)
curl -sS -X POST $LAUNCHS/auth/login -H 'Content-Type: application/json' \
  -d '{"userName":"...","password":"..."}'                        # → {"token": "<refreshToken>"}
# refresh token → access token(15 分)
curl -sS -X POST $LAUNCHS/auth/refresh -H "Authorization: Bearer <refreshToken>"   # → {"token": "<accessToken>"}
```

仕様は [docs/endpoints/auth.md](../endpoints/auth.md)。ログインには、設定により、人間であることの確認(Turnstile)が要る。
