# アクセスログの転送(webhook / Google Drive)

ingress のアクセスログ(`ingress_access_logs`)を、外へ書き出す設定の API。設計は [../log-export.md](../log-export.md)。共通のルールは [common.md](common.md)。

- **転送の設定は ingress に属する**。1 つのアカウントに **10 個まで**(webhook と Google Drive を合わせて)。同じ ingress に、Google Drive は 1 つまで(webhook は複数でよい)。
- **Google Drive の接続はアカウントに属する**(アカウント設定)。
- **サーバーで、暗号化の鍵(`EXPORT_CREDENTIAL_KEY`)が設定されていないと、機能は無効**。作る・テスト・同意の URL は **503**(一覧と、接続の状態は返る)。Google の OAuth(`GOOGLE_OAUTH_*`)が無いときは、Drive だけが使えない(`available` が false)。
- 他のユーザーのものは、すべて 404。

## Google Drive の接続(アカウント)

### 接続の状態
- Get: /account/google-drive
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"available": true, "connected": true, "accountEmail": "me@example.com", "status": "active", "lastError": ""}
```
- Note: `available` は、サーバーで Google Drive に新しくつなげるか。`status` は `active` / `reauth_required`(つないでいなければ空)

### 同意の画面の URL
- Post: /account/google-drive/authorize
- Response (200): `{"url": "https://accounts.google.com/o/oauth2/v2/auth?…"}`
- Note: ブラウザで、この URL を開く。再接続も、同じ。Drive が設定されていなければ 503
- Note: 権限は `drive.file`(launchs が作ったファイルだけ)と `openid email`。`state` は、利用者と有効期限(10 分)を署名したもの

### Google からの戻り先
- Get: /export/google/callback?code=…&state=…(パスは `/api/v1/export/google/callback`)
- Note: **認証ヘッダーは無い**(`state` の署名で、利用者を確かめる)。結果は **302** で、画面に戻す: `/?google=connected`、同意しなかったとき `/?google=denied`、失敗 `/?google=error`(理由の文章は、URL に入れない)
- Note: 合鍵(リフレッシュトークン)は、暗号化して保存する。つなぎ直したときは、上書きし、再接続待ちだった転送を、動ける状態に戻す

### 解除
- Delete: /account/google-drive
- Response (200): `{}`
- Note: 合鍵の取り消しを Google に依頼する(失敗しても、解除は進む)。**この接続を使う転送(Drive 宛て)は、消える**。Drive に書き出したファイルは、残る。つないでいなければ 404

## 転送の設定(ingress)

### 一覧
- Get: /projects/:projectId/ingresses/:ingressId/log-exports
- Response (200)
```json
{
  "logExports": [
    {"logExportId": "export-<uuid>", "kind": "webhook", "enabled": true, "status": "active", "url": "https://example.com/hook?…", "intervalSeconds": 30,
     "lastSuccessAt": "2026-11-26T10:00:00+09:00", "lastError": "", "failureCount": 0, "createdAt": "2026-11-26T09:00:00+09:00"},
    {"logExportId": "export-<uuid>", "kind": "google_drive", "enabled": true, "status": "reauth_required", "lastSuccessAt": null, "lastError": "Google の許可が無効です…", "failureCount": 0, "createdAt": "…"}
  ]
}
```
- Note: 作った順。`url` は、**クエリを伏せる**(`?…`)。署名の秘密は、出ない。`status`: `active`(動作中)/ `failing`(失敗が続いている。再試行する)/ `reauth_required`(Google の再接続が必要)

### 作成
- Post: /projects/:projectId/ingresses/:ingressId/log-exports
- Body(webhook)
```json
{"kind": "webhook", "url": "https://example.com/hook", "intervalSeconds": 30}
```
- Body(Google Drive): `{"kind": "google_drive"}`
- Response (201): 一覧の 1 件と同じ形。**webhook は、署名の秘密 `secret`("whsec_…")が入る。この 1 回だけ**
- Note: `url` は https だけ(1 文字〜2000 文字。ユーザー情報なし。localhost、内部のネットワーク、予約された範囲の IP は 400)。`intervalSeconds` は 10 / 30 / 60 / 300(省略は 30。それ以外は 400)
- Note: Google Drive は、アカウントがつないでいないと 409。つないではいるが再接続待ちのときは、`reauth_required` から始まる
- Note: **作った時点より後のログから**送る(過去のログは送らない)
- Note: 上限(アカウントで 10 個)、Drive の重複は 409。機能が無効なら 503

### 変更
- Patch: /projects/:projectId/ingresses/:ingressId/log-exports/:logExportId
- Body(指定した項目だけ)
```json
{"enabled": false, "url": "https://example.com/new", "intervalSeconds": 60}
```
- Response (200): 一覧の 1 件と同じ形
- Note: `url` と `intervalSeconds` は webhook だけ(Drive に付けると 400)。変えると、失敗の記録を消して、すぐに動ける状態にする(再接続待ちは、そのまま)。無効にしている間は、送らない(再開しても、止めていた間に消えたログは、送れない)

### 削除
- Delete: /projects/:projectId/ingresses/:ingressId/log-exports/:logExportId
- Response (200): `{}`
- Note: Drive に書き出したファイルは、残る

### テスト送信
- Post: /projects/:projectId/ingresses/:ingressId/log-exports/:logExportId/test
- Response (200)
```json
{"ok": false, "message": "受け口が 500 を返しました"}
```
- Note: webhook は `{"test": true, "logs": []}` を送る。Drive は、プロジェクトのフォルダに `launchs-connection-test.txt` を作る。**送れなかったときも 200 で、`ok` が false と理由**(理由に、URL やクエリは含まない)

## webhook の受け取り方

- `POST <url>`、`Content-Type: application/json`。タイムアウトは 10 秒。リダイレクトは辿らない。2xx が成功。
- ヘッダー: `X-Launchs-Timestamp`(Unix 秒)、`X-Launchs-Signature`(`sha256=` + `HMAC-SHA256(秘密, タイムスタンプ + "." + 本文)` の 16 進数)、`X-Launchs-Delivery`(`<設定の ID>-<最後の行の id>`)。
- 本文: `{"projectId", "ingressId", "destinationId", "logs": [{"id", "ingressId", "host", "time"(UTC・ミリ秒), "method", "url", "status", "durationMs", "clientIp", "bytes"}]}`。1 回に最大 1000 行、`id` の昇順。
- **配送は「少なくとも 1 回」**。失敗したら、同じ行を、間隔を倍々に延ばして(最大 1 時間)再送する。受け取る側は、行の `id` で、重複を除く。
