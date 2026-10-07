[← イングレス関連](README.md)

# イングレス削除

- カテゴリ: イングレス関連 (/projects/:projectId/ingresses)
- Method / Path: `Delete /projects/:projectId/ingresses/:ingressId`

## 説明

イングレスを削除するエンドポイント。配下の path rule(`rules`/`pending_rules`)も全て連鎖して削除される。

## テスト要件

### 正常系
- `status` が `"active"` または `"failed"` の `ingressId` を指定すると 200 が返り、以後 [イングレス取得](003-get-projects-projectid-ingresses-ingressid.md) が 404 になること
- `rules` に path rule が設定されている状態でも削除を受け付け、配下の `rules`/`pending_rules` が連鎖して削除されること

### 異常系
- 存在しない `ingressId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`ingressId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `status` が `"applying"` の間に削除を呼び出した場合 409 が返ること(進行中のapplyと排他制御する。他のconfig/service/volumeに対する操作と同様の扱い)

### 境界値
- 特になし

## 仕様

- Delete: /projects/:projectId/ingresses/:ingressId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: 配下の path rule(rules/pending_rules) も全て連鎖して削除される
- Note: ingress の status が "applying" の間は 409 を返す(進行中のapplyと排他制御する)

## 参照

- [docs/endpoints/ingresses.md](../../endpoints/ingresses.md)
