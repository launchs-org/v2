[← ボリューム関連](README.md)

# ボリューム取得

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Get /projects/:projectId/volumes/:volumeId`

## 説明

指定したボリュームの詳細を取得するエンドポイント。どの deploy にマウントされているか(`mountedBy`)も含む。

## テスト要件

### 正常系
- 自分が所有する `volumeId` を指定すると 200 が返り、`name`/`sizeMB`/`mountedBy` が取得できること
- どの deploy にもマウントされていない場合、`mountedBy` が空配列であること
- [ボリューム名前変更](004-patch-projects-projectid-volumes-volumeid.md) 後、変更後の `name` が即座に取得できること
- `pending_sizeMB` が存在する場合、[ボリューム拡張](005-patch-projects-projectid-volumes-volumeid-size.md) 後まだ [適用](006-post-projects-projectid-volumes-volumeid-apply.md)/[破棄](007-post-projects-projectid-volumes-volumeid-discard.md) されていない変更内容が返ること(無ければ `null`)

### 異常系
- 存在しない `volumeId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`volumeId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- `status` は "applying" | "active" | "failed" 以外の値を取らないこと
- 複数の deploy にマウントされている場合、`mountedBy` にそれら全ての `deployId` が含まれること

## 仕様

- Get: /projects/:projectId/volumes/:volumeId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200、pending が無い場合)
```json
{
  "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "name": "db-data",
  "pending_sizeMB": null,
  "sizeMB": 5120,
  "status": "active",
  "mountedBy": ["deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"]
}
```
- Response (200、pending が存在する場合)
```json
{
  "volumeId": "volume-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "name": "db-data",
  "pending_sizeMB": 10240,
  "sizeMB": 5120,
  "status": "active",
  "mountedBy": ["deploy-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"]
}
```
- Note: sizeMB は現在有効な内容。pending_sizeMB は[ボリューム拡張](005-patch-projects-projectid-volumes-volumeid-size.md)後まだ[適用](006-post-projects-projectid-volumes-volumeid-apply.md)されていない変更内容(変更が無ければ null)
- Note: status は "applying" | "active" | "failed" のいずれか。applyを呼ぶと反映が完了するまで "applying" になる

## 参照

- [docs/endpoints/volumes.md](../../endpoints/volumes.md)
