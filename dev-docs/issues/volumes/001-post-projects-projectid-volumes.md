[← ボリューム関連](README.md)

# ボリューム作成

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Post /projects/:projectId/volumes`

## 説明

project に属する永続ストレージ(volume)を作成するエンドポイント。deploy を削除してもデータは残り、別の deploy から再マウントできる。`sizeMB` にはアカウントごとの上限(デフォルト 5GB = 5120MB)があり、上限を引き上げたい場合は申請する(申請フロー自体はアカウント管理側の別グループで設計する)。実体は環境変数で選んだ StorageClass の PVC で、拡張(リサイズ)に対応した StorageClass が前提。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- アカウントの上限以下の `name`/`sizeMB` を指定すると 200 が返り、`volumeId` が発行されること
- 作成直後、`pending_sizeMB` が `null` で、`status` が `"active"` であること
- 作成直後、[ボリューム一覧](002-get-projects-projectid-volumes.md) にそのボリュームが含まれること

### 異常系
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `name`/`sizeMB` が未指定の場合 400 が返ること
- `sizeMB` に0以下の値を指定した場合 400 が返ること
- `name` が英数字とハイフン以外を含む場合、または65文字以上の場合 400 が返ること
- 同じ project 内で既に存在する `name` を指定した場合 409 が返ること
- `status` が `"deleting"` の project に volume を作成しようとした場合 409 が返ること
- アカウントの上限(デフォルト5120MB)を超える `sizeMB` を指定した場合エラーになること

### 境界値
- `name` は64文字ちょうどで作成できること、65文字で 400 になること。最小長は1文字(仮)
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
  "status": "active",
  "pending_sizeMB": null,
  "sizeMB": 5120
}
```
- Note: volume は project に属する永続ストレージ。deploy を削除してもデータは残り、別の deploy から再マウントできる
- Note: sizeMB にはアカウントごとの上限(デフォルト 5GB = 5120MB)があり、超過した場合はエラーになる。上限はアカウントごとに個別設定可能で、デフォルトを超える上限が必要な場合は申請して引き上げる(申請の手段・承認フローは別途、アカウント管理側で設計する)
- Note: status はボリューム自体の状態。"applying"(拡張の apply 中)| "active" | "failed"(拡張の反映に失敗)| "deleting"(削除中)。作成直後は "active"
- Note: name は英数字とハイフンのみ、最大64文字(最小1文字(仮))。違反は 400。project 内で重複した場合は 409
- Note: volume の実体は、環境変数で選んだ StorageClass の PVC。拡張(リサイズ)に対応した StorageClass が前提
- Note: project の status が "deleting" の間は volume を作成できず 409 を返す

## 参照

- [dev-docs/endpoints/volumes.md](../../endpoints/volumes.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)
