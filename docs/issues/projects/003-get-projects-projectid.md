[← プロジェクト関連](README.md)

# プロジェクト取得

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Get /projects/:projectId`

## 説明

指定した `projectId` の単一プロジェクト情報を取得するエンドポイント。

## テスト要件

### 正常系
- 自分が所有する `projectId` を指定すると 200 が返り、`projectId`/`name` が取得できること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合アクセスできないこと(403 または 404)
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- 特になし

## 仕様

- Get: /projects/:projectId
- Header
```json
{"Authorization": "Bearer <accessToken>"}
```
- Response (200)
```json
{"projectId": "project-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "name": "my-app"}
```

## 参照

- [docs/endpoints/projects.md](../../endpoints/projects.md)
