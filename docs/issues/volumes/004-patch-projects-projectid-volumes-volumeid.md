[← ボリューム関連](README.md)

# ボリューム名前変更

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Patch /projects/:projectId/volumes/:volumeId`

## 説明

ボリュームの `name` を変更するエンドポイント。`sizeMB` の変更はこのエンドポイントでは扱わず、[ボリューム拡張](005-patch-projects-projectid-volumes-volumeid-size.md) を使う。`name` の変更はストレージの実体に影響しないメタデータ変更のため、他グループのような pending_*→apply/discard モデルは使わず、呼び出し直後に即時反映される。

## テスト要件

### 正常系
- 有効な `name` を指定すると 200 が返り、[ボリューム取得](003-get-projects-projectid-volumes-volumeid.md) で変更後の `name` が即座に取得できること(apply等の追加操作は不要)

### 異常系
- 存在しない `volumeId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`volumeId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `name` が未指定の場合 400 が返ること
- `sizeMB` を一緒に送った場合は無視すること(拡張は[ボリューム拡張](005-patch-projects-projectid-volumes-volumeid-size.md)専用のエンドポイントで行う)

### 境界値
- `name` の最小長・最大長、使用可能文字種(仕様未確定のため別途定義する)

## 仕様

- Patch: /projects/:projectId/volumes/:volumeId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "db-data-renamed"}
```
- Response (200)
```json
{}
```
- Note: name のみ変更可能(sizeMBはこのエンドポイントでは変更できない。拡張は[ボリューム拡張](005-patch-projects-projectid-volumes-volumeid-size.md)を使う)。pending_*→apply/discardモデルは使わず、呼び出し直後に即時反映される(ストレージ側の実体には影響しないメタデータの変更のため)

## 参照

- [docs/endpoints/volumes.md](../../endpoints/volumes.md)
