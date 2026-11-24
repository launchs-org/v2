[← プロジェクト関連](README.md)

# プロジェクト取得

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Get /projects/:projectId`

## 説明

指定した `projectId` の単一プロジェクト情報を取得するエンドポイント。エラーのボディは [共通ルール](../../endpoints/common.md#エラーレスポンス) の形式。

## テスト要件

### 正常系
- 自分が所有する `projectId` を指定すると 200 が返り、`projectId`/`name`/`status` が取得できること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合 404 が返ること(存在しない場合と区別させないため、同じ 404 にする)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- 削除を受け付けた直後(削除が完了するまで)は 200 が返り、`status` が `"deleting"` であること

## 仕様

- Get: /projects/:projectId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "name": "my-app", "status": "active"}
```
- Note: `status` はプロジェクトの状態。`"active"` は通常の状態、`"deleting"` は削除を受け付けて実体を削除している最中の状態

## 参照

- [dev-docs/endpoints/projects.md](../../endpoints/projects.md)
- [dev-docs/endpoints/common.md](../../endpoints/common.md)
