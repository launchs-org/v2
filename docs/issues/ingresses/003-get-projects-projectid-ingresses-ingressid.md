[← イングレス関連](README.md)

# イングレス取得

- カテゴリ: イングレス関連 (/projects/:projectId/ingresses)
- Method / Path: `Get /projects/:projectId/ingresses/:ingressId`

## 説明

指定したイングレスの詳細(`host`/`rules`/`pending_rules`/`status`)を取得するエンドポイント。

## テスト要件

### 正常系
- 自分が所有する `ingressId` を指定すると 200 が返り、`host`/`rules`/`status` が取得できること
- `pending_rules` が存在する場合、[イングレス更新](005-put-projects-projectid-ingresses-ingressid.md) 後まだ [適用](006-post-projects-projectid-ingresses-ingressid-apply.md)/[破棄](007-post-projects-projectid-ingresses-ingressid-discard.md) されていない変更内容が返ること(無ければ `null`)

### 異常系
- 存在しない `ingressId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`ingressId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- `status` は "applying" | "active" | "failed" 以外の値を取らないこと

## 仕様

- Get: /projects/:projectId/ingresses/:ingressId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、pending が無い場合)
```json
{
  "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "host": "random-word-1234.launchs.org",
  "rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080}
  ],
  "pending_rules": null,
  "status": "active"
}
```
- Response (200、pending が存在する場合)
```json
{
  "ingressId": "ingress-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "host": "random-word-1234.launchs.org",
  "rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080}
  ],
  "pending_rules": [
    {"path": "/", "serviceId": "service-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "port": 8080},
    {"path": "/api", "serviceId": "service-yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", "port": 8081}
  ],
  "status": "active"
}
```
- Note: rules は現在反映されているpath ruleの一覧。pending_rules は[イングレス更新](005-put-projects-projectid-ingresses-ingressid.md)後まだ[適用](006-post-projects-projectid-ingresses-ingressid-apply.md)されていない変更内容(変更が無ければ null)
- Note: status は "applying" | "active" | "failed" のいずれか。applyを呼ぶと反映が完了するまで "applying" になる

## 参照

- [docs/endpoints/ingresses.md](../../endpoints/ingresses.md)
