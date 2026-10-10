[← ボリューム関連](README.md)

# ボリューム削除

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Delete /projects/:projectId/volumes/:volumeId`

## 説明

ボリュームを削除するエンドポイント。いずれかの deploy にマウント中の場合は削除できず、先にマウント解除(deployのボリュームマウント設定から外して適用)する必要がある。

## テスト要件

### 正常系
- どの deploy にもマウントされていない(`mountedBy` が空配列の)`volumeId` を指定すると 200 が返り、以後 [ボリューム取得](003-get-projects-projectid-volumes-volumeid.md) が 404 になること

### 異常系
- 存在しない `volumeId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`volumeId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- いずれかの deploy にマウント中の場合 409 が返ること(マウント解除してから削除する)
- `status` が `"applying"` の間に削除を呼び出した場合 409 が返ること(進行中の拡張applyと排他制御する)

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
- Note: いずれかの deploy にマウント中の場合は 409 を返す(マウント解除してから削除する)
- Note: volume の status が "applying" の間は 409 を返す(進行中の拡張applyと排他制御する)

## 参照

- [docs/endpoints/volumes.md](../../endpoints/volumes.md)
