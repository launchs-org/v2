[← ボリューム関連](README.md)

# ボリューム作成

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Post /projects/:projectId/volumes`

## 説明

project に属する永続ストレージ(volume)を作成するエンドポイント。deploy を削除してもデータは残り、別の deploy から再マウントできる。`sizeMB` にはアカウントごとの上限(デフォルト 5GB = 5120MB)があり、上限を引き上げたい場合は申請する(申請フロー自体はアカウント管理側の別グループで設計する)。

## テスト要件

### 正常系
- アカウントの上限以下の `name`/`sizeMB` を指定すると 200 が返り、`volumeId` が発行されること
- 作成直後、`pending_sizeMB` が `null` であること
- 作成直後、[ボリューム一覧](002-get-projects-projectid-volumes.md) にそのボリュームが含まれること

### 異常系
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合アクセスできないこと(403 または 404)
- `name`/`sizeMB` が未指定の場合 400 が返ること
- `sizeMB` に0以下の値を指定した場合 400 が返ること
- アカウントの上限(デフォルト5120MB)を超える `sizeMB` を指定した場合エラーになること

### 境界値
- `name` の最小長・最大長、使用可能文字種(仕様未確定のため別途定義する)
- アカウントの上限ちょうどの `sizeMB` は許可されること

## 仕様

- Post: /projects/:projectId/volumes
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "db-data", "sizeMB": 5120}
```
- Response (200)
```json
{
  "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "name": "db-data",
  "pending_sizeMB": null,
  "sizeMB": 5120
}
```
- Note: volume は project に属する永続ストレージ。deploy を削除してもデータは残り、別の deploy から再マウントできる
- Note: sizeMB にはアカウントごとの上限(デフォルト 5GB = 5120MB)があり、超過した場合はエラーになる。上限はアカウントごとに個別設定可能で、デフォルトを超える上限が必要な場合は申請して引き上げる(申請の手段・承認フローは別途、アカウント管理側で設計する)

## 参照

- [docs/endpoints/volumes.md](../../endpoints/volumes.md)
