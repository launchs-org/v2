[← ボリューム関連](README.md)

# ボリューム削除

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Delete /projects/:projectId/volumes/:volumeId`

## 説明

ボリュームを削除するエンドポイント。削除は非同期で、受け付けると `status` が直ちに `"deleting"` になり、実体の削除が完了するとレコード自体が消える。いずれかの deploy にマウント中の場合は削除できず、先にマウント解除(deployのボリュームマウント設定から外して適用)する必要がある。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- どの deploy にもマウントされていない(`mountedBy` が空配列の)`volumeId` を指定すると 200 が返り、`status` が即座に `"deleting"` になること(GET は `"deleting"` の状態を返す)
- 削除が完了したら [ボリューム取得](003-get-projects-projectid-volumes-volumeid.md) が 404 になること(実体の削除は Workflow が非同期に行う)
- project を削除すると、配下の volume が、マウントの有無にかかわらず、データごと削除されること

### 異常系
- 存在しない `volumeId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`volumeId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- いずれかの deploy にマウント中の場合 409 が返ること(マウント解除してから削除する)
- `status` が `"applying"` の間に削除を呼び出した場合 409 が返ること(進行中の拡張applyと排他制御する)
- `status` が `"deleting"` の間の変更系操作(名前変更・拡張・apply・discard)は 409 が返ること
- `status` が `"deleting"` の間の再度の削除は 409 が返ること

### 境界値
- 特になし

## 仕様

- Delete: /projects/:projectId/volumes/:volumeId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: 呼び出すと即座に応答し、status は直ちに "deleting" になる。実体の削除は Workflow で非同期に行い、完了するとレコード自体が削除される(以後の GET は 404 を返す)
- Note: deleting の間は、変更系の操作と再度の削除は 409 を返す。GET は status が "deleting" の状態を返す
- Note: いずれかの deploy にマウント中の場合は 409 を返す(マウント解除してから削除する)
- Note: volume の status が "applying" の間は 409 を返す(進行中の拡張applyと排他制御する)
- Note: project を削除すると、配下の volume は、マウントの有無にかかわらず、データごと削除される

## 参照

- [docs/endpoints/volumes.md](../../endpoints/volumes.md)
- [docs/endpoints/common.md](../../endpoints/common.md)
