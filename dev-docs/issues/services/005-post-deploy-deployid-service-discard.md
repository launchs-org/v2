[← サービス関連](README.md)

# サービス 破棄

- カテゴリ: サービス関連 (/deploy/:deployId/service)
- Method / Path: `Post /deploy/:deployId/service/discard`

## 説明

[サービス更新](003-patch-deploy-deployid-service.md) で保存した `pending_ports` を適用せずに破棄するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `pending_ports` が存在する状態で呼び出すと 200 が返り、`pending_ports` が `null` に戻ること
- `ports`(現在反映されている値)には影響しないこと

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- service が未作成の `deployId` に対して呼び出した場合 404 が返ること
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `pending_ports` が `null` の場合 409 が返ること
- deploy の `status` が `"applying"` の間に呼び出した場合 409 が返ること
- deploy の `status` が `"deleting"` の間に呼び出した場合 409 が返ること

### 境界値
- 特になし

## 仕様

- Post: /deploy/:deployId/service/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_ports の内容を適用せずに破棄する(ports には影響しない)。pending_ports が null の場合は 409 を返す
- Note: deploy の status が "applying" の間は 409 を返す
- Note: deploy の status が "deleting" の間は 409 を返す(deploy の削除に伴い service も deleting になるため)

## 参照

- [dev-docs/endpoints/services.md](../../endpoints/services.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)
