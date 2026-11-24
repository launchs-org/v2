[← ボリューム関連](README.md)

# ボリューム 破棄

- カテゴリ: ボリューム関連 (/projects/:projectId/volumes, /deploy/:deployId/mounts)
- Method / Path: `Post /projects/:projectId/volumes/:volumeId/discard`

## 説明

[ボリューム拡張](005-patch-projects-projectid-volumes-volumeid-size.md) で保存した `pending_sizeMB` を適用せずに破棄するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- `pending_sizeMB` が存在する状態で呼び出すと 200 が返り、`pending_sizeMB` が `null` に戻ること
- `sizeMB`(現在有効な値)には影響しないこと

### 異常系
- 存在しない `volumeId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId`/`volumeId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること
- `pending_sizeMB` が `null` の場合 409 が返ること
- `status` が `"applying"` の間に呼び出した場合 409 が返ること
- `status` が `"deleting"` の間に呼び出した場合 409 が返ること

### 境界値
- 特になし

## 仕様

- Post: /projects/:projectId/volumes/:volumeId/discard
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{}
```
- Note: pending_sizeMB の内容を適用せずに破棄する(sizeMB には影響しない)。pending_sizeMB が null の場合は 409 を返す
- Note: volume の status が "applying" の間は 409 を返す。"deleting" の間も 409 を返す

## 参照

- [dev-docs/endpoints/volumes.md](../../endpoints/volumes.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)
