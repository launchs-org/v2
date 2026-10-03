[← プロジェクト関連](README.md)

# プロジェクト更新

- カテゴリ: プロジェクト関連 (/projects)
- Method / Path: `Patch /projects/:projectId`

## 説明

プロジェクトの `name` を変更するエンドポイント。

## テスト要件

### 正常系
- 有効な `name` を指定すると 200 が返り、[プロジェクト取得](003-get-projects-projectid.md) で変更後の `name` が取得できること

### 異常系
- 存在しない `projectId` を指定した場合 404 が返ること
- 他ユーザーが所有する `projectId` を指定した場合アクセスできないこと(403 または 404)
- `name` が未指定の場合 400 が返ること
- `Authorization` ヘッダが無い、または無効なアクセストークンの場合 401 が返ること

### 境界値
- `name` の最小長・最大長、使用可能文字種(仕様未確定のため別途定義する)

## 仕様

- Patch: /projects/:projectId
- Header
```json
{"Content-Type": "application/json", "Authorization": "Bearer <accessToken>"}
```
- Body
```json
{"name": "my-app-renamed"}
```
- Response (200)
```json
{}
```

## 参照

- [docs/endpoints/projects.md](../../endpoints/projects.md)
