[← ボリューム関連](README.md)

# デプロイ ボリュームマウント 破棄

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Post /deploy/:deployId/mounts/discard`

## 説明

[マウント設定](010-put-deploy-deployid-mounts.md) で保存した `pending_mounts` を適用せずに破棄するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `pending_mounts` が存在する状態で呼び出すと 200 が返り、`pending_mounts` が `null` に戻ること
- `mounts`(現在反映されている値)には影響しないこと

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `pending_mounts` が無い場合 409 が返ること
- `status` が `"applying"` の間に呼び出した場合 409 が返ること
- deploy の `status` が `"deleting"` の間に呼び出した場合 409 が返ること

### 境界値
- 特になし

## 仕様

- Post: /deploy/:deployId/mounts/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_mounts の内容を適用せずに破棄する(mounts には影響しない)。pending_mounts が無い場合は 409 を返す
- Note: deploy の status が "applying" の間は 409 を返す。"deleting" の間も 409 を返す

## 参照

- [dev-docs/endpoints/volumes.md](../../endpoints/volumes.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)
