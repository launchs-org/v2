[← デプロイ・起動設定関連](README.md)

# デプロイ 起動設定取得 (環境変数・起動コマンド・引数)

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Get /deploy/:deployId/config`

## 説明

deploy の環境変数(`env`)・起動コマンド(`command`)・起動引数(`args`)を確認するエンドポイント。現在コンテナに反映されている値と、[起動設定更新](006-put-deploy-deployid-config.md) 後まだ [適用](007-post-deploy-deployid-config-apply.md)/[破棄](008-post-deploy-deployid-config-discard.md) されていない `pending_*` を、同一階層にフラットな形で返す(pending_*→apply/discardモデル)。

## テスト要件

### 正常系
- デプロイ作成直後、`env` は空オブジェクト、`command`/`args` は `null` で返り、`pending_env`/`pending_command`/`pending_args` も全て `null` であること(この時点でコンテナイメージのデフォルト起動コマンドで起動している)
- [起動設定更新](006-put-deploy-deployid-config.md) を実行した直後、`pending_env`/`pending_command`/`pending_args` に送信した内容が反映され、`env`/`command`/`args` は変化していないこと
- [適用](007-post-deploy-deployid-config-apply.md) 呼び出し直後は [デプロイ詳細取得](002-get-deploy-deployid.md) の `status` が `"applying"` になり、`pending_env`/`pending_command`/`pending_args` は消費されずそのまま保持され、`env`/`command`/`args` もまだ更新されていないこと
- コンテナの再起動が完了すると `status` が `"active"` に戻り、`env`/`command`/`args` が旧`pending_*`の内容に一致し、`pending_env`/`pending_command`/`pending_args` が全て `null` に戻ること
- 再起動が失敗した場合も、`status` が `"failed"` になった時点で `env`/`command`/`args` が旧`pending_*`の内容(適用しようとしたが失敗した内容)に更新され、`pending_env`/`pending_command`/`pending_args` は `null` に戻ること(実際にコンテナが起動できているかは `status` で判断する)
- [破棄](008-post-deploy-deployid-config-discard.md) 実行後、`pending_env`/`pending_command`/`pending_args` が `null` に戻り `env`/`command`/`args` は変化しないこと

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- アクセス権のない `deployId`(自分が所属していないプロジェクトのdeploy)を指定した場合アクセスできないこと(403 または 404)
- 認証ヘッダなし、または無効なアクセストークンの場合 401 が返ること

### 境界値
- `env` が空オブジェクト、`command`/`args` が未設定の状態でも正しく取得できること
- `command`/`args` が `null` の場合、コンテナイメージ自身が持つデフォルトの起動コマンド(DockerfileのCMD/ENTRYPOINT等)で起動していること(=`env`だけ指定し`command`/`args`を省略したデプロイでも正常に起動できる)
- `pending_env`/`pending_command`/`pending_args` は常に3つセットで null になる、または3つセットで値が入る(いずれか1つだけがnullでない、という中途半端な状態にはならないこと)

## 仕様

- Get: /deploy/:deployId/config
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、デプロイ作成直後の初期状態)
```json
{
  "env": {},
  "pending_env": null,
  "command": null,
  "pending_command": null,
  "args": null,
  "pending_args": null
}
```
- Response (200、起動設定を1度適用した後、pending が無い場合)
```json
{
  "env": {"PORT": "8080", "DATABASE_URL": "postgres://..."},
  "pending_env": null,
  "command": "/app/server",
  "pending_command": null,
  "args": ["--config", "/app/config.yaml"],
  "pending_args": null
}
```
- Response (200、pending が存在する場合)
```json
{
  "env": {"PORT": "8080", "DATABASE_URL": "postgres://..."},
  "pending_env": {"PORT": "8081", "DATABASE_URL": "postgres://..."},
  "command": "/app/server",
  "pending_command": null,
  "args": ["--config", "/app/config.yaml"],
  "pending_args": ["--config", "/app/config.v2.yaml"]
}
```
- Note: env/command/args は現在コンテナに反映されている値。pending_env/pending_command/pending_args はそれぞれ [起動設定更新](006-put-deploy-deployid-config.md) 後、まだ [適用](007-post-deploy-deployid-config-apply.md) されていない変更内容(変更が無ければ null)
- Note: command/args が null の場合、コンテナイメージ自身が持つデフォルトの起動コマンド(DockerfileのCMD/ENTRYPOINT等)がそのまま使われる。デプロイ作成直後は command/args ともに null であり、起動設定更新/適用を一度も行っていなければデフォルト起動コマンドのまま稼働する
- Note: env/command/args は常に3つセットで一括更新するため(部分更新はしない)、いずれか1つでも変更があれば pending_env/pending_command/pending_args の全てに値が入る(変更していないフィールドも直前にPUTした内容がそのまま入る)

## 参照

- [docs/endpoints/deploys.md](../../endpoints/deploys.md)
