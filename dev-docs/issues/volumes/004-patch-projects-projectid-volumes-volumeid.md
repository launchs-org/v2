[← ボリューム関連](README.md)

# ボリューム名前変更

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Patch /projects/:projectId/volumes/:volumeId`

## 説明

ボリュームの `name` を変更するエンドポイント。`sizeMB` の変更はこのエンドポイントでは扱わず、[ボリューム拡張](005-patch-projects-projectid-volumes-volumeid-size.md) を使う。`name` の変更はストレージの実体に影響しないメタデータ変更のため、他グループのような pending_*→apply/discard モデルは使わず、呼び出し直後に即時反映される。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 有効な `name` を指定すると 200 が返り、[ボリューム取得](003-get-projects-projectid-volumes-volumeid.md) で変更後の `name` が即座に取得できること(apply等の追加操作は不要)

### 異常系
- 存在しない `volumeId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`volumeId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `name` が未指定の場合 400 が返ること
- `name` が英数字とハイフン以外を含む場合、または65文字以上の場合 400 が返ること
- 同じ project 内の他の volume と重複する `name` を指定した場合 409 が返ること
- `status` が `"deleting"` の間に呼び出した場合 409 が返ること
- `sizeMB` を一緒に送った場合は無視すること(拡張は[ボリューム拡張](005-patch-projects-projectid-volumes-volumeid-size.md)専用のエンドポイントで行う)

### 境界値
- `name` は64文字ちょうどで変更できること、65文字で 400 になること。最小長は1文字(仮)

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
- Note: name は英数字とハイフンのみ、最大64文字(最小1文字(仮))。違反は 400。project 内で他の volume と重複する場合は 409
- Note: volume の status が "deleting" の間はこの操作を受け付けず 409 を返す

## 参照

- [dev-docs/endpoints/volumes.md](../../endpoints/volumes.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)
