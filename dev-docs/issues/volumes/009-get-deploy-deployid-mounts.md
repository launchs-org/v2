[← ボリューム関連](README.md)

# デプロイ ボリュームマウント取得

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Get /deploy/:deployId/mounts`

## 説明

deploy のコンテナに現在マウントされているボリューム構成(`mounts`)と、[マウント設定](010-put-deploy-deployid-mounts.md) 後まだ [適用](011-post-deploy-deployid-mounts-apply.md)/[破棄](012-post-deploy-deployid-mounts-discard.md) されていない `pending_mounts` を取得するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- マウント未設定のdeployの場合、`mounts` が空配列、`pending_mounts` が `null` であること
- `pending_mounts` が存在する場合、その内容が返ること

### 異常系
- 存在しない `deployId` を指定した場合 404 が返ること
- 他ユーザーが所有する `deployId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- 複数の volume を同時にマウントしている場合、`mounts` 配列に全て含まれること

## 仕様

- Get: /deploy/:deployId/mounts
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、pending が無い場合)
```json
{
  "mounts": [{"volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "mountPath": "/data"}],
  "pending_mounts": null
}
```
- Response (200、pending が存在する場合)
```json
{
  "mounts": [{"volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "mountPath": "/data"}],
  "pending_mounts": [
    {"volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "mountPath": "/data"},
    {"volumeId": "volume-yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy", "mountPath": "/logs"}
  ]
}
```
- Note: mounts は現在コンテナに反映されているマウント構成。pending_mounts は未 /apply の変更内容(変更が無ければ null)

## 参照

- [dev-docs/endpoints/volumes.md](../../endpoints/volumes.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)
