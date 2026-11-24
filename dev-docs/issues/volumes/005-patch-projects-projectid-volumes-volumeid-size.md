[← ボリューム関連](README.md)

# ボリューム拡張 (一括置き換え、pendingとして保存)

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Patch /projects/:projectId/volumes/:volumeId/size`

## 説明

`sizeMB` を拡大方向にのみ変更し、`pending_sizeMB` として保存するエンドポイント。[適用](006-post-projects-projectid-volumes-volumeid-apply.md) を呼ぶまで実際のストレージには反映されない。実体は環境変数で選んだ StorageClass の PVC で、拡張(リサイズ)に対応した StorageClass が前提。[ボリューム名前変更](004-patch-projects-projectid-volumes-volumeid.md) とは別のエンドポイントとして分離している。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 現在の `sizeMB` を上回り、かつアカウントの上限以下の値を指定すると 200 が返り、[ボリューム取得](003-get-projects-projectid-volumes-volumeid.md) の `pending_sizeMB` に反映されること
- 呼び出し直後、`sizeMB`(現在有効な値)は変化しないこと

### 異常系
- 存在しない `volumeId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`volumeId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- 現在の `sizeMB` を下回る値を指定した場合エラーになること(縮小は不可)
- アカウントの上限(デフォルト5120MB)を超える `sizeMB` を指定した場合エラーになること
- `sizeMB` が未指定の場合 400 が返ること
- `status` が `"applying"` の間はこの操作を受け付けず 409 が返ること
- `status` が `"deleting"` の間は 409 が返ること

### 境界値
- 現在の `sizeMB` と同じ値を指定した場合(拡大なし)は許可されること
- アカウントの上限ちょうどの値まで拡張できること

## 仕様

- Patch: /projects/:projectId/volumes/:volumeId/size
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"sizeMB": 10240}
```
- Response (200)
```json
{}
```
- Note: sizeMB は拡大のみ許可し、縮小(現在のsizeMBを下回る値)を指定した場合はエラーを返す(永続ボリュームの一般的な制約に合わせる)
- Note: この時点ではまだ反映されない。pending_sizeMB として保存され、[ボリューム適用](006-post-projects-projectid-volumes-volumeid-apply.md) を呼ぶまで sizeMB(現在有効な値)は変わらない
- Note: アカウントごとの上限(デフォルト 5120MB)を超える sizeMB を指定した場合はエラーを返す
- Note: volume の status が "applying" の間はこの操作を受け付けず 409 を返す。"deleting" の間も 409 を返す
- Note: 拡張(リサイズ)は、環境変数で選んだ StorageClass の PVC を拡張して行う。拡張に対応した StorageClass が前提

## 参照

- [dev-docs/endpoints/volumes.md](../../endpoints/volumes.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)
