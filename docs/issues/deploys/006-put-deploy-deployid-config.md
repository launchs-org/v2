[← デプロイ・起動設定関連](README.md)

# デプロイ 起動設定更新 (一括置き換え、pendingとして保存)

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Put /deploy/:deployId/config`

## 説明

`env`/`command`/`args` を一括で置き換え、`pending_*` として保存するエンドポイント。[適用](007-post-deploy-deployid-config-apply.md) を呼ぶまでコンテナには反映されない。 エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `env`/`command`/`args` を指定すると 200 が返り、[起動設定取得](005-get-deploy-deployid-config.md) の `pending_env`/`pending_command`/`pending_args` に反映されること
- 呼び出し直後、`env`/`command`/`args`(現在反映されている値)は変化しないこと

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `status` が `"applying"` の間はこの操作を受け付けず 409 が返ること(applying中はconfigに対する変更・apply・discardを排他制御する)
- `status` が `"deleting"` の間はこの操作を受け付けず 409 が返ること

### 境界値
- `env`/`command`/`args` は個別key単位のCRUDではなく、常に全件を送って置き換える方式であること(部分更新は不可)
- 現在の `env`/`command`/`args` と同じ内容をPUTした場合、`pending_*` は保存されず `null` になること(以前の pending も消える。変更が無いものとして扱う)
- 既存の `pending_*` が存在する状態で再度PUTした場合、新しい内容で `pending_*` が上書きされること(前回のpendingは破棄される)

## 仕様

- Put: /deploy/:deployId/config
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{
  "env": {"PORT": "8080", "DATABASE_URL": "postgres://..."},
  "command": "/app/server",
  "args": ["--config", "/app/config.yaml"]
}
```
- Response (200)
```json
{}
```
- Note: env/command/args は個別key単位のCRUDではなく、常に全件を送って置き換える方式(部分更新はしない)
- Note: この時点ではまだコンテナには反映されない。pending_env/pending_command/pending_args として保存され、[適用](007-post-deploy-deployid-config-apply.md) を呼ぶまで env/command/args(現在反映されている値)は変わらない
- Note: 送った内容が現在の env/command/args と同じ(変更が無い)場合は、pending_* を保存せず null にする
- Note: deploy の status が "applying" の間はこの操作を受け付けず 409 を返す(applying中はconfigに対する変更・apply・discardを排他制御する)
- Note: deploy の status が "deleting" の間も 409 を返す

## 参照

- [docs/endpoints/deploys.md](../../endpoints/deploys.md)
- [docs/endpoints/common.md](../../endpoints/common.md)
