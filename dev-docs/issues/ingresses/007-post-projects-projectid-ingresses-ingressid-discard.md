[← イングレス関連](README.md)

# イングレス 破棄

- カテゴリ: イングレス関連 (/projects/:projectId/ingresses)
- Method / Path: `Post /projects/:projectId/ingresses/:ingressId/discard`

## 説明

[イングレス更新](005-put-projects-projectid-ingresses-ingressid.md) で保存した `pending_rules` を適用せずに破棄するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `pending_rules` が存在する状態で呼び出すと 200 が返り、`pending_rules` が `null` に戻ること
- `rules`(現在反映されている値)には影響しないこと

### 異常系
- 存在しない `ingressId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`ingressId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `pending_rules` が `null` の場合 409 が返ること
- `status` が `"applying"` の間に呼び出した場合 409 が返ること
- `status` が `"deleting"` の間に呼び出した場合 409 が返ること

### 境界値
- 特になし

## 仕様

- Post: /projects/:projectId/ingresses/:ingressId/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_rules の内容を適用せずに破棄する(rules には影響しない)。pending_rules が null の場合は 409 を返す
- Note: ingress の status が "applying" または "deleting" の間は 409 を返す

## 参照

- [dev-docs/endpoints/ingresses.md](../../endpoints/ingresses.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)
