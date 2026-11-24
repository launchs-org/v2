[← デプロイ・起動設定関連](README.md)

# デプロイ削除

- カテゴリ: デプロイ・起動設定関連 (/deploy)
- Method / Path: `Delete /deploy/:deployId`

## 説明

deploy を削除するエンドポイント。`replicas` の値や `status` に関わらず、稼働中の deploy に対してもそのまま削除コマンドを受け付ける(事前にスケーリングで `replicas` を 0 にする手順は不要)。ただし `status` が `"applying"` の間は 409。削除は非同期で、受け付けると `status` が `"deleting"` になり、実体の削除は Workflow が行う。 エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `replicas` が1以上の稼働中(`status: "active"`)の deploy に対してそのまま削除を呼び出しても 200 が返り、コンテナの停止・削除まで一貫して行われること
- 削除を受け付けると 200 が返り、`status` が即座に `"deleting"` になること(以後の [デプロイ詳細取得](002-get-deploy-deployid.md) は `status` が `"deleting"` の状態を返す)
- 削除が完了したら(レコード自体が削除され)、[デプロイ詳細取得](002-get-deploy-deployid.md) が 404 になること
- 削除時、配下の [サービス](../services/README.md) も連鎖して削除されること(service は `deleting` になる)
- 削除時、配下のボリュームマウント設定は削除されるが、マウントされていた [ボリューム](../volumes/README.md) 自体は残ること(volumeはprojectに属するため)

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `status` が `"applying"` の間に削除を呼び出した場合 409 が返ること(進行中の apply を中断して削除することはしない)
- `status` が `"deleting"` の間に変更系の操作([スケーリング](003-post-deploy-deployid-scale.md)・[起動設定更新](006-put-deploy-deployid-config.md)・[適用](007-post-deploy-deployid-config-apply.md)・[破棄](008-post-deploy-deployid-config-discard.md) など)を呼び出した場合 409 が返ること
- `status` が `"deleting"` の間に再度削除を呼び出した場合 409 が返ること

### 境界値
- `deleting` の間、GET は `status` が `"deleting"` の状態を返すこと
- 削除した deploy のログも、deploy の削除と一緒に削除されること

## 仕様

- Delete: /deploy/:deployId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: replicas や status に関わらず、稼働中の deploy に対してもそのまま削除を受け付ける。事前に [スケーリング](003-post-deploy-deployid-scale.md) で replicas を 0 にする必要はなく、削除処理の内部でコンテナの停止まで行う
- Note: 呼び出すと即座に応答し、status は直ちに "deleting" になる。実体の削除は Workflow で非同期に行い、完了するとレコード自体が削除される(以後の GET は 404 を返す)
- Note: deleting の間は、変更系の操作と再度の削除は 409 を返す。GET は status が "deleting" の状態を返す
- Note: deploy の status が "applying" の間は 409 を返す(進行中の apply を中断して削除することはしない)
- Note: 配下の service も連鎖して削除される(service は "deleting" になる)。ボリュームのマウント設定も削除されるが、マウントされていた volume 自体は残る(volume は project に属するため)
- Note: deploy のログも deploy の削除と一緒に削除される

## 参照

- [dev-docs/endpoints/deploys.md](../../endpoints/deploys.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)
