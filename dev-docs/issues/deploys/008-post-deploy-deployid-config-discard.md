[← デプロイ・起動設定関連](README.md)

# デプロイ 起動設定 破棄

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Post /deploy/:deployId/config/discard`

## 説明

[起動設定更新](006-put-deploy-deployid-config.md) で保存した `pending_env`/`pending_command`/`pending_args` を適用せずに破棄するエンドポイント。 エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `pending_*` が存在する状態で呼び出すと 200 が返り、`pending_env`/`pending_command`/`pending_args` が `null` に戻ること
- `env`/`command`/`args`(現在反映されている値)には影響しないこと

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `pending_env`/`pending_command`/`pending_args` が全て `null` の場合 409 が返ること
- `status` が `"applying"` の間に呼び出した場合 409 が返ること(applying中はconfigに対する操作を排他制御する)
- `status` が `"deleting"` の間に呼び出した場合 409 が返ること

### 境界値
- 特になし

## 仕様

- Post: /deploy/:deployId/config/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_env/pending_command/pending_args の内容を適用せずに破棄する(env/command/args には影響しない)。全て null の場合は 409 を返す
- Note: deploy の status が "applying" の間は 409 を返す(applying中はconfigに対する操作を排他制御する)
- Note: deploy の status が "deleting" の間も 409 を返す

## 参照

- [dev-docs/endpoints/deploys.md](../../endpoints/deploys.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)
