[← エンドポイント一覧](README.md)

## API トークンと webhook (/api-tokens、/hooks、/deploy/:deployId/image)

パス、エラー形式、ID、日時などの共通ルールは [共通ルール](common.md) を参照。`/api-tokens` と `/deploy/:deployId/image` は `/api/v1` を省いて書いている。設計は [dev-docs/api-tokens.md](../api-tokens.md)。

### トークンの発行
- Post: /api-tokens
- Header: `Authorization: Bearer <ログインの access token>`(API トークンでは呼べない: 403)
- Body
```json
{"name": "CI", "scope": ["read"], "deploys": ["deploy-…"], "projects": ["project-…"], "expiresInDays": 90}
```
- Response (201)
```json
{"tokenId": "apitoken-…", "name": "CI", "scope": ["read"], "deploys": [], "projects": [], "expiresAt": "2026-…+09:00", "revokedAt": null, "lastUsedAt": null, "createdAt": "2026-…+09:00", "token": "lkt_eyJ…"}
```
- Note: `token` は、**この応答でだけ**返す(保存しない)。
- Note: `scope` は 1〜3 個。`read`(GET だけ)、`write`(全て)、`deploy:image`(`PUT /deploy/:id/image` と webhook だけ)。`name` は必須で 100 文字まで。`deploys`、`projects` は 50 個まで(省略は、絞らない)。`expiresInDays` は 1〜3650(省略は無期限)。
- Note: `deploy:image` だけのトークンは、`deploys` をちょうど 1 つ指定し、`projects` は空にする(400)。
- Note: 環境変数 `API_TOKEN_SECRET` が未設定のときは 503(機能が無効)。

### トークンの一覧
- Get: /api-tokens
- Response (200): `{"tokens": [{…発行の応答から token を除いたもの…}]}`(新しい順。失効したものも含む)

### トークンの失効
- Delete: /api-tokens/:tokenId
- Response (200): `{}`。無い、または他のユーザーのものは 404。すでに失効していても 200。失効は、すぐに効く。

### deploy 用トークンの再表示
- Get: /api-tokens/:tokenId/reveal(ログインしたユーザーだけ。API トークンでは 403)
- Response (200): `{"token": "lkt_…"}`(`Cache-Control: no-store`)
- Note: トークンの本体は保存していない。保存してある発行の日時、持ち主、scope、対象、期限から、**同じ秘密で、同じトークンを作り直す**(HS512 は、同じ入力から、同じ署名ができる)。
- Note: 対象は、**`deploy:image` だけのトークン**(webhook 用)。`read`、`write` のように権限の広いものは、発行のときの 1 度だけで、409。失効、期限切れも 409。無い、他人のものは 404。
- Note: `API_TOKEN_SECRET` を替えると、作り直したトークンも、使えなくなる(全てのトークンが失効する)。

### API トークンで API を呼ぶ
- Header: `Authorization: Bearer lkt_…`
- 持ち主のユーザーとして扱う(管理者の権限は無い)。署名・期限・失効が正しくなければ 401。scope と対象(`deploys`、`projects`)の範囲外の操作は 403。
- 対象の判定は、パスの `:deployId` と `:projectId` で行う。`deploys` か `projects` を指定したトークンは、そのどちらかが範囲内のものだけを許す。

### deploy のイメージの更新
- Put: /deploy/:deployId/image
- Body: `{"imageRef": "ghcr.io/org/app:v2", "apply": true}`(`apply` は省略可。true なら、続けて反映も始める)
- Response (200): `{"changed": true, "applied": true}`
- Note: イメージは `pending_image_ref` に保存する(deploy の `config.pending_image_ref` に出る)。起動設定の pending(env、command、args)も、現在の値のコピーで一緒に作るので、反映は `POST /deploy/:deployId/config/apply` と同じ。`apply` を指定しないときは、あとで `config/apply` を呼ぶ。`config/discard` で、イメージの pending も破棄する。
- Note: 現在と同じイメージなら、何も保存しない(`changed: false`)。反映中・削除の途中・起動中は 409。`imageRef` は、deploy の作成と同じ規則(必須、512 文字まで)。

### webhook(自動デプロイ)
- Post: /hooks/deploy(`/api/v1` の外)
- Header: `X-Launchs-Token: lkt_…`(または `Authorization: Bearer lkt_…`)
- Body(上から順に見る): `{"imageRef": "ghcr.io/org/app:v2"}`、`{"tag": "v2"}`、Docker Hub の `{"push_data": {"tag": "v2"}}`
- Response (200): `{"deployId": "deploy-…", "imageRef": "…", "changed": true, "applied": true}`
- Note: トークンは、`deploy:image`(または `write`)で、`deploys` がちょうど 1 つのもの。それ以外は 403。トークンが無効なら 401。
- Note: `tag` だけのときは、いまのイメージの、タグ(と digest)を差し替える(ホストのポートを、タグと取り違えない)。決められない、または参照に使えない文字があれば 400。
- Note: 変更があれば、イメージを更新して反映を始める。同じイメージなら何もしない(`changed: false`、`applied: false`)。反映中などは 409(あとから、やり直す)。
